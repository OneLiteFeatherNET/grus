package api

import (
	"bufio"
	"context"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

const (
	defaultLogTail = 200
	maxLogTail     = 5000
)

type listInstancesInput struct {
	Pool  string `query:"pool" doc:"Only instances of this pool"`
	State string `query:"state" doc:"Only instances in this lifecycle state"`
	Role  string `query:"role" enum:"proxy,game,other" doc:"Only instances with this role"`
}

type instancesOutput struct {
	Body []model.Instance
}

type instanceOutput struct {
	Body model.Instance
}

type instancePath struct {
	ID string `path:"id" doc:"Instance id"`
}

type createInstanceInput struct {
	Body createInstanceBody
}

type createInstanceBody struct {
	Pool string `json:"pool" minLength:"1" doc:"Pool that gets the new instance"`
}

type stopInstanceInput struct {
	ID string `path:"id" doc:"Instance id"`
	// Pointer so the body is optional: a stop without options is a plain stop.
	Body *controller.StopOptions
}

type commandInput struct {
	ID   string `path:"id" doc:"Instance id"`
	Body commandBody
}

type commandBody struct {
	Line string `json:"line" minLength:"1" doc:"Console line sent to the instance"`
}

type logsInput struct {
	ID   string `path:"id" doc:"Instance id"`
	Tail int    `query:"tail" default:"200" minimum:"1" maximum:"5000" doc:"Number of trailing lines"`
}

type debugStatusInput struct {
	ID   string `path:"id" doc:"Instance id"`
	Body controller.StatusUpdate
}

func (s *server) registerInstances(api huma.API) {
	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "list-instances", Method: http.MethodGet, Path: "/v1/instances",
		Summary: "List instances, optionally filtered by pool, state and role",
	}), func(ctx context.Context, in *listInstancesInput) (*instancesOutput, error) {
		list, err := s.svc.ListInstances(ctx, store.InstanceFilter{
			Pool:  in.Pool,
			State: model.State(in.State),
			Role:  model.Role(in.Role),
		})
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if list == nil {
			list = []model.Instance{}
		}
		return &instancesOutput{Body: list}, nil
	})

	huma.Register(api, operation(scopeInstancesWrite, huma.Operation{
		OperationID: "create-instance", Method: http.MethodPost, Path: "/v1/instances",
		Summary: "Add an instance to a pool", DefaultStatus: http.StatusCreated,
	}), func(ctx context.Context, in *createInstanceInput) (*instanceOutput, error) {
		inst, err := s.svc.CreateInstance(ctx, in.Body.Pool)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "get-instance", Method: http.MethodGet, Path: "/v1/instances/{id}", Summary: "Get an instance",
	}), func(ctx context.Context, in *instancePath) (*instanceOutput, error) {
		inst, err := s.svc.GetInstance(ctx, in.ID)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeInstancesWrite, huma.Operation{
		OperationID: "stop-instance", Method: http.MethodPost, Path: "/v1/instances/{id}/stop",
		Summary: "Stop an instance; with players it needs drain or force",
	}), func(ctx context.Context, in *stopInstanceInput) (*instanceOutput, error) {
		var opts controller.StopOptions
		if in.Body != nil {
			opts = *in.Body
		}
		inst, err := s.svc.StopInstance(ctx, in.ID, opts)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeInstancesCons, huma.Operation{
		OperationID: "send-instance-command", Method: http.MethodPost, Path: "/v1/instances/{id}/command",
		Summary: "Send a console command", DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *commandInput) (*struct{}, error) {
		if err := s.svc.Command(ctx, in.ID, in.Body.Line); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "get-instance-logs", Method: http.MethodGet, Path: "/v1/instances/{id}/logs",
		Summary: "Read the last lines of an instance log as text/plain",
	}), func(ctx context.Context, in *logsInput) (*huma.StreamResponse, error) {
		logs, err := s.svc.Logs(ctx, in.ID, in.Tail, false)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return textBody(logs), nil
	})
}

// registerDebug adds the admin-only debug routes. They are registered only with DebugEndpoints.
func (s *server) registerDebug(api huma.API) {
	huma.Register(api, operation(scopeAdmin, huma.Operation{
		OperationID: "debug-set-instance-status", Method: http.MethodPost, Path: "/v1/instances/{id}/debug/status",
		Summary: "Debug: set the status of an instance as if it reported itself",
	}), func(ctx context.Context, in *debugStatusInput) (*instanceOutput, error) {
		inst, err := s.svc.SelfStatus(ctx, in.ID, in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})
}

// registerRaw adds the routes that are not huma operations: streaming endpoints and the template upload.
func (s *server) registerRaw(mux *http.ServeMux) {
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /readyz", s.readyz)
	mux.Handle("GET /v1/instances/{id}/logs/stream", s.auth.raw(scopeInstancesRead, http.HandlerFunc(s.logsStream)))
	mux.Handle("POST /v1/templates/{name}/versions", s.auth.raw(scopeTemplatesWrite, http.HandlerFunc(s.uploadVersion)))
	mux.Handle("GET /v1/events", s.auth.raw(scopeEventsRead, s.events))
}

// logsStream writes every log line as one SSE "data:" event until the client goes away.
func (s *server) logsStream(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	logs, err := s.svc.Logs(ctx, r.PathValue("id"), defaultLogTail, true)
	if err != nil {
		writeProblem(w, mapError(s.log, err))
		return
	}
	defer logs.Close()
	// Closing the reader unblocks a follow read when the client disconnects.
	go func() {
		<-ctx.Done()
		_ = logs.Close()
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	flusher := http.NewResponseController(w)
	_ = flusher.Flush()

	scanner := bufio.NewScanner(logs)
	for scanner.Scan() {
		if _, err := fmt.Fprintf(w, "data: %s\n\n", scanner.Text()); err != nil {
			return
		}
		_ = flusher.Flush()
	}
}
