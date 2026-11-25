package client

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.Handler) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return New(srv.URL, "secret", srv.Client())
}

func TestListSendsBearerTokenAndQuery(t *testing.T) {
	var gotAuth, gotQuery string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotQuery = r.Header.Get("Authorization"), r.URL.RawQuery
		_, _ = w.Write([]byte(`[{"id":"a"}]`))
	}))
	got, err := c.List(context.Background(), "instances", map[string][]string{"pool": {"lobby"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["id"] != "a" {
		t.Errorf("List = %v", got)
	}
	if gotAuth != "Bearer secret" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotQuery != "pool=lobby" {
		t.Errorf("query = %q", gotQuery)
	}
}

func TestProblemDocumentBecomesAPIError(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"title":"Unprocessable","detail":"invalid spec","errors":[{"message":"must be >= 0","location":"body.scaling.min"}]}`))
	}))
	_, err := c.Put(context.Background(), "pools", "lobby", map[string]any{})
	var ae *APIError
	if !errors.As(err, &ae) || ae.Status != 422 {
		t.Fatalf("err = %v, want APIError 422", err)
	}
	if !strings.Contains(err.Error(), "body.scaling.min: must be >= 0") {
		t.Errorf("message = %q lacks the field error", err)
	}
}

func TestMissingOptionalListIsNotSupported(t *testing.T) {
	c := newTestClient(t, http.NotFoundHandler())
	for _, res := range []string{"groups", "environments", "hosts"} {
		_, err := c.List(context.Background(), res, nil)
		var ns *NotSupportedError
		if !errors.As(err, &ns) {
			t.Errorf("List(%s) err = %v, want NotSupportedError", res, err)
		}
	}
	_, err := c.Network(context.Background())
	var ns *NotSupportedError
	if !errors.As(err, &ns) || !strings.Contains(err.Error(), "not supported by this controller") {
		t.Errorf("Network err = %v", err)
	}
}

func TestMissingPoolIsPlainNotFound(t *testing.T) {
	c := newTestClient(t, http.NotFoundHandler())
	_, err := c.List(context.Background(), "pools", nil)
	var ns *NotSupportedError
	if errors.As(err, &ns) {
		t.Fatalf("pools must never be reported as unsupported: %v", err)
	}
}

func TestGetUnknownGroupOnSupportingControllerIsNotFound(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/groups", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`[]`)) })
	c := newTestClient(t, mux)
	_, err := c.Get(context.Background(), "groups", "nope")
	var ns *NotSupportedError
	if errors.As(err, &ns) || !isStatus(err, 404) {
		t.Fatalf("err = %v, want plain 404", err)
	}
}

func TestPutGroupOnOldControllerIsNotSupported(t *testing.T) {
	c := newTestClient(t, http.NotFoundHandler())
	_, err := c.Put(context.Background(), "groups", "g", map[string]any{})
	var ns *NotSupportedError
	if !errors.As(err, &ns) {
		t.Fatalf("err = %v, want NotSupportedError", err)
	}
}

func TestStopSendsOptionsOnlyWhenSet(t *testing.T) {
	var bodies []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		_, _ = w.Write([]byte(`{"id":"i-1"}`))
	}))
	if _, err := c.StopInstance(context.Background(), "i-1", false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.StopInstance(context.Background(), "i-1", true, false); err != nil {
		t.Fatal(err)
	}
	if bodies[0] != "" || !strings.Contains(bodies[1], `"drain":true`) {
		t.Errorf("bodies = %q", bodies)
	}
}

func TestCommandPostsLine(t *testing.T) {
	var got string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = r.URL.Path + " " + string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := c.Command(context.Background(), "i-1", "say hi"); err != nil {
		t.Fatal(err)
	}
	if got != `/v1/instances/i-1/command {"line":"say hi"}` {
		t.Errorf("got %q", got)
	}
}

func TestLogsReturnsText(t *testing.T) {
	var q string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q = r.URL.RawQuery
		_, _ = w.Write([]byte("line1\nline2\n"))
	}))
	rc, err := c.Logs(context.Background(), "i-1", 50)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	if string(b) != "line1\nline2\n" || q != "tail=50" {
		t.Errorf("body = %q query = %q", b, q)
	}
}

func TestFollowLogsDeliversEveryDataLine(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: first\n\ndata: second\n\n")
	}))
	var lines []string
	err := c.FollowLogs(context.Background(), "i-1", func(l string) error { lines = append(lines, l); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(lines, "|") != "first|second" {
		t.Errorf("lines = %v", lines)
	}
}

func TestEventsParsesIDTypeAndResumes(t *testing.T) {
	var last string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = r.Header.Get("Last-Event-ID")
		_, _ = io.WriteString(w, ": hello\n\nid: 1-2\nevent: pool.updated\ndata: {\"pool\":\"lobby\"}\n\n")
	}))
	var got []Event
	err := c.Events(context.Background(), "1-1", func(e Event) error { got = append(got, e); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "1-2" || got[0].Type != "pool.updated" || string(got[0].Data) != `{"pool":"lobby"}` {
		t.Errorf("events = %+v", got)
	}
	if last != "1-1" {
		t.Errorf("Last-Event-ID = %q", last)
	}
}

func TestTemplates(t *testing.T) {
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"paper":{"stable":"sha256:ab"}}`))
	}))
	got, err := c.Templates(context.Background())
	if err != nil || got["paper"]["stable"] != "sha256:ab" {
		t.Errorf("got %v err %v", got, err)
	}
}
