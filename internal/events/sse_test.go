package events

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// fakeTicker lets a test fire heartbeats by sending on C, without waiting for a real interval.
type fakeTicker struct{ c chan time.Time }

func (f *fakeTicker) C() <-chan time.Time { return f.c }
func (f *fakeTicker) Stop()               {}

// sseStream reads complete SSE messages (up to and including the blank line) from a response body in a
// goroutine and hands them over on a channel.
type sseStream struct {
	t    *testing.T
	msgs chan string
}

func (s *sseStream) next() string {
	s.t.Helper()
	msg, ok := <-s.msgs
	if !ok {
		s.t.Fatalf("stream ended before the next message was read")
	}
	return msg
}

// openStream starts the handler behind httptest and returns once the response headers arrived. The stream
// is cancelled and the server closed when the test ends.
func openStream(t *testing.T, h *Hub, query, lastEventID string) (*sseStream, *http.Response, *fakeTicker) {
	t.Helper()
	ft := &fakeTicker{c: make(chan time.Time)}
	h.newTicker = func(time.Duration) ticker { return ft }

	srv := httptest.NewServer(h.Handler(15 * time.Second))
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/?"+query, nil)
	if err != nil {
		t.Fatalf("building request: %v", err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("opening stream: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })

	msgs := make(chan string, 64)
	go func() {
		defer close(msgs)
		br := bufio.NewReader(resp.Body)
		var msg strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			msg.WriteString(line)
			if line == "\n" {
				msgs <- msg.String()
				msg.Reset()
			}
		}
	}()
	return &sseStream{t: t, msgs: msgs}, resp, ft
}

func sseMessage(id, eventType, data string) string {
	return fmt.Sprintf("id: %s\nevent: %s\ndata: %s\n\n", id, eventType, data)
}

func TestHandlerSetsSSEHeaders(t *testing.T) {
	h := newTestHub(10)
	_, resp, _ := openStream(t, h, "", "")

	checks := map[string]string{
		"Content-Type":      "text/event-stream",
		"Cache-Control":     "no-cache",
		"X-Accel-Buffering": "no",
	}
	for name, want := range checks {
		if got := resp.Header.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
}

func TestHandlerWritesResyncThenLiveEventsInSSEFormat(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)

	s, _, _ := openStream(t, h, "", "e0-5")

	wantResync := sseMessage("e1-2", model.EventResync, `{"reason":"last-event-id cannot be replayed"}`)
	if got := s.next(); got != wantResync {
		t.Fatalf("resync message = %q, want %q", got, wantResync)
	}

	h.Publish(model.EventInstanceState, map[string]string{"instance": "lobby-1"})

	wantLive := sseMessage("e1-3", model.EventInstanceState, `{"instance":"lobby-1"}`)
	if got := s.next(); got != wantLive {
		t.Fatalf("live message = %q, want %q", got, wantLive)
	}
}

func TestHandlerWritesBacklogBeforeLiveEvents(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 3)

	s, _, _ := openStream(t, h, "", "e1-1")

	if got, want := s.next(), sseMessage("e1-2", model.EventInstanceState, `{"n":1}`); got != want {
		t.Fatalf("first backlog message = %q, want %q", got, want)
	}
	if got, want := s.next(), sseMessage("e1-3", model.EventInstanceState, `{"n":2}`); got != want {
		t.Fatalf("second backlog message = %q, want %q", got, want)
	}

	h.Publish(model.EventInstanceState, map[string]int{"n": 3})
	if got, want := s.next(), sseMessage("e1-4", model.EventInstanceState, `{"n":3}`); got != want {
		t.Fatalf("live message after backlog = %q, want %q", got, want)
	}
}

func TestHandlerUsesLastEventIDQueryParameterAsFallback(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)

	s, _, _ := openStream(t, h, "lastEventId=e1-1", "")

	if got, want := s.next(), sseMessage("e1-2", model.EventInstanceState, `{"n":1}`); got != want {
		t.Fatalf("message = %q, want backlog event %q", got, want)
	}
}

func TestHandlerFiltersByTypeAndPool(t *testing.T) {
	h := newTestHub(10)
	s, _, _ := openStream(t, h, "types=instance.*&pool=lobby", "")

	h.Publish(model.EventInstanceCreated, map[string]string{"pool": "lobby"})  // delivered
	h.Publish(model.EventPoolScaled, map[string]string{"pool": "lobby"})       // wrong type
	h.Publish(model.EventInstanceState, map[string]string{"pool": "survival"}) // wrong pool
	h.Publish(model.EventInstanceState, map[string]string{"pool": "lobby"})    // delivered

	if got, want := s.next(), sseMessage("e1-1", model.EventInstanceCreated, `{"pool":"lobby"}`); got != want {
		t.Fatalf("first filtered message = %q, want %q", got, want)
	}
	if got, want := s.next(), sseMessage("e1-4", model.EventInstanceState, `{"pool":"lobby"}`); got != want {
		t.Fatalf("second filtered message = %q, want %q", got, want)
	}
}

func TestHandlerSendsHeartbeatPing(t *testing.T) {
	h := newTestHub(10)
	s, _, ft := openStream(t, h, "", "")

	ft.c <- time.Time{}

	if got, want := s.next(), ": ping\n\n"; got != want {
		t.Fatalf("heartbeat = %q, want %q", got, want)
	}
}

func TestHandlerRejectsNonGETMethods(t *testing.T) {
	h := newTestHub(10)
	rec := httptest.NewRecorder()
	h.Handler(time.Second).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}
