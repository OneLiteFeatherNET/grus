package client

import (
	"bufio"
	"io"
	"strings"
)

// maxSSELine bounds one SSE line; event payloads are small JSON documents.
const maxSSELine = 1 << 20

// ReadSSE parses a text/event-stream and calls fn for every dispatched event. Comment lines are skipped.
func ReadSSE(r io.Reader, fn func(Event) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), maxSSELine)
	var ev Event
	var data []string
	flush := func() error {
		if len(data) == 0 && ev.Type == "" {
			ev = Event{}
			return nil
		}
		ev.Data = []byte(strings.Join(data, "\n"))
		out := ev
		ev, data = Event{}, nil
		return fn(out)
	}
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			ev.ID = value
		case "event":
			ev.Type = value
		case "data":
			data = append(data, value)
		}
	}
	return sc.Err()
}
