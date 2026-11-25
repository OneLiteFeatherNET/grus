package templates

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// testEntry describes one tar entry for tarGz.
type testEntry struct {
	name     string
	body     string
	mode     int64
	typeflag byte
	linkname string
}

func tarGz(t *testing.T, entries ...testEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		typ := e.typeflag
		if typ == 0 {
			typ = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &tar.Header{Name: e.name, Typeflag: typ, Mode: mode, Linkname: e.linkname}
		if typ == tar.TypeReg {
			hdr.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %q: %v", e.name, err)
		}
		if typ == tar.TypeReg {
			if _, err := io.WriteString(tw, e.body); err != nil {
				t.Fatalf("write tar body %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func newFS(t *testing.T) *FS {
	t.Helper()
	s, err := NewFS(t.TempDir())
	if err != nil {
		t.Fatalf("NewFS: %v", err)
	}
	return s
}

func importTar(t *testing.T, s *FS, name string, archive []byte) Version {
	t.Helper()
	v, err := s.ImportTarGz(context.Background(), name, bytes.NewReader(archive))
	if err != nil {
		t.Fatalf("ImportTarGz(%s): %v", name, err)
	}
	return v
}

func readManifest(t *testing.T, s *FS, name, digest string) Manifest {
	t.Helper()
	m, err := s.readManifest(context.Background(), name, digest)
	if err != nil {
		t.Fatalf("readManifest(%s, %s): %v", name, digest, err)
	}
	return m
}

func countEntries(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	return len(entries)
}

func TestImportTarGzDigestIgnoresEntryOrder(t *testing.T) {
	s := newFS(t)
	alpha := testEntry{name: "a.txt", body: "alpha"}
	beta := testEntry{name: "b.txt", body: "beta"}

	v1 := importTar(t, s, "server", tarGz(t, alpha, beta))
	v2 := importTar(t, s, "server", tarGz(t, beta, alpha))

	if v1.Digest != v2.Digest {
		t.Fatalf("same content in other entry order gave digest %s, want %s", v2.Digest, v1.Digest)
	}
}

func TestImportTarGzDifferentContentYieldsDifferentDigest(t *testing.T) {
	s := newFS(t)

	v1 := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	v2 := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "gamma"}))

	if v1.Digest == v2.Digest {
		t.Fatalf("different content produced the same digest %s", v1.Digest)
	}
}

func TestImportTarGzWritesNoBlobTwice(t *testing.T) {
	s := newFS(t)
	blobs := filepath.Join(s.root, "blobs", "sha256")

	importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}, testEntry{name: "b.txt", body: "beta"}))
	before := countEntries(t, blobs)
	importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}, testEntry{name: "b.txt", body: "beta"}))
	if after := countEntries(t, blobs); after != before {
		t.Fatalf("re-import of identical files changed blob count from %d to %d", before, after)
	}

	importTar(t, s, "other", tarGz(t, testEntry{name: "a.txt", body: "alpha"}, testEntry{name: "c.txt", body: "gamma"}))
	if got := countEntries(t, blobs); got != before+1 {
		t.Fatalf("only the missing blob should be written: blob count %d, want %d", got, before+1)
	}
}

func TestImportTarGzSkipsDirectoriesAndStripsDotSlash(t *testing.T) {
	s := newFS(t)
	archive := tarGz(t,
		testEntry{name: "./", typeflag: tar.TypeDir},
		testEntry{name: "./conf/", typeflag: tar.TypeDir},
		testEntry{name: "./conf/app.yml", body: "x: 1"},
	)

	v := importTar(t, s, "server", archive)
	m := readManifest(t, s, "server", v.Digest)

	var paths []string
	for _, f := range m.Files {
		paths = append(paths, f.Path)
	}
	if want := []string{"conf/app.yml"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("manifest paths = %v, want %v", paths, want)
	}
}

func TestImportTarGzRejectsUnsafePaths(t *testing.T) {
	cases := map[string]string{
		"parent segment":   "../evil.txt",
		"nested parent":    "a/../../evil.txt",
		"dot-slash parent": "./../x.txt",
		"absolute path":    "/etc/passwd",
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			s := newFS(t)
			_, err := s.ImportTarGz(context.Background(), "server", bytes.NewReader(tarGz(t, testEntry{name: entry, body: "x"})))
			if err == nil {
				t.Fatalf("entry %q was accepted, want an error", entry)
			}
		})
	}
}

