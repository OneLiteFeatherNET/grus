package templates

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// ExtractTarGz overlays the entries of a tar.gz stream onto dest. Directories are created, regular
// files are written with their archive mode and overwrite existing files. Other entry types and
// unsafe paths are rejected.
func ExtractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("templates: open gzip: %w", err)
	}
	defer gz.Close()
	if err := os.MkdirAll(dest, dirPerm); err != nil {
		return err
	}

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("templates: read tar: %w", err)
		}
		p, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return err
		}
		isDir, err := checkEntryType(hdr)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, filepath.FromSlash(p))
		if isDir {
			if err := os.MkdirAll(target, dirPerm); err != nil {
				return err
			}
			continue
		}
		if p == "" {
			return fmt.Errorf("templates: entry %q has no file name", hdr.Name)
		}
		if err := writeOverlayFile(target, tr, entryMode(hdr.Mode)); err != nil {
			return err
		}
	}
}

// writeOverlayFile replaces target with the content of r (temp file + rename) and sets mode exactly.
func writeOverlayFile(target string, r io.Reader, mode uint32) error {
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".extract-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, r)
	if err == nil {
		err = tmp.Chmod(os.FileMode(mode))
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("templates: write %s: %w", target, err)
	}
	return os.Rename(tmp.Name(), target)
}

// cleanArchivePath normalizes an archive entry name. Leading "./" is stripped; absolute names and any
// ".." segment are rejected. It returns the slash separated relative path, "" for the archive root.
func cleanArchivePath(name string) (string, error) {
	n := name
	for strings.HasPrefix(n, "./") {
		n = n[2:]
	}
	if n == "." {
		n = ""
	}
	if strings.HasPrefix(n, "/") {
		return "", fmt.Errorf("templates: absolute path %q", name)
	}
	for _, seg := range strings.Split(n, "/") {
		if seg == ".." {
			return "", fmt.Errorf("templates: path %q escapes the template root", name)
		}
	}
	if n == "" {
		return "", nil
	}
	return path.Clean(n), nil
}

// checkEntryType reports whether the entry is a directory (to be skipped by importers) and rejects
// everything that is neither a directory nor a regular file.
func checkEntryType(hdr *tar.Header) (bool, error) {
	switch hdr.Typeflag {
	case tar.TypeDir:
		return true, nil
	case tar.TypeReg, '\x00':
		return false, nil
	default:
		return false, fmt.Errorf("templates: unsupported tar entry %q (type %q)", hdr.Name, hdr.Typeflag)
	}
}

// entryMode keeps the permission bits of a tar or file mode; 0 means the default 0o644.
func entryMode(m int64) uint32 {
	p := uint32(m & 0o777)
	if p == 0 {
		return filePerm
	}
	return p
}
