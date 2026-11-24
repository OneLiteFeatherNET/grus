// Package events implements the SSE event hub. It numbers every event, keeps the most recent ones in a
// ring buffer so that a reconnecting client can resume with Last-Event-ID, and fans events out to
// subscribers without ever blocking the publisher. It depends only on the standard library and model.
package events

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// subscriberBuffer is the channel capacity per subscriber. A subscriber that falls this far behind is dropped.
const subscriberBuffer = 256

// ticker abstracts time.Ticker so that tests can drive heartbeats without waiting.
type ticker interface {
	C() <-chan time.Time
	Stop()
}

type stdTicker struct{ t *time.Ticker }

func (s stdTicker) C() <-chan time.Time { return s.t.C }
func (s stdTicker) Stop()               { s.t.Stop() }

// Hub numbers, buffers and fans out events. It implements model.Publisher. The zero value is not usable;
// create hubs with NewHub.
type Hub struct {
	epoch     string
	clock     model.Clock
	logger    *slog.Logger
	newTicker func(time.Duration) ticker

	mu    sync.Mutex
	seq   uint64        // sequence number of the last published event; 0 before the first one
	ring  []model.Event // fixed-size ring buffer, oldest event at start
	start int
	count int
	subs  map[*Subscription]struct{}
}

// NewHub creates a hub. epoch identifies this controller run (e.g. start time in unix millis as string);
// capacity is the ring buffer size (e.g. 10000).
func NewHub(epoch string, capacity int, clock model.Clock, logger *slog.Logger) *Hub {
	if capacity < 1 {
		capacity = 1
	}
	if clock == nil {
		clock = model.SystemClock{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		epoch:     epoch,
		clock:     clock,
		logger:    logger,
		newTicker: func(d time.Duration) ticker { return stdTicker{time.NewTicker(d)} },
		ring:      make([]model.Event, capacity),
		subs:      make(map[*Subscription]struct{}),
	}
}

// Publish marshals data to JSON, assigns the ID "<epoch>-<seq>", stores the event in the ring buffer and
// fans it out to subscribers. It never blocks: a subscriber whose channel is full is dropped and logged.
func (h *Hub) Publish(eventType string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.logger.Error("event dropped: payload is not JSON-encodable", "type", eventType, "error", err)
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.seq++
	ev := model.Event{ID: h.idFor(h.seq), Type: eventType, Data: raw, At: h.clock.Now()}
	h.store(ev)

	for sub := range h.subs {
		if sub.filter != nil && !sub.filter(ev) {
			continue
		}
		select {
		case sub.ch <- ev:
		default:
			close(sub.ch)
			delete(h.subs, sub)
			h.logger.Warn("event subscriber dropped: buffer full", "type", eventType, "buffer", subscriberBuffer)
		}
	}
}

// Subscribe registers a subscriber. lastEventID is the client's Last-Event-ID ("" = none). It returns the
// backlog to send first and whether the client must resync. Backlog and registration happen under one lock,
// so no event is lost or duplicated between the two.
func (h *Hub) Subscribe(lastEventID string, filter func(model.Event) bool) (*Subscription, []model.Event, bool) {
	sub, backlog, resync, _ := h.subscribe(lastEventID, filter)
	return sub, backlog, resync
}

// subscribe is Subscribe plus the head ID at registration time, which the handler needs for the resync event.
func (h *Hub) subscribe(lastEventID string, filter func(model.Event) bool) (*Subscription, []model.Event, bool, string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	sub := &Subscription{ch: make(chan model.Event, subscriberBuffer), filter: filter, hub: h}
	h.subs[sub] = struct{}{}
	head := h.idFor(h.seq)

	if lastEventID == "" {
		return sub, nil, false, head
	}
	seq, ok := h.parseSeq(lastEventID)
	if !ok || seq > h.seq || seq+1 < h.firstSeq() {
		return sub, nil, true, head
	}

	var backlog []model.Event
	from := 0
	if seq >= h.firstSeq() {
		from = int(seq + 1 - h.firstSeq())
	}
	for i := from; i < h.count; i++ {
		ev := h.at(i)
		if filter == nil || filter(ev) {
			backlog = append(backlog, ev)
		}
	}
	return sub, backlog, false, head
}

// Head returns the ID of the most recent event, or "<epoch>-0" when no event was published yet.
func (h *Hub) Head() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.idFor(h.seq)
}

// Subscription is one registered subscriber. Its channel is closed when the subscription is closed or
// dropped for falling behind.
type Subscription struct {
	ch     chan model.Event
	filter func(model.Event) bool
	hub    *Hub
}

// Events returns the channel on which matching events arrive.
func (s *Subscription) Events() <-chan model.Event { return s.ch }

// Close unregisters the subscriber and closes its channel. Calling Close again, or after the hub dropped
// the subscriber, does nothing.
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	defer s.hub.mu.Unlock()
	if _, ok := s.hub.subs[s]; ok {
		delete(s.hub.subs, s)
		close(s.ch)
	}
}

// idFor must be called with h.mu held.
func (h *Hub) idFor(seq uint64) string {
	return fmt.Sprintf("%s-%d", h.epoch, seq)
}

// parseSeq returns the sequence of an ID that belongs to this epoch. It must be called with h.mu held.
func (h *Hub) parseSeq(id string) (uint64, bool) {
	i := strings.LastIndexByte(id, '-')
	if i < 0 || id[:i] != h.epoch {
		return 0, false
	}
	seq, err := strconv.ParseUint(id[i+1:], 10, 64)
	return seq, err == nil
}

// firstSeq is the sequence of the oldest buffered event, or the next sequence when the buffer is empty.
// It must be called with h.mu held.
func (h *Hub) firstSeq() uint64 {
	if h.count == 0 {
		return h.seq + 1
	}
	return h.seq - uint64(h.count) + 1
}

// store appends ev to the ring buffer, overwriting the oldest event when full. Must be called with h.mu held.
func (h *Hub) store(ev model.Event) {
	if h.count == len(h.ring) {
		h.ring[h.start] = ev
		h.start = (h.start + 1) % len(h.ring)
		return
	}
	h.ring[(h.start+h.count)%len(h.ring)] = ev
	h.count++
}

// at returns the i-th buffered event, 0 being the oldest. Must be called with h.mu held.
func (h *Hub) at(i int) model.Event {
	return h.ring[(h.start+i)%len(h.ring)]
}
