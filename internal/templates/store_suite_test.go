package templates

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// runStoreSuite checks the behaviour every Store backend must share. newStore returns a fresh, empty
// store per subtest.
func runStoreSuite(t *testing.T, newStore func(t *testing.T) Store) {
	t.Helper()
	ctx := context.Background()

	t.Run("digest ignores entry order", func(t *testing.T) {
		s := newStore(t)
		alpha := testEntry{name: "a.txt", body: "alpha"}
		beta := testEntry{name: "b.txt", body: "beta"}
		v1 := importArchive(t, s, "server", tarGz(t, alpha, beta))
		v2 := importArchive(t, s, "server", tarGz(t, beta, alpha))
		if v1.Digest != v2.Digest {
			t.Fatalf("same content in other entry order gave digest %s, want %s", v2.Digest, v1.Digest)
		}
	})

	t.Run("different content yields a different digest", func(t *testing.T) {
		s := newStore(t)
		v1 := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		v2 := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "gamma"}))
		if v1.Digest == v2.Digest {
			t.Fatalf("different content produced the same digest %s", v1.Digest)
		}
	})

	t.Run("re-import of a known version keeps its digest and size", func(t *testing.T) {
		s := newStore(t)
		archive := tarGz(t, testEntry{name: "a.txt", body: "alpha"}, testEntry{name: "b.txt", body: "beta"})
		v1 := importArchive(t, s, "server", archive)
		v2 := importArchive(t, s, "server", archive)
		if v1 != v2 {
			t.Fatalf("re-import gave %+v, want %+v", v2, v1)
		}
		if v1.Size != int64(len("alpha")+len("beta")) {
			t.Fatalf("Size = %d, want %d", v1.Size, len("alpha")+len("beta"))
		}
	})

	t.Run("WriteTarGz returns the imported files with modes", func(t *testing.T) {
		s := newStore(t)
		v := importArchive(t, s, "server", tarGz(t,
			testEntry{name: "run.sh", body: "#!/bin/sh", mode: 0o755},
			testEntry{name: "conf/server.properties", body: "motd=hi"},
		))
		got := exportFiles(t, s, "server", v.Digest)
		want := map[string]exportedFile{
			"run.sh":                 {body: "#!/bin/sh", mode: 0o755},
			"conf/server.properties": {body: "motd=hi", mode: 0o644},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("exported files = %v, want %v", got, want)
		}
	})

	t.Run("ImportDir takes only the visible files of a ConfigMap layout", func(t *testing.T) {
		src := t.TempDir()
		mustWrite(t, filepath.Join(src, "..2026_11_25", "server.properties"), "motd=hi")
		mustWrite(t, filepath.Join(src, "..2026_11_25", "extra.txt"), "extra")
		mustSymlink(t, "..2026_11_25", filepath.Join(src, "..data"))
		mustSymlink(t, "..data/server.properties", filepath.Join(src, "server.properties"))
		mustSymlink(t, "..data/extra.txt", filepath.Join(src, "extra.txt"))

		s := newStore(t)
		v, err := s.ImportDir(ctx, "server", src)
		if err != nil {
			t.Fatalf("ImportDir: %v", err)
		}
		got := exportFiles(t, s, "server", v.Digest)
		want := map[string]exportedFile{
			"extra.txt":         {body: "extra", mode: 0o644},
			"server.properties": {body: "motd=hi", mode: 0o644},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("ImportDir files = %v, want %v", got, want)
		}
	})

	t.Run("tag resolves to its digest", func(t *testing.T) {
		s := newStore(t)
		v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		if err := s.Tag(ctx, "server", "stable", v.Digest); err != nil {
			t.Fatalf("Tag: %v", err)
		}
		got, err := s.Resolve(ctx, "server", "stable")
		if err != nil {
			t.Fatalf("Resolve tag: %v", err)
		}
		if got != v.Digest {
			t.Fatalf("Resolve(stable) = %s, want %s", got, v.Digest)
		}
	})

	t.Run("a digest resolves to itself", func(t *testing.T) {
		s := newStore(t)
		v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		got, err := s.Resolve(ctx, "server", v.Digest)
		if err != nil {
			t.Fatalf("Resolve digest: %v", err)
		}
		if got != v.Digest {
			t.Fatalf("Resolve(digest) = %s, want %s", got, v.Digest)
		}
	})

	t.Run("unknown tag is ErrNotFound", func(t *testing.T) {
		s := newStore(t)
		importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		if _, err := s.Resolve(ctx, "server", "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Resolve(missing) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("unknown digest is ErrNotFound", func(t *testing.T) {
		s := newStore(t)
		unknown := "sha256:" + strings.Repeat("0", 64)
		if _, err := s.Resolve(ctx, "server", unknown); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Resolve(unknown digest) error = %v, want ErrNotFound", err)
		}
	})

	t.Run("List returns the tags of each template", func(t *testing.T) {
		s := newStore(t)
		v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		other := importArchive(t, s, "proxy", tarGz(t, testEntry{name: "b.txt", body: "beta"}))
		for _, tag := range []string{"stable", "2.20.0"} {
			if err := s.Tag(ctx, "server", tag, v.Digest); err != nil {
				t.Fatalf("Tag %s: %v", tag, err)
			}
		}
		if err := s.Tag(ctx, "proxy", "stable", other.Digest); err != nil {
			t.Fatalf("Tag proxy: %v", err)
		}
		got, err := s.List(ctx)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		want := map[string]map[string]string{
			"server": {"stable": v.Digest, "2.20.0": v.Digest},
			"proxy":  {"stable": other.Digest},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("List() = %v, want %v", got, want)
		}
	})

	t.Run("invalid template name is rejected", func(t *testing.T) {
		for _, name := range []string{"Server", "", "-server", "a/b", strings.Repeat("a", 64)} {
			s := newStore(t)
			_, err := s.ImportTarGz(ctx, name, bytes.NewReader(tarGz(t, testEntry{name: "a.txt", body: "x"})))
			if err == nil {
				t.Fatalf("template name %q was accepted, want an error", name)
			}
		}
	})

	t.Run("invalid tag is rejected", func(t *testing.T) {
		s := newStore(t)
		v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
		if err := s.Tag(ctx, "server", "Stable!", v.Digest); err == nil {
			t.Fatal("invalid tag was accepted, want an error")
		}
	})

	t.Run("paths escaping the template root are rejected", func(t *testing.T) {
		s := newStore(t)
		_, err := s.ImportTarGz(ctx, "server", bytes.NewReader(tarGz(t, testEntry{name: "../evil.txt", body: "x"})))
		if err == nil {
			t.Fatal("path with .. was accepted, want an error")
		}
	})

	t.Run("symlink entries in a tar are rejected", func(t *testing.T) {
		s := newStore(t)
		_, err := s.ImportTarGz(ctx, "server", bytes.NewReader(tarGz(t, testEntry{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"})))
		if err == nil {
			t.Fatal("symlink entry was accepted, want an error")
		}
	})
}

// importArchive imports a tar.gz and fails the test on error.
func importArchive(t *testing.T, s Store, name string, archive []byte) Version {
	t.Helper()
	v, err := s.ImportTarGz(context.Background(), name, bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("ImportTarGz(%s): %v", name, err)
	}
	return v
}

type exportedFile struct {
	body string
	mode int64
}

// exportFiles reads a version back through WriteTarGz and returns its regular files by path.
func exportFiles(t *testing.T, s Store, name, digest string) map[string]exportedFile {
	t.Helper()
	var buf bytes.Buffer
	if err := s.WriteTarGz(context.Background(), name, digest, &buf); err != nil {
		t.Fatalf("WriteTarGz: %v", err)
	}
	gz, err := gzip.NewReader(&buf)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string]exportedFile{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = exportedFile{body: string(body), mode: hdr.Mode}
	}
}

func TestFSStoreSuite(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store { return newFS(t) })
}
