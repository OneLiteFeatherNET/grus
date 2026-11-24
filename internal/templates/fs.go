package templates

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	identRe  = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

const (
	dirPerm  = 0o755
	filePerm = 0o644
)

// FS is a Store that keeps blobs, manifests and tags in a directory tree:
//
//	blobs/sha256/<hex>
//	manifests/<name>/<hex>.json
//	tags/<name>/<tag>
type FS struct {
	root string
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
	return &FS{root: root}, nil
}

// ImportTarGz stores the regular files of a tar.gz stream as a new version of name.
func (s *FS) ImportTarGz(ctx context.Context, name string, r io.Reader) (Version, error) {
	if err := validIdent("template name", name); err != nil {
		return Version{}, err
	}
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return Version{}, fmt.Errorf("templates: open gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	var entries []ManifestEntry
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return Version{}, fmt.Errorf("templates: read tar: %w", err)
		}
		p, err := cleanArchivePath(hdr.Name)
		if err != nil {
			return Version{}, err
		}
		isDir, err := checkEntryType(hdr)
		if err != nil {
			return Version{}, err
		}
		if isDir {
			continue
		}
		if p == "" {
			return Version{}, fmt.Errorf("templates: entry %q has no file name", hdr.Name)
		}
		e, err := s.putFile(tr, p, hdr.Mode)
		if err != nil {
			return Version{}, err
		}
		entries = append(entries, e)
	}
	return s.commit(name, entries)
}

// ImportDir imports a directory tree. Kubernetes ConfigMap volumes are handled: entries whose name
// starts with ".." (the ..data link and the ..<timestamp> directories) are skipped, symlinks to
// regular files are followed, symlinks to directories are skipped.
func (s *FS) ImportDir(ctx context.Context, name, dir string) (Version, error) {
	if err := validIdent("template name", name); err != nil {
		return Version{}, err
	}
	if err := ctx.Err(); err != nil {
		return Version{}, err
	}
	var entries []ManifestEntry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dir {
			return nil
		}
		if strings.HasPrefix(d.Name(), "..") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		entryPath, err := cleanArchivePath(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		info, err := os.Stat(p) // follows symlinks
		if err != nil {
			return fmt.Errorf("templates: stat %s: %w", entryPath, err)
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("templates: unsupported file %s (%s)", entryPath, info.Mode().Type())
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		e, err := s.putFile(f, entryPath, int64(info.Mode().Perm()))
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		entries = append(entries, e)
		return nil
	})
	if err != nil {
		return Version{}, fmt.Errorf("templates: import dir: %w", err)
	}
	return s.commit(name, entries)
}

// Tag points tag at an existing version of name.
func (s *FS) Tag(ctx context.Context, name, tag, digest string) error {
	if err := validIdent("template name", name); err != nil {
		return err
	}
	if err := validIdent("tag", tag); err != nil {
		return err
	}
	if err := s.checkVersion(name, digest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(s.root, "tags", name), dirPerm); err != nil {
		return err
	}
	return writeFileAtomic(s.tagPath(name, tag), []byte(digest))
}

// Resolve returns the digest for a tag, or checks and returns a digest argument.
func (s *FS) Resolve(ctx context.Context, name, tagOrDigest string) (string, error) {
	if err := validIdent("template name", name); err != nil {
		return "", err
	}
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		if err := s.checkVersion(name, tagOrDigest); err != nil {
			return "", err
		}
		return tagOrDigest, nil
	}
	if err := validIdent("tag", tagOrDigest); err != nil {
		return "", err
	}
	b, err := os.ReadFile(s.tagPath(name, tagOrDigest))
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: tag %s of %s", ErrNotFound, tagOrDigest, name)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// List returns every template name with its tags (tag -> digest).
func (s *FS) List(ctx context.Context) (map[string]map[string]string, error) {
	out := map[string]map[string]string{}
	names, err := os.ReadDir(filepath.Join(s.root, "tags"))
	if err != nil {
		return nil, err
	}
	for _, n := range names {
		if !n.IsDir() || !identRe.MatchString(n.Name()) {
			continue
		}
		tags, err := os.ReadDir(filepath.Join(s.root, "tags", n.Name()))
		if err != nil {
			return nil, err
		}
		m := map[string]string{}
		for _, t := range tags {
			if t.IsDir() || !identRe.MatchString(t.Name()) {
				continue
			}
			b, err := os.ReadFile(s.tagPath(n.Name(), t.Name()))
			if err != nil {
				return nil, err
			}
			m[t.Name()] = strings.TrimSpace(string(b))
		}
		if len(m) > 0 {
			out[n.Name()] = m
		}
	}
	return out, nil
}

