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

// errNoObject is returned by objectClient.Get for a missing key.
var errNoObject = errors.New("templates: no such object")

// objectClient is the object storage the layout is built on. Keys are slash separated and relative to
// the store. Implementations: a directory tree (fs) and an S3 bucket (s3).
type objectClient interface {
	// Stat reports whether key exists.
	Stat(ctx context.Context, key string) (bool, error)
	// Put stores size bytes read from r under key, replacing an existing object.
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	// Get opens key. A missing key yields errNoObject.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// List returns every key that starts with prefix, recursively.
	List(ctx context.Context, prefix string) ([]string, error)
}

// layout implements Store on top of an objectClient. It owns the layout shared by all backends:
//
//	blobs/sha256/<hex>
//	manifests/<name>/<hex>.json
//	tags/<name>/<tag>
type layout struct {
	objects  objectClient
	spoolDir string // directory for the temporary copy of an imported file ("" = os temp dir)
}

// ImportTarGz stores the regular files of a tar.gz stream as a new version of name.
func (l *layout) ImportTarGz(ctx context.Context, name string, r io.Reader) (Version, error) {
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
		e, err := l.putFile(ctx, tr, p, hdr.Mode)
		if err != nil {
			return Version{}, err
		}
		entries = append(entries, e)
	}
	return l.commit(ctx, name, entries)
}

// ImportDir imports a directory tree. Kubernetes ConfigMap volumes are handled: entries whose name
// starts with ".." (the ..data link and the ..<timestamp> directories) are skipped, symlinks to
// regular files are followed, symlinks to directories are skipped.
func (l *layout) ImportDir(ctx context.Context, name, dir string) (Version, error) {
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
		e, err := l.putFile(ctx, f, entryPath, int64(info.Mode().Perm()))
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
	return l.commit(ctx, name, entries)
}

// Tag points tag at an existing version of name.
func (l *layout) Tag(ctx context.Context, name, tag, digest string) error {
	if err := validIdent("template name", name); err != nil {
		return err
	}
	if err := validIdent("tag", tag); err != nil {
		return err
	}
	if err := l.checkVersion(ctx, name, digest); err != nil {
		return err
	}
	return l.objects.Put(ctx, tagKey(name, tag), strings.NewReader(digest), int64(len(digest)))
}

// Resolve returns the digest for a tag, or checks and returns a digest argument.
func (l *layout) Resolve(ctx context.Context, name, tagOrDigest string) (string, error) {
	if err := validIdent("template name", name); err != nil {
		return "", err
	}
	if strings.HasPrefix(tagOrDigest, "sha256:") {
		if err := l.checkVersion(ctx, name, tagOrDigest); err != nil {
			return "", err
		}
		return tagOrDigest, nil
	}
	if err := validIdent("tag", tagOrDigest); err != nil {
		return "", err
	}
	b, err := l.readObject(ctx, tagKey(name, tagOrDigest))
	if errors.Is(err, errNoObject) {
		return "", fmt.Errorf("%w: tag %s of %s", ErrNotFound, tagOrDigest, name)
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// List returns every template name with its tags (tag -> digest).
func (l *layout) List(ctx context.Context) (map[string]map[string]string, error) {
	keys, err := l.objects.List(ctx, "tags/")
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]string{}
	for _, key := range keys {
		name, tag, ok := strings.Cut(strings.TrimPrefix(key, "tags/"), "/")
		if !ok || strings.Contains(tag, "/") || !identRe.MatchString(name) || !identRe.MatchString(tag) {
			continue
		}
		b, err := l.readObject(ctx, key)
		if err != nil {
			return nil, err
		}
		if out[name] == nil {
			out[name] = map[string]string{}
		}
		out[name][tag] = strings.TrimSpace(string(b))
	}
	return out, nil
}

// WriteTarGz writes a version as a deterministic tar.gz: sorted regular files, epoch mtime, uid/gid 0.
func (l *layout) WriteTarGz(ctx context.Context, name, digest string, w io.Writer) error {
	if err := validIdent("template name", name); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m, err := l.readManifest(ctx, name, digest)
	if err != nil {
		return err
	}
	files := append([]ManifestEntry(nil), m.Files...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	for _, f := range files {
		blob, err := l.objects.Get(ctx, blobKey(f.SHA256))
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
func (l *layout) putFile(ctx context.Context, r io.Reader, p string, mode int64) (ManifestEntry, error) {
	sum, size, err := l.putBlob(ctx, r)
	if err != nil {
		return ManifestEntry{}, err
	}
	return ManifestEntry{Path: p, SHA256: sum, Mode: entryMode(mode), Size: size}, nil
}

// putBlob spools r to a local temp file while hashing it, then uploads the file as blobs/sha256/<hex>
// unless that blob already exists. Existing blobs are never written again.
func (l *layout) putBlob(ctx context.Context, r io.Reader) (string, int64, error) {
	tmp, err := os.CreateTemp(l.spoolDir, ".spool-*")
	if err != nil {
		return "", 0, fmt.Errorf("templates: create temp blob: %w", err)
	}
	defer os.Remove(tmp.Name())

	h := sha256.New()
	size, err := io.Copy(io.MultiWriter(tmp, h), r)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", 0, fmt.Errorf("templates: write blob: %w", err)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	key := blobKey(sum)
	exists, err := l.objects.Stat(ctx, key)
	if err != nil {
		return "", 0, fmt.Errorf("templates: stat blob: %w", err)
	}
	if exists {
		return sum, size, nil
	}
	if err := l.putLocal(ctx, key, tmp.Name(), size); err != nil {
		return "", 0, fmt.Errorf("templates: store blob: %w", err)
	}
	return sum, size, nil
}

// putLocal uploads the local file at p under key.
func (l *layout) putLocal(ctx context.Context, key, p string, size int64) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return l.objects.Put(ctx, key, f, size)
}

// commit writes the manifest for entries (sorted by path) and returns the version.
func (l *layout) commit(ctx context.Context, name string, entries []ManifestEntry) (Version, error) {
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

	key := manifestKey(name, digest)
	exists, err := l.objects.Stat(ctx, key)
	if err != nil {
		return Version{}, err
	}
	if !exists {
		if err := l.objects.Put(ctx, key, strings.NewReader(string(raw)), int64(len(raw))); err != nil {
			return Version{}, err
		}
	}
	return Version{Name: name, Digest: digest, Size: size}, nil
}

func (l *layout) readManifest(ctx context.Context, name, digest string) (Manifest, error) {
	if err := l.checkVersion(ctx, name, digest); err != nil {
		return Manifest{}, err
	}
	raw, err := l.readObject(ctx, manifestKey(name, digest))
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
func (l *layout) checkVersion(ctx context.Context, name, digest string) error {
	if !digestRe.MatchString(digest) {
		return fmt.Errorf("templates: invalid digest %q", digest)
	}
	exists, err := l.objects.Stat(ctx, manifestKey(name, digest))
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: version %s of %s", ErrNotFound, digest, name)
	}
	return nil
}

// readObject returns the whole content of key.
func (l *layout) readObject(ctx context.Context, key string) ([]byte, error) {
	rc, err := l.objects.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func blobKey(hexSum string) string {
	return "blobs/sha256/" + hexSum
}

func manifestKey(name, digest string) string {
	return "manifests/" + name + "/" + strings.TrimPrefix(digest, "sha256:") + ".json"
}

func tagKey(name, tag string) string {
	return "tags/" + name + "/" + tag
}

func validIdent(kind, s string) error {
	if !identRe.MatchString(s) {
		return fmt.Errorf("templates: invalid %s %q", kind, s)
	}
	return nil
}