func TestImportTarGzRejectsNonRegularEntries(t *testing.T) {
	cases := map[string]testEntry{
		"symlink":  {name: "link", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"},
		"hardlink": {name: "hard", typeflag: tar.TypeLink, linkname: "a.txt"},
		"chardev":  {name: "dev", typeflag: tar.TypeChar},
	}
	for name, entry := range cases {
		t.Run(name, func(t *testing.T) {
			s := newFS(t)
			_, err := s.ImportTarGz(context.Background(), "server", bytes.NewReader(tarGz(t, entry)))
			if err == nil {
				t.Fatalf("%s entry was accepted, want an error", name)
			}
		})
	}
}

func TestImportDirUsesOnlyVisibleFilesOfConfigMapLayout(t *testing.T) {
	src := t.TempDir()
	mustWrite(t, filepath.Join(src, "..2026_11_25", "server.properties"), "motd=hi")
	mustWrite(t, filepath.Join(src, "..2026_11_25", "extra.txt"), "extra")
	mustSymlink(t, "..2026_11_25", filepath.Join(src, "..data"))
	mustSymlink(t, "..data/server.properties", filepath.Join(src, "server.properties"))
	mustSymlink(t, "..data/extra.txt", filepath.Join(src, "extra.txt"))
	if err := os.Mkdir(filepath.Join(src, "..2026_11_25", "plugins"), 0o755); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "..data/plugins", filepath.Join(src, "plugins"))

	s := newFS(t)
	v, err := s.ImportDir(context.Background(), "server", src)
	if err != nil {
		t.Fatalf("ImportDir: %v", err)
	}
	m := readManifest(t, s, "server", v.Digest)

	var paths []string
	for _, f := range m.Files {
		paths = append(paths, f.Path)
	}
	if want := []string{"extra.txt", "server.properties"}; !reflect.DeepEqual(paths, want) {
		t.Fatalf("ImportDir paths = %v, want only the visible files %v", paths, want)
	}
}

func TestTagPointsAtVersionAndResolvesBack(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))

	if err := s.Tag(context.Background(), "server", "stable", v.Digest); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	got, err := s.Resolve(context.Background(), "server", "stable")
	if err != nil {
		t.Fatalf("Resolve tag: %v", err)
	}
	if got != v.Digest {
		t.Fatalf("Resolve(stable) = %s, want %s", got, v.Digest)
	}
}

func TestResolveAcceptsExistingDigestAsIs(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))

	got, err := s.Resolve(context.Background(), "server", v.Digest)
	if err != nil {
		t.Fatalf("Resolve digest: %v", err)
	}
	if got != v.Digest {
		t.Fatalf("Resolve(digest) = %s, want %s", got, v.Digest)
	}
}

func TestResolveUnknownTagIsNotFound(t *testing.T) {
	s := newFS(t)
	importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))

	_, err := s.Resolve(context.Background(), "server", "missing")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve(missing) error = %v, want ErrNotFound", err)
	}
}

func TestResolveUnknownDigestIsNotFound(t *testing.T) {
	s := newFS(t)
	unknown := "sha256:" + strings.Repeat("0", 64)

	_, err := s.Resolve(context.Background(), "server", unknown)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve(unknown digest) error = %v, want ErrNotFound", err)
	}
}

func TestListReturnsTagsPerTemplate(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	if err := s.Tag(context.Background(), "server", "stable", v.Digest); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if err := s.Tag(context.Background(), "server", "2.20.0", v.Digest); err != nil {
		t.Fatalf("Tag: %v", err)
	}

	got, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := map[string]map[string]string{
		"server": {"stable": v.Digest, "2.20.0": v.Digest},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
}

func TestImportRejectsInvalidTemplateName(t *testing.T) {
	cases := []string{"Server", "", "-server", "a/b", strings.Repeat("a", 64)}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			s := newFS(t)
			_, err := s.ImportTarGz(context.Background(), name, bytes.NewReader(tarGz(t, testEntry{name: "a.txt", body: "x"})))
			if err == nil {
				t.Fatalf("template name %q was accepted, want an error", name)
			}
		})
	}
}

func TestTagRejectsInvalidTag(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))

	if err := s.Tag(context.Background(), "server", "Stable!", v.Digest); err == nil {
		t.Fatal("invalid tag was accepted, want an error")
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}
