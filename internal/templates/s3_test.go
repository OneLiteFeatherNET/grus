package templates

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/minio/minio-go/v7"
)

// fakeObjects is an in-memory objectClient. It counts uploads per key so tests can prove that a blob
// is not uploaded twice.
type fakeObjects struct {
	mu      sync.Mutex
	objects map[string][]byte
	puts    map[string]int
	failPut error
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{objects: map[string][]byte{}, puts: map[string]int{}}
}

func (f *fakeObjects) Stat(_ context.Context, key string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[key]
	return ok, nil
}

func (f *fakeObjects) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failPut != nil {
		return f.failPut
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	f.objects[key] = b
	f.puts[key]++
	return nil
}

func (f *fakeObjects) Get(_ context.Context, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.objects[key]
	if !ok {
		return nil, errNoObject
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (f *fakeObjects) List(_ context.Context, prefix string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var keys []string
	for k := range f.objects {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys, nil
}

func (f *fakeObjects) putCount(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.puts[key]
}

func (f *fakeObjects) keysWithPrefix(prefix string) []string {
	keys, _ := f.List(context.Background(), prefix)
	return keys
}

// newFakeS3 returns an S3 store on an in-memory bucket, optionally under a prefix.
func newFakeS3(t *testing.T, prefix string) (*S3, *fakeObjects) {
	t.Helper()
	fake := newFakeObjects()
	return newS3(fake, prefix), fake
}

func TestS3StoreSuite(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store {
		s, _ := newFakeS3(t, "")
		return s
	})
}

func TestS3StoreSuiteUnderPrefix(t *testing.T) {
	runStoreSuite(t, func(t *testing.T) Store {
		s, _ := newFakeS3(t, "envs/dev/templates")
		return s
	})
}

func TestS3ImportDoesNotUploadKnownBlobAgain(t *testing.T) {
	s, fake := newFakeS3(t, "")
	shared := testEntry{name: "shared.jar", body: "same bytes"}
	importArchive(t, s, "server", tarGz(t, shared, testEntry{name: "a.txt", body: "alpha"}))
	importArchive(t, s, "proxy", tarGz(t, shared, testEntry{name: "b.txt", body: "beta"}))

	sharedKey := blobKey(sha256Hex("same bytes"))
	if n := fake.putCount(sharedKey); n != 1 {
		t.Fatalf("shared blob uploaded %d times, want 1", n)
	}
}

func TestS3ImportWritesLayoutKeys(t *testing.T) {
	s, fake := newFakeS3(t, "")
	v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	if err := s.Tag(context.Background(), "server", "stable", v.Digest); err != nil {
		t.Fatalf("Tag: %v", err)
	}

	hex := strings.TrimPrefix(v.Digest, "sha256:")
	for _, key := range []string{
		blobKey(sha256Hex("alpha")),
		"manifests/server/" + hex + ".json",
		"tags/server/stable",
	} {
		if n := fake.putCount(key); n != 1 {
			t.Errorf("key %s written %d times, want 1", key, n)
		}
	}
}

func TestS3KeysAreUnderConfiguredPrefix(t *testing.T) {
	s, fake := newFakeS3(t, "envs/dev")
	v := importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	if err := s.Tag(context.Background(), "server", "stable", v.Digest); err != nil {
		t.Fatalf("Tag: %v", err)
	}

	all := fake.keysWithPrefix("")
	if len(all) == 0 {
		t.Fatal("no objects were written")
	}
	for _, key := range all {
		if !strings.HasPrefix(key, "envs/dev/") {
			t.Errorf("object %q written outside the prefix envs/dev/", key)
		}
	}
}

func TestS3UploadErrorIsReturned(t *testing.T) {
	s, fake := newFakeS3(t, "")
	fake.failPut = errors.New("bucket unavailable")

	_, err := s.ImportTarGz(context.Background(), "server", bytes.NewReader(tarGz(t, testEntry{name: "a.txt", body: "alpha"})))
	if !errors.Is(err, fake.failPut) {
		t.Fatalf("ImportTarGz error = %v, want the upload error", err)
	}
}

func TestS3MissingTagIsNotFound(t *testing.T) {
	s, _ := newFakeS3(t, "")
	importArchive(t, s, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	if _, err := s.Resolve(context.Background(), "server", "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Resolve(missing) = %v, want ErrNotFound", err)
	}
}

func TestNewS3RejectsIncompleteConfig(t *testing.T) {
	valid := S3Config{Endpoint: "minio.local:9000", Bucket: "grus", AccessKey: "ak", SecretKey: "sk"}
	cases := map[string]func(c *S3Config){
		"missing endpoint":   func(c *S3Config) { c.Endpoint = "" },
		"missing bucket":     func(c *S3Config) { c.Bucket = "" },
		"missing access key": func(c *S3Config) { c.AccessKey = "" },
		"missing secret key": func(c *S3Config) { c.SecretKey = "" },
		"path in endpoint":   func(c *S3Config) { c.Endpoint = "https://minio.local/base" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := valid
			mutate(&c)
			if _, err := NewS3(c); err == nil {
				t.Fatalf("NewS3 accepted %s", name)
			}
		})
	}
}

func TestParseS3Endpoint(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		insecure bool
		host     string
		secure   bool
		wantErr  bool
	}{
		{name: "bare host is TLS by default", endpoint: "s3.local:9000", host: "s3.local:9000", secure: true},
		{name: "bare host with insecure is plain HTTP", endpoint: "minio.local:9000", insecure: true, host: "minio.local:9000"},
		{name: "http scheme is plain HTTP", endpoint: "http://minio.local:9000", host: "minio.local:9000"},
		{name: "https scheme is TLS", endpoint: "https://s3.local", host: "s3.local", secure: true},
		{name: "https scheme with insecure is rejected", endpoint: "https://s3.local", insecure: true, wantErr: true},
		{name: "unknown scheme is rejected", endpoint: "ftp://s3.local", wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, secure, err := parseS3Endpoint(c.endpoint, c.insecure)
			if c.wantErr {
				if err == nil {
					t.Fatalf("parseS3Endpoint(%q) accepted the endpoint", c.endpoint)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseS3Endpoint(%q): %v", c.endpoint, err)
			}
			if host != c.host || secure != c.secure {
				t.Fatalf("parseS3Endpoint(%q) = (%q, %v), want (%q, %v)", c.endpoint, host, secure, c.host, c.secure)
			}
		})
	}
}

func TestIsNoSuchKey(t *testing.T) {
	cases := map[string]bool{
		"NoSuchKey":    true,
		"NotFound":     true,
		"AccessDenied": false,
	}
	for code, want := range cases {
		err := minio.ErrorResponse{Code: code}
		if got := isNoSuchKey(err); got != want {
			t.Errorf("isNoSuchKey(%s) = %v, want %v", code, got, want)
		}
	}
	if isNoSuchKey(errors.New("connection refused")) {
		t.Error("isNoSuchKey(plain error) = true, want false")
	}
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