// WriteTarGz writes a version as a deterministic tar.gz: sorted regular files, epoch mtime, uid/gid 0.
func (s *FS) WriteTarGz(ctx context.Context, name, digest string, w io.Writer) error {
	if err := validIdent("template name", name); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m, err := s.readManifest(name, digest)
	if err != nil {
		return err
	}
	files := append([]ManifestEntry(nil), m.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		blob, err := os.Open(s.blobPath(f.SHA256))
		if err != nil {
			return fmt.Errorf("templates: open blob for %s: %w", f.Path, err)
		}
		hdr := &tar.Header{
			Name:     f.Path,
			Mode:     int64(f.Mode),
			Size:     f.Size,
			Typeflag: tar.TypeReg,
			ModTime:  time.Unix(0, 0),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			blob.Close()
			return err
		}
		n, err := io.Copy(tw, blob)
		blob.Close()
		if err != nil {
			return fmt.Errorf("templates: write %s: %w", f.Path, err)
		}
		if n != f.Size {
			return fmt.Errorf("templates: blob for %s has %d bytes, manifest says %d", f.Path, n, f.Size)
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

// putFile stores r as a blob and returns its manifest entry.
func (s *FS) putFile(r io.Reader, p string, mode int64) (ManifestEntry, error) {
	sum, size, err := s.putBlob(r)
	if err != nil {
		return ManifestEntry{}, err
	}
	return ManifestEntry{Path: p, SHA256: sum, Mode: entryMode(mode), Size: size}, nil
}

// putBlob streams r into a temp file, hashes it and renames it to blobs/sha256/<hex> unless that blob exists.
func (s *FS) putBlob(r io.Reader) (string, int64, error) {
	dir := filepath.Join(s.root, "blobs", "sha256")
	tmp, err := os.CreateTemp(dir, ".blob-*")
	if err != nil {
		return "", 0, fmt.Errorf("templates: create temp blob: %w", err)
	}
	defer os.Remove(tmp.Name())

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if err == nil {
		err = tmp.Chmod(filePerm)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, fmt.Errorf("templates: write blob: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	dst := s.blobPath(sum)
	if _, err := os.Stat(dst); err == nil {
		return sum, size, nil
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return "", 0, fmt.Errorf("templates: store blob: %w", err)
	}
	return sum, size, nil
}

// commit writes the manifest for entries (sorted by path) and returns the version.
func (s *FS) commit(name string, entries []ManifestEntry) (Version, error) {
	if entries == nil {
		entries = []ManifestEntry{}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	var size int64
	for i, e := range entries {
		if i > 0 && entries[i-1].Path == e.Path {
			return Version{}, fmt.Errorf("templates: duplicate path %q", e.Path)
		}
		size += e.Size
	}
	raw, err := json.Marshal(Manifest{Files: entries})
	if err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256(raw)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	mp := s.manifestPath(name, digest)
	if _, err := os.Stat(mp); errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(mp), dirPerm); err != nil {
			return Version{}, err
		}
		if err := writeFileAtomic(mp, raw); err != nil {
			return Version{}, err
		}
	}
	return Version{Name: name, Digest: digest, Size: size}, nil
}

func (s *FS) readManifest(name, digest string) (Manifest, error) {
	if err := s.checkVersion(name, digest); err != nil {
		return Manifest{}, err
	}
	raw, err := os.ReadFile(s.manifestPath(name, digest))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return Manifest{}, fmt.Errorf("templates: decode manifest %s: %w", digest, err)
	}
	return m, nil
}

// checkVersion validates the digest format and reports ErrNotFound when the manifest is missing.
func (s *FS) checkVersion(name, digest string) error {
	if !digestRe.MatchString(digest) {
		return fmt.Errorf("templates: invalid digest %q", digest)
	}
	_, err := os.Stat(s.manifestPath(name, digest))
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: version %s of %s", ErrNotFound, digest, name)
	}
	return err
}

func (s *FS) blobPath(hexSum string) string {
	return filepath.Join(s.root, "blobs", "sha256", hexSum)
}

func (s *FS) manifestPath(name, digest string) string {
	return filepath.Join(s.root, "manifests", name, strings.TrimPrefix(digest, "sha256:")+".json")
}

func (s *FS) tagPath(name, tag string) string {
	return filepath.Join(s.root, "tags", name, tag)
}

// writeFileAtomic writes data to a temp file next to path and renames it into place.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Chmod(filePerm)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func validIdent(kind, s string) error {
	if !identRe.MatchString(s) {
		return fmt.Errorf("templates: invalid %s %q", kind, s)
	}
	return nil
}
