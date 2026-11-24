package events

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// defaultHeartbeat is used when Handler is given a non-positive interval.
const defaultHeartbeat = 15 * time.Second

// resyncData tells the client that its Last-Event-ID cannot be replayed and it must reload its state.
var resyncData = json.RawMessage(`{"reason":"last-event-id cannot be replayed"}`)

// Handler serves GET SSE (text/event-stream). Query: types=comma list, entries match exactly or as prefix
// when ending in ".*" (e.g. "instance.*"); pool=<name> matches events whose JSON data has "pool" == name.
// Last-Event-ID comes from the header, or from the query lastEventId as fallback. The stream stops when the
// request context ends.
func (h *Hub) Handler(heartbeat time.Duration) http.Handler {
	if heartbeat <= 0 {
		heartbeat = defaultHeartbeat
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		q := r.URL.Query()
		lastID := r.Header.Get("Last-Event-ID")
		if lastID == "" {
			lastID = q.Get("lastEventId")
		}
		sub, backlog, resync, head := h.subscribe(lastID, newFilter(q.Get("types"), q.Get("pool")))
		defer sub.Close()

		header := w.Header()
		header.Set("Content-Type", "text/event-stream")
		header.Set("Cache-Control", "no-cache")
		header.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		if resync {
			if writeEvent(w, model.Event{ID: head, Type: model.EventResync, Data: resyncData}) != nil {
				return
			}
		}
		for _, ev := range backlog {
			if writeEvent(w, ev) != nil {
				return
			}
		}
		flusher.Flush()

		t := h.newTicker(heartbeat)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case ev, open := <-sub.Events():
				if !open {
					return
				}
				if writeEvent(w, ev) != nil {
					return
				}
				flusher.Flush()
			case <-t.C():
				if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// writeEvent writes one event in SSE wire format. Event data is compact JSON, so it has no newlines.
func writeEvent(w io.Writer, ev model.Event) error {
	_, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Type, ev.Data)
	return err
}

// newFilter builds the subscriber filter from the types and pool query parameters. It returns nil when
// neither is set, which lets every event through.
func newFilter(types, pool string) func(model.Event) bool {
	var names, prefixes []string
	for _, t := range strings.Split(types, ",") {
		t = strings.TrimSpace(t)
		switch {
		case t == "":
		case strings.HasSuffix(t, ".*"):
			prefixes = append(prefixes, strings.TrimSuffix(t, "*"))
		default:
			names = append(names, t)
		}
	}
	if len(names) == 0 && len(prefixes) == 0 && pool == "" {
		return nil
	}
	return func(ev model.Event) bool {
		if (len(names) > 0 || len(prefixes) > 0) && !matchesType(ev.Type, names, prefixes) {
			return false
		}
		return pool == "" || eventPool(ev) == pool
	}
}

func matchesType(eventType string, names, prefixes []string) bool {
	for _, n := range names {
		if eventType == n {
			return true
		}
	}
	for _, p := range prefixes {
		if strings.HasPrefix(eventType, p) {
			return true
		}
	}
	return false
}

// eventPool returns the "pool" field of the event data, or "" when the event has none.
func eventPool(ev model.Event) string {
	var p struct {
		Pool string `json:"pool"`
	}
	if err := json.Unmarshal(ev.Data, &p); err != nil {
		return ""
	}
	return p.Pool
}
