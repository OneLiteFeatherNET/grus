package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
)

// Agents is what the agent and host routes need from the remote hosts. *remote.Registry implements it.
type Agents interface {
	Register(ctx context.Context, host string, req remote.RegisterRequest) (remote.HostInfo, error)
	Desired(ctx context.Context, host string, revision int64, wait time.Duration) (remote.Desired, error)
	Observed(ctx context.Context, host string, rep remote.ObservedReport) error
	Hosts() []remote.HostInfo
	Host(name string) (remote.HostInfo, error)
}

type registerInput struct {
	Host string `path:"host" pattern:"^[a-z0-9][a-z0-9._-]{0,62}$" doc:"Agent host name"`
	Body remote.RegisterRequest
}

type hostOutput struct {
	Body remote.HostInfo
}

type hostsOutput struct {
	Body []remote.HostInfo
}

type desiredInput struct {
	Host     string `path:"host" pattern:"^[a-z0-9][a-z0-9._-]{0,62}$" doc:"Agent host name"`
	Wait     string `query:"wait" default:"25s" doc:"How long the request may wait for a change, as a duration (max 60s)"`
	Revision int64  `query:"revision" default:"-1" doc:"Revision the agent already has; the call returns at once when it differs"`
}

type desiredOutput struct {
	Body remote.Desired
}

type observedInput struct {
	Host string `path:"host" pattern:"^[a-z0-9][a-z0-9._-]{0,62}$" doc:"Agent host name"`
	Body remote.ObservedReport
}

type hostNameInput struct {
	Name string `path:"name" doc:"Host name"`
}

func (s *server) registerAgents(api huma.API) {
	huma.Register(api, operation(scopeAgent, huma.Operation{
		OperationID: "register-agent", Method: http.MethodPut, Path: "/v1/agents/{host}",
		Summary: "Register an agent host or send its heartbeat",
	}), func(ctx context.Context, in *registerInput) (*hostOutput, error) {
		if s.cfg.Agents == nil {
			return nil, huma.Error404NotFound("agents are not enabled")
		}
		info, err := s.cfg.Agents.Register(ctx, in.Host, in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &hostOutput{Body: info}, nil
	})

	huma.Register(api, operation(scopeAgent, huma.Operation{
		OperationID: "get-agent-desired", Method: http.MethodGet, Path: "/v1/agents/{host}/desired",
		Summary: "Long-poll the desired state of an agent host",
	}), func(ctx context.Context, in *desiredInput) (*desiredOutput, error) {
		if s.cfg.Agents == nil {
			return nil, huma.Error404NotFound("agents are not enabled")
		}
		wait, err := time.ParseDuration(in.Wait)
		if err != nil || wait < 0 {
			return nil, huma.Error422UnprocessableEntity("wait must be a duration like 25s")
		}
		desired, err := s.cfg.Agents.Desired(ctx, in.Host, in.Revision, wait)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &desiredOutput{Body: desired}, nil
	})

	huma.Register(api, operation(scopeAgent, huma.Operation{
		OperationID: "put-agent-observed", Method: http.MethodPut, Path: "/v1/agents/{host}/observed",
		Summary: "Report the instances an agent host runs", DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *observedInput) (*struct{}, error) {
		if s.cfg.Agents == nil {
			return nil, huma.Error404NotFound("agents are not enabled")
		}
		if err := s.cfg.Agents.Observed(ctx, in.Host, in.Body); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopeHostsRead, huma.Operation{
		OperationID: "list-hosts", Method: http.MethodGet, Path: "/v1/hosts",
		Summary: "List agent hosts",
	}), func(_ context.Context, _ *struct{}) (*hostsOutput, error) {
		if s.cfg.Agents == nil {
			return &hostsOutput{Body: []remote.HostInfo{}}, nil
		}
		hosts := s.cfg.Agents.Hosts()
		if hosts == nil {
			hosts = []remote.HostInfo{}
		}
		return &hostsOutput{Body: hosts}, nil
	})

	huma.Register(api, operation(scopeHostsRead, huma.Operation{
		OperationID: "get-host", Method: http.MethodGet, Path: "/v1/hosts/{name}",
		Summary: "Get an agent host",
	}), func(_ context.Context, in *hostNameInput) (*hostOutput, error) {
		if s.cfg.Agents == nil {
			return nil, huma.Error404NotFound("not found")
		}
		info, err := s.cfg.Agents.Host(in.Name)
		if errors.Is(err, remote.ErrUnknownHost) {
			return nil, huma.Error404NotFound("not found")
		}
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &hostOutput{Body: info}, nil
	})
}
