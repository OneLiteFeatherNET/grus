package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
)

// selfInstance returns the instance id of the caller. The self scope is only granted to instance
// tokens, so the id is always set when a self handler runs.
func selfInstance(ctx context.Context) string {
	return principalFrom(ctx).instanceID
}

type selfStatusInput struct {
	Body controller.StatusUpdate
}

func (s *server) registerSelf(api huma.API) {
	huma.Register(api, operation(scopeSelf, huma.Operation{
		OperationID: "get-self", Method: http.MethodGet, Path: "/v1/self", Summary: "Get the calling instance",
	}), func(ctx context.Context, _ *struct{}) (*instanceOutput, error) {
		inst, err := s.svc.GetInstance(ctx, selfInstance(ctx))
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeSelf, huma.Operation{
		OperationID: "self-ready", Method: http.MethodPost, Path: "/v1/self/ready",
		Summary: "Report that the calling instance is ready",
	}), func(ctx context.Context, _ *struct{}) (*instanceOutput, error) {
		inst, err := s.svc.SelfReady(ctx, selfInstance(ctx))
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeSelf, huma.Operation{
		OperationID: "self-status", Method: http.MethodPut, Path: "/v1/self/status",
		Summary: "Report players and labels; counts as heartbeat",
	}), func(ctx context.Context, in *selfStatusInput) (*instanceOutput, error) {
		inst, err := s.svc.SelfStatus(ctx, selfInstance(ctx), in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeSelf, huma.Operation{
		OperationID: "self-allocate", Method: http.MethodPost, Path: "/v1/self/allocate",
		Summary: "Mark the calling instance as allocated",
	}), func(ctx context.Context, _ *struct{}) (*instanceOutput, error) {
		inst, err := s.svc.SelfAllocate(ctx, selfInstance(ctx))
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})

	huma.Register(api, operation(scopeSelf, huma.Operation{
		OperationID: "self-shutdown", Method: http.MethodPost, Path: "/v1/self/shutdown",
		Summary: "Ask the controller to shut the calling instance down",
	}), func(ctx context.Context, _ *struct{}) (*instanceOutput, error) {
		inst, err := s.svc.SelfShutdown(ctx, selfInstance(ctx))
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})
}
