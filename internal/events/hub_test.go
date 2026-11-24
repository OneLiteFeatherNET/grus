package events

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

type fixedClock struct{ now time.Time }

func (c fixedClock) Now() time.Time { return c.now }

func newTestHub(capacity int) *Hub {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewHub("e1", capacity, fixedClock{now: time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)}, logger)
}

func publishN(h *Hub, n int) {
	for i := 0; i < n; i++ {
		h.Publish(model.EventInstanceState, map[string]int{"n": i})
	}
}

func ids(events []model.Event) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, ev.ID)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHeadIsEpochZeroBeforeFirstEvent(t *testing.T) {
	h := newTestHub(10)
	if got := h.Head(); got != "e1-0" {
		t.Fatalf("Head() = %q, want %q before any event", got, "e1-0")
	}
}

func TestPublishAssignsEpochPrefixedSequentialIDs(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 3)
	_, backlog, _ := h.Subscribe("e1-0", nil)
	want := []string{"e1-1", "e1-2", "e1-3"}
	if got := ids(backlog); !equalStrings(got, want) {
		t.Fatalf("published IDs = %v, want %v", got, want)
	}
	if got := h.Head(); got != "e1-3" {
		t.Fatalf("Head() = %q, want %q", got, "e1-3")
	}
}

func TestPublishStoresJSONPayloadAndTimestamp(t *testing.T) {
	h := newTestHub(10)
	h.Publish(model.EventPoolUpdated, map[string]string{"pool": "lobby"})
	_, backlog, _ := h.Subscribe("e1-0", nil)
	if len(backlog) != 1 {
		t.Fatalf("backlog has %d events, want 1", len(backlog))
	}
	ev := backlog[0]
	if ev.Type != model.EventPoolUpdated {
		t.Errorf("Type = %q, want %q", ev.Type, model.EventPoolUpdated)
	}
	if string(ev.Data) != `{"pool":"lobby"}` {
		t.Errorf("Data = %s, want compact JSON {\"pool\":\"lobby\"}", ev.Data)
	}
	if !ev.At.Equal(time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("At = %v, want the injected clock time", ev.At)
	}
}

func TestSubscribeWithoutLastEventIDReturnsNoBacklog(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 3)
	_, backlog, resync := h.Subscribe("", nil)
	if resync {
		t.Errorf("resync = true for empty Last-Event-ID, want false")
	}
	if len(backlog) != 0 {
		t.Errorf("backlog = %v, want none for empty Last-Event-ID", ids(backlog))
	}
}

func TestSubscribeReplaysEventsAfterKnownID(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 5)
	_, backlog, resync := h.Subscribe("e1-2", nil)
	if resync {
		t.Fatalf("resync = true for a known ID in range, want false")
	}
	want := []string{"e1-3", "e1-4", "e1-5"}
	if got := ids(backlog); !equalStrings(got, want) {
		t.Fatalf("backlog = %v, want %v", got, want)
	}
}

func TestSubscribeWithHeadIDReturnsNoBacklogWithoutResync(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)
	_, backlog, resync := h.Subscribe("e1-2", nil)
	if resync {
		t.Errorf("resync = true for the head ID, want false")
	}
	if len(backlog) != 0 {
		t.Errorf("backlog = %v, want none when client is at head", ids(backlog))
	}
}

func TestSubscribeResyncsOnForeignEpoch(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)
	_, backlog, resync := h.Subscribe("e0-1", nil)
	if !resync {
		t.Fatalf("resync = false for a foreign epoch, want true")
	}
	if len(backlog) != 0 {
		t.Errorf("backlog = %v, want none on resync", ids(backlog))
	}
}

func TestSubscribeResyncsOnUnparsableID(t *testing.T) {
	cases := map[string]string{
		"garbage":           "garbage",
		"epoch without seq": "e1",
		"non-numeric seq":   "e1-x",
		"negative seq":      "e1--1",
	}
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			h := newTestHub(10)
			publishN(h, 2)
			_, backlog, resync := h.Subscribe(id, nil)
			if !resync {
				t.Errorf("resync = false for Last-Event-ID %q, want true", id)
			}
			if len(backlog) != 0 {
				t.Errorf("backlog = %v, want none on resync", ids(backlog))
			}
		})
	}
}

func TestSubscribeResyncsWhenIDIsAheadOfHead(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)
	_, backlog, resync := h.Subscribe("e1-3", nil)
	if !resync {
		t.Fatalf("resync = false for an ID ahead of head, want true")
	}
	if len(backlog) != 0 {
		t.Errorf("backlog = %v, want none on resync", ids(backlog))
	}
}

func TestSubscribeResyncsWhenIDIsOlderThanBuffer(t *testing.T) {
	h := newTestHub(3)
	publishN(h, 10) // buffer now holds e1-8, e1-9, e1-10
	_, backlog, resync := h.Subscribe("e1-6", nil)
	if !resync {
		t.Fatalf("resync = false for an ID older than the buffer, want true")
	}
	if len(backlog) != 0 {
		t.Errorf("backlog = %v, want none on resync", ids(backlog))
	}
}

