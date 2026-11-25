package templates

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures NewS3.
type S3Config struct {
	// Endpoint is host:port or a URL with an http or https scheme. A URL scheme decides TLS.
	Endpoint string
	Bucket   string
	// Prefix is an optional key prefix inside the bucket; the layout lives below it.
	Prefix string
	// Insecure selects plain HTTP for a bare host:port endpoint. It is rejected together with https://.
	Insecure  bool
	AccessKey string
	SecretKey string
}

// S3 is a Store that keeps the shared layout (see layout.go) in an S3 bucket.
type S3 struct {
	layout
}

var _ Store = (*S3)(nil)

// NewS3 connects to the bucket described by cfg. It does not contact the server.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Bucket == "" {
		return nil, errors.New("templates: s3 bucket is required")
	}
	if cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("templates: s3 access key and secret key are required")
	}
	host, secure, err := parseS3Endpoint(cfg.Endpoint, cfg.Insecure)
	if err != nil {
		return nil, err
	}
	client, err := minio.New(host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: secure,
	})
	if err != nil {
		return nil, fmt.Errorf("templates: s3 client: %w", err)
	}
	return newS3(&minioObjects{client: client, bucket: cfg.Bucket}, cfg.Prefix), nil
}

// newS3 builds the store on any objectClient; the prefix is applied to every key.
func newS3(objects objectClient, prefix string) *S3 {
	return &S3{layout: layout{objects: prefixedObjects{inner: objects, prefix: normalizePrefix(prefix)}}}
}

// parseS3Endpoint returns the host:port for the minio client and whether to use TLS.
func parseS3Endpoint(endpoint string, insecure bool) (string, bool, error) {
	if endpoint == "" {
		return "", false, errors.New("templates: s3 endpoint is required")
	}
	if !strings.Contains(endpoint, "://") {
		return endpoint, !insecure, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return "", false, fmt.Errorf("templates: s3 endpoint: %w", err)
	}
	if u.Path != "" && u.Path != "/" {
		return "", false, fmt.Errorf("templates: s3 endpoint %q must not contain a path", endpoint)
	}
	switch u.Scheme {
	case "https":
		if insecure {
			return "", false, errors.New("templates: s3 endpoint is https but GRUS_S3_INSECURE is true")
		}
		return u.Host, true, nil
	case "http":
		return u.Host, false, nil
	default:
		return "", false, fmt.Errorf("templates: s3 endpoint scheme %q is not http or https", u.Scheme)
	}
}

// isNoSuchKey reports whether err is the S3 answer for a missing object.
func isNoSuchKey(err error) bool {
	code := minio.ToErrorResponse(err).Code
	return code == "NoSuchKey" || code == "NotFound"
}

func normalizePrefix(prefix string) string {
	p := strings.Trim(prefix, "/")
	if p == "" {
		return ""
	}
	return p + "/"
}

// prefixedObjects places every key below prefix ("" or "name/").
type prefixedObjects struct {
	inner  objectClient
	prefix string
}

func (p prefixedObjects) Stat(ctx context.Context, key string) (bool, error) {
	return p.inner.Stat(ctx, p.prefix+key)
}

func (p prefixedObjects) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	return p.inner.Put(ctx, p.prefix+key, r, size)
}

func (p prefixedObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	return p.inner.Get(ctx, p.prefix+key)
}

func (p prefixedObjects) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := p.inner.List(ctx, p.prefix+prefix)
	if err != nil {
		return nil, err
	}
	for i, k := range keys {
		keys[i] = strings.TrimPrefix(k, p.prefix)
	}
	return keys, nil
}

// minioObjects implements objectClient on an S3 bucket with the minio-go client.
type minioObjects struct {
	client *minio.Client
	bucket string
}

func (m *minioObjects) Stat(ctx context.Context, key string) (bool, error) {
	_, err := m.client.StatObject(ctx, m.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if isNoSuchKey(err) {
		return false, nil
	}
	return false, fmt.Errorf("templates: stat %s: %w", key, err)
}

func (m *minioObjects) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	_, err := m.client.PutObject(ctx, m.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: "application/octet-stream",
	})
	if err != nil {
		return fmt.Errorf("templates: put %s: %w", key, err)
	}
	return nil
}

func (m *minioObjects) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := m.client.GetObject(ctx, m.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("templates: get %s: %w", key, err)
	}
	// GetObject is lazy: Stat performs the request and reports a missing key.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if isNoSuchKey(err) {
			return nil, errNoObject
		}
		return nil, fmt.Errorf("templates: get %s: %w", key, err)
	}
	return obj, nil
}

func (m *minioObjects) List(ctx context.Context, prefix string) ([]string, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var keys []string
	for obj := range m.client.ListObjects(ctx, m.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("templates: list %s: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	return keys, nil
}
