// Package templates stores content-addressed, immutable template versions. A version is a manifest
// (sorted list of path, sha256, mode, size); its digest is "sha256:" + hex SHA-256 of the canonical
// manifest JSON. Tags point at digests. Backends: fs (directory) now, s3 later with the same layout:
//
//	blobs/sha256/<hex>
//	manifests/<name>/<hex>.json
//	tags/<name>/<tag>        (content: the digest)
package templates

import (
	"context"
	"errors"
	"io"
)

var ErrNotFound = errors.New("templates: not found")

// ManifestEntry is one regular file of a template version.
type ManifestEntry struct {
	Path   string `json:"path"` // slash separated, relative, no ".." segments
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
}

// Manifest lists the files of a version, sorted by Path. Canonical JSON = encoding/json of this struct.
type Manifest struct {
	Files []ManifestEntry `json:"files"`
}

// Version is a stored version of a template.
type Version struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
	Size   int64  `json:"size"`
}

// Store is the template service used by the controller API and the reconciler.
type Store interface {
	// ImportTarGz reads a tar.gz stream, stores missing blobs and the manifest, returns the digest.
	ImportTarGz(ctx context.Context, name string, r io.Reader) (Version, error)
	// ImportDir imports a local directory tree (used for the ConfigMap seed).
	ImportDir(ctx context.Context, name string, dir string) (Version, error)
	// Tag points tag at digest (digest must exist).
	Tag(ctx context.Context, name, tag, digest string) error
	// Resolve returns the digest for a tag; a "sha256:" argument is checked and returned as is.
	Resolve(ctx context.Context, name, tagOrDigest string) (string, error)
	// List returns all template names with their tags (tag -> digest).
	List(ctx context.Context) (map[string]map[string]string, error)
	// WriteTarGz writes the version as tar.gz (regular files only, with modes) to w.
	WriteTarGz(ctx context.Context, name, digest string, w io.Writer) error
}