func TestSubscribeReplaysFromOneBeforeOldestBufferedEvent(t *testing.T) {
	h := newTestHub(3)
	publishN(h, 10) // buffer now holds e1-8, e1-9, e1-10
	_, backlog, resync := h.Subscribe("e1-7", nil)
	if resync {
		t.Fatalf("resync = true for the boundary ID oldest-1, want false")
	}
	want := []string{"e1-8", "e1-9", "e1-10"}
	if got := ids(backlog); !equalStrings(got, want) {
		t.Fatalf("backlog = %v, want %v", got, want)
	}
}

func TestSubscribeDeliversEachEventOnceAcrossBacklogAndLive(t *testing.T) {
	h := newTestHub(10)
	publishN(h, 2)
	sub, backlog, _ := h.Subscribe("e1-1", nil)
	if got := ids(backlog); !equalStrings(got, []string{"e1-2"}) {
		t.Fatalf("backlog = %v, want [e1-2]", got)
	}

	h.Publish(model.EventInstanceState, map[string]int{"n": 3})

	select {
	case ev := <-sub.Events():
		if ev.ID != "e1-3" {
			t.Fatalf("live event ID = %q, want %q", ev.ID, "e1-3")
		}
	default:
		t.Fatalf("live event e1-3 not delivered to subscriber")
	}
	select {
	case ev := <-sub.Events():
		t.Fatalf("unexpected extra event %q: backlog event was duplicated", ev.ID)
	default:
	}
}

func TestSubscribeBacklogRespectsFilter(t *testing.T) {
	h := newTestHub(10)
	h.Publish(model.EventInstanceCreated, map[string]string{})
	h.Publish(model.EventPoolScaled, map[string]string{})
	h.Publish(model.EventInstanceState, map[string]string{})
	onlyCreated := func(ev model.Event) bool { return ev.Type == model.EventInstanceCreated }

	_, backlog, _ := h.Subscribe("e1-0", onlyCreated)
	if got := ids(backlog); !equalStrings(got, []string{"e1-1"}) {
		t.Fatalf("filtered backlog = %v, want [e1-1]", got)
	}
}

func TestSlowSubscriberIsDroppedWithoutBlockingPublish(t *testing.T) {
	h := newTestHub(10)
	slow, _, _ := h.Subscribe("", nil)
	fast, _, _ := h.Subscribe("", nil)

	const published = subscriberBuffer + 44
	for i := 0; i < published; i++ {
		// Publish must return even though the slow subscriber never reads.
		h.Publish(model.EventInstanceState, map[string]int{"n": i})
		select {
		case <-fast.Events():
		default:
			t.Fatalf("fast subscriber did not receive event %d", i+1)
		}
	}

	received := 0
	for range slow.Events() {
		received++
	}
	if received != subscriberBuffer {
		t.Fatalf("slow subscriber received %d events before its channel closed, want %d", received, subscriberBuffer)
	}
}

func TestClosedSubscriptionReceivesNothingMoreAndCloseIsIdempotent(t *testing.T) {
	h := newTestHub(10)
	sub, _, _ := h.Subscribe("", nil)
	sub.Close()
	sub.Close()

	h.Publish(model.EventInstanceState, map[string]int{"n": 1})

	if _, open := <-sub.Events(); open {
		t.Fatalf("closed subscription delivered an event, want its channel closed")
	}
}

func TestTypeFilterMatchesExactTypesAndPrefixWildcard(t *testing.T) {
	filter := newFilter("instance.*, pool.updated", "")
	if filter == nil {
		t.Fatalf("filter = nil, want a type filter")
	}
	cases := map[string]bool{
		"instance.created":  true,
		"instance.state":    true,
		"pool.updated":      true,
		"pool.scaled":       false,
		"template.tagged":   false,
		"instanceX.created": false,
	}
	for eventType, want := range cases {
		if got := filter(model.Event{Type: eventType}); got != want {
			t.Errorf("type %q: filter = %v, want %v", eventType, got, want)
		}
	}
}

func TestPoolFilterMatchesPoolField(t *testing.T) {
	filter := newFilter("", "lobby")
	if !filter(model.Event{Type: model.EventInstanceState, Data: []byte(`{"pool":"lobby"}`)}) {
		t.Errorf("event of pool lobby rejected, want accepted")
	}
	if filter(model.Event{Type: model.EventInstanceState, Data: []byte(`{"pool":"other"}`)}) {
		t.Errorf("event of pool other accepted, want rejected")
	}
}

func TestPoolFilterRejectsEventsWithoutPoolField(t *testing.T) {
	filter := newFilter("", "lobby")
	if filter(model.Event{Type: model.EventTemplateTagged, Data: []byte(`{"name":"x"}`)}) {
		t.Errorf("event without a pool field accepted by pool filter, want rejected")
	}
}

func TestNoFilterParametersPassEverything(t *testing.T) {
	if filter := newFilter("", ""); filter != nil {
		t.Errorf("newFilter with no parameters returned a filter, want nil (pass everything)")
	}
}
