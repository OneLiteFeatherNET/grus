package templates

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteTarGzRoundTripPreservesContentAndMode(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t,
		testEntry{name: "bin/run.sh", body: "#!/bin/sh\n", mode: 0o755},
		testEntry{name: "conf/app.yml", body: "x: 1\n", mode: 0o600},
	))

	var archive bytes.Buffer
	if err := s.WriteTarGz(context.Background(), "server", v.Digest, &archive); err != nil {
		t.Fatalf("WriteTarGz: %v", err)
	}
	dest := t.TempDir()
	if err := ExtractTarGz(&archive, dest); err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}

	assertFile(t, filepath.Join(dest, "bin", "run.sh"), "#!/bin/sh\n", 0o755)
	assertFile(t, filepath.Join(dest, "conf", "app.yml"), "x: 1\n", 0o600)
}

func TestWriteTarGzIsDeterministic(t *testing.T) {
	s := newFS(t)
	v := importTar(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}, testEntry{name: "b.txt", body: "beta"}))

	var first, second bytes.Buffer
	if err := s.WriteTarGz(context.Background(), "server", v.Digest, &first); err != nil {
		t.Fatalf("WriteTarGz: %v", err)
	}
	if err := s.WriteTarGz(context.Background(), "server", v.Digest, &second); err != nil {
		t.Fatalf("WriteTarGz: %v", err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("two WriteTarGz calls for the same digest produced different bytes")
	}
}

func TestExtractOverwritesExistingFile(t *testing.T) {
	dest := t.TempDir()
	mustWrite(t, filepath.Join(dest, "a.txt"), "old")

	if err := ExtractTarGz(bytes.NewReader(tarGz(t, testEntry{name: "a.txt", body: "new"})), dest); err != nil {
		t.Fatalf("ExtractTarGz: %v", err)
	}

	assertFile(t, filepath.Join(dest, "a.txt"), "new", 0o644)
}

func TestExtractRejectsPathTraversal(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "dest")

	err := ExtractTarGz(bytes.NewReader(tarGz(t, testEntry{name: "../escaped.txt", body: "x"})), dest)
	if err == nil {
		t.Fatal("traversal entry was accepted, want an error")
	}
	if _, statErr := os.Stat(filepath.Join(root, "escaped.txt")); statErr == nil {
		t.Fatal("traversal entry wrote a file outside dest")
	}
}

func TestExtractRejectsSymlinkEntries(t *testing.T) {
	dest := t.TempDir()

	err := ExtractTarGz(bytes.NewReader(tarGz(t, testEntry{name: "link", typeflag: tar.TypeSymlink, linkname: "/etc"})), dest)
	if err == nil {
		t.Fatal("symlink entry was accepted, want an error")
	}
}

func assertFile(t *testing.T, path, content string, perm os.FileMode) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(b) != content {
		t.Fatalf("%s content = %q, want %q", path, b, content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != perm {
		t.Fatalf("%s mode = %o, want %o", path, got, perm)
	}
}
