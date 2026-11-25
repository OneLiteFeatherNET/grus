package templates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// FS is a Store that keeps the shared layout (see layout.go) in a directory tree:
//
//	blobs/sha256/<hex>
//	manifests/<name>/<hex>.json
//	tags/<name>/<tag>
type FS struct {
	root string
	layout
}

var _ Store = (*FS)(nil)

// NewFS opens the store rooted at root, creating the layout directories if needed.
func NewFS(root string) (*FS, error) {
	if root == "" {
		return nil, errors.New("templates: empty fs root")
	}
	for _, sub := range []string{"blobs/sha256", "manifests", "tags"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(sub)), dirPerm); err != nil {
			return nil, fmt.Errorf("templates: create %s: %w", sub, err)
		}
	}
	return &FS{root: root, layout: layout{objects: dirObjects{root: root}, spoolDir: root}}, nil
}

// dirObjects implements objectClient on a directory tree. Puts are atomic: the content goes to a temp
// file next to the target and is renamed into place.
type dirObjects struct {
	root string
}

func (d dirObjects) path(key string) string {
	return filepath.Join(d.root, filepath.FromSlash(key))
}

func (d dirObjects) Stat(_ context.Context, key string) (bool, error) {
	_, err := os.Stat(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (d dirObjects) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	target := d.path(key)
	if err := os.MkdirAll(filepath.Dir(target), dirPerm); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, r)
	if err == nil {
		err = tmp.Chmod(filePerm)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("templates: write %s: %w", key, err)
	}
	return os.Rename(tmp.Name(), target)
}

func (d dirObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f, err := os.Open(d.path(key))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, errNoObject
	}
	return f, err
}

// List walks the directory below prefix and returns the file keys in slash form.
func (d dirObjects) List(_ context.Context, prefix string) ([]string, error) {
	var keys []string
	err := filepath.WalkDir(d.path(prefix), func(p string, e fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(d.root, p)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return keys, err
}
