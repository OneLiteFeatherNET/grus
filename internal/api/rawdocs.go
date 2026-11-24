package api

import (
	"io"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// healthz answers liveness: the process serves HTTP.
func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

// readyz answers readiness from Config.Ready. A nil Ready counts as ready.
func (s *server) readyz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if s.cfg.Ready != nil && !s.cfg.Ready() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, "not ready\n")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok\n")
}

// documentRaw adds the plain net/http routes to the OpenAPI document, because huma does not
// know them. It is called for the served document and for the checked-in file.
func documentRaw(doc *huma.OpenAPI) {
	if doc.Paths == nil {
		doc.Paths = map[string]*huma.PathItem{}
	}
	text := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{
			"text/plain": {Schema: &huma.Schema{Type: huma.TypeString}},
		}}
	}
	sse := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{
			"text/event-stream": {Schema: &huma.Schema{Type: huma.TypeString}},
		}}
	}
	problem := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{
			"application/problem+json": {Schema: &huma.Schema{Type: huma.TypeObject}},
		}}
	}
	secured := func(op huma.Operation) *huma.Operation {
		op.Security = []map[string][]string{{bearerScheme: {}}}
		if op.Responses == nil {
			op.Responses = map[string]*huma.Response{}
		}
		op.Responses["401"] = problem("missing or invalid bearer token")
		op.Responses["403"] = problem("token lacks the required scope")
		return &op
	}

	doc.Paths["/healthz"] = &huma.PathItem{Get: &huma.Operation{
		OperationID: "healthz", Summary: "Liveness", Responses: map[string]*huma.Response{
			"200": text("process is alive"),
		},
	}}
	doc.Paths["/readyz"] = &huma.PathItem{Get: &huma.Operation{
		OperationID: "readyz", Summary: "Readiness", Responses: map[string]*huma.Response{
			"200": text("ready"),
			"503": text("not ready"),
		},
	}}

	logs := secured(huma.Operation{
		OperationID: "stream-instance-logs", Summary: "Follow an instance log as server-sent events",
		Description: "Each log line is sent as one `data:` event.",
		Responses:   map[string]*huma.Response{"200": sse("log stream")},
	})
	logs.Responses["404"] = problem("instance not found")
	doc.Paths["/v1/instances/{id}/logs/stream"] = &huma.PathItem{Get: logs}

	upload := secured(huma.Operation{
		OperationID: "upload-template-version", Summary: "Store a template version",
		Description: "Request body is a tar.gz archive of at most 256 MiB.",
		RequestBody: &huma.RequestBody{Required: true, Content: map[string]*huma.MediaType{
			"application/gzip": {Schema: &huma.Schema{Type: huma.TypeString, Format: "binary"}},
		}},
		Responses: map[string]*huma.Response{
			"201": {Description: "version stored", Content: map[string]*huma.MediaType{
				"application/json": {Schema: &huma.Schema{Type: huma.TypeObject}},
			}},
			"413": problem("archive larger than 256 MiB"),
		},
	})
	doc.Paths["/v1/templates/{name}/versions"] = &huma.PathItem{Post: upload}

	events := secured(huma.Operation{
		OperationID: "stream-events", Summary: "Event stream (server-sent events)",
		Description: "Each event has an `event:` type, an `id:` sequence and JSON `data:`. Resume with `Last-Event-ID`.",
		Responses:   map[string]*huma.Response{"200": sse("event stream")},
	})
	doc.Paths["/v1/events"] = &huma.PathItem{Get: events}
}
