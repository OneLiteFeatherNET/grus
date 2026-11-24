package templates

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"

func noSleep(time.Duration) {}

// serveTemplates serves archives from store at /v1/templates/{name}/versions/{digest}/archive and
// requires the bearer token.
func serveTemplates(t *testing.T, store *FS, token string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/v1/templates/"), "/")
		if len(parts) != 4 || parts[1] != "versions" || parts[3] != "archive" {
			http.NotFound(w, r)
			return
		}
		var buf bytes.Buffer
		if err := store.WriteTarGz(r.Context(), parts[0], parts[2], &buf); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		_, _ = w.Write(buf.Bytes())
	}))
}

func TestFetchOverlaysTemplatesInOrderWithBearerToken(t *testing.T) {
	store := newFS(t)
	base := importTar(t, store, "base", tarGz(t,
		testEntry{name: "a.txt", body: "base"},
		testEntry{name: "b.txt", body: "base"},
	))
	override := importTar(t, store, "override", tarGz(t, testEntry{name: "a.txt", body: "override"}))
	srv := serveTemplates(t, store, "s3cret")
	defer srv.Close()
	dest := t.TempDir()

	err := Fetch(context.Background(), FetchOptions{
		BaseURL:   srv.URL,
		Token:     "s3cret",
		Dest:      dest,
		Templates: []Ref{{Name: "base", Digest: base.Digest}, {Name: "override", Digest: override.Digest}},
		Client:    srv.Client(),
		Sleep:     noSleep,
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	assertFile(t, filepath.Join(dest, "a.txt"), "override", 0o644)
	assertFile(t, filepath.Join(dest, "b.txt"), "base", 0o644)
}

func TestFetchRetriesAfterServerError(t *testing.T) {
	store := newFS(t)
	v := importTar(t, store, "server", tarGz(t, testEntry{name: "a.txt", body: "alpha"}))
	var hits, sleeps atomic.Int32
	ok := serveTemplates(t, store, "tok")
	defer ok.Close()
	flaky := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hits.Add(1) == 1 {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		ok.Config.Handler.ServeHTTP(w, r)
	}))
	defer flaky.Close()
	dest := t.TempDir()

	err := Fetch(context.Background(), FetchOptions{
		BaseURL:   flaky.URL,
		Token:     "tok",
		Dest:      dest,
		Templates: []Ref{{Name: "server", Digest: v.Digest}},
		Client:    flaky.Client(),
		Sleep:     func(time.Duration) { sleeps.Add(1) },
	})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server hits = %d, want 2 (one failure, one success)", got)
	}
	if got := sleeps.Load(); got != 1 {
		t.Fatalf("backoff sleeps = %d, want 1", got)
	}
	assertFile(t, filepath.Join(dest, "a.txt"), "alpha", 0o644)
}

func TestFetchReportsStatusAndBodyWhenRetriesAreExhausted(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "template not found", http.StatusNotFound)
	}))
	defer srv.Close()

	err := Fetch(context.Background(), FetchOptions{
		BaseURL:   srv.URL,
		Token:     "tok",
		Dest:      t.TempDir(),
		Templates: []Ref{{Name: "server", Digest: testDigest}},
		Client:    srv.Client(),
		Sleep:     noSleep,
	})
	if err == nil {
		t.Fatal("Fetch succeeded against a 404 server, want an error")
	}
	if !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "template not found") {
		t.Fatalf("error %q should contain the status and the response body", err)
	}
	if got := hits.Load(); got != fetchAttempts {
		t.Fatalf("server hits = %d, want %d attempts", got, fetchAttempts)
	}
}

func TestParseRefsReadsCommaSeparatedNameDigestPairs(t *testing.T) {
	got, err := ParseRefs("base@" + testDigest + ", server@" + testDigest)
	if err != nil {
		t.Fatalf("ParseRefs: %v", err)
	}
	want := []Ref{{Name: "base", Digest: testDigest}, {Name: "server", Digest: testDigest}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParseRefs = %v, want %v", got, want)
	}
}

func TestParseRefsRejectsMalformedEntries(t *testing.T) {
	cases := map[string]string{
		"missing digest": "base",
		"bad name":       "Base@" + testDigest,
		"bad digest":     "base@md5:abc",
		"empty entry":    "base@" + testDigest + ",",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRefs(input); err == nil {
				t.Fatalf("ParseRefs(%q) accepted malformed input", input)
			}
		})
	}
}

func TestParseRefsEmptyStringYieldsNoRefs(t *testing.T) {
	got, err := ParseRefs("")
	if err != nil {
		t.Fatalf("ParseRefs(\"\"): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ParseRefs(\"\") = %v, want no refs", got)
	}
}
