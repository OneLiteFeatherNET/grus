package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

type poolPath struct {
	Pool string `path:"pool" doc:"Pool name"`
}

type poolOutput struct {
	Body model.Pool
}

type poolsOutput struct {
	Body []model.Pool
}

type putPoolInput struct {
	Pool string `path:"pool" doc:"Pool name"`
	Body model.PoolSpec
}

type deletePoolInput struct {
	Pool  string `path:"pool" doc:"Pool name"`
	Drain bool   `query:"drain" doc:"Drain the instances before the pool is removed"`
}

type maintenanceInput struct {
	Pool string `path:"pool" doc:"Pool name"`
	Body maintenanceBody
}

type maintenanceBody struct {
	Enabled bool `json:"enabled"`
}

func (s *server) registerPools(api huma.API) {
	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "list-pools", Method: http.MethodGet, Path: "/v1/pools", Summary: "List pools",
	}), func(ctx context.Context, _ *struct{}) (*poolsOutput, error) {
		pools, err := s.svc.ListPools(ctx)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if pools == nil {
			pools = []model.Pool{}
		}
		return &poolsOutput{Body: pools}, nil
	})

	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "put-pool", Method: http.MethodPut, Path: "/v1/pools/{pool}",
		Summary: "Create or replace a pool", Description: "Idempotent. Generation increases when the spec changes.",
	}), func(ctx context.Context, in *putPoolInput) (*poolOutput, error) {
		pool, err := s.svc.PutPool(ctx, in.Pool, in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &poolOutput{Body: pool}, nil
	})

	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "get-pool", Method: http.MethodGet, Path: "/v1/pools/{pool}", Summary: "Get a pool",
	}), func(ctx context.Context, in *poolPath) (*poolOutput, error) {
		pool, err := s.svc.GetPool(ctx, in.Pool)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &poolOutput{Body: pool}, nil
	})

	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "delete-pool", Method: http.MethodDelete, Path: "/v1/pools/{pool}",
		Summary: "Delete a pool", DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *deletePoolInput) (*struct{}, error) {
		if err := s.svc.DeletePool(ctx, in.Pool, in.Drain); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "set-pool-maintenance", Method: http.MethodPut, Path: "/v1/pools/{pool}/maintenance",
		Summary: "Enable or disable maintenance for a pool",
	}), func(ctx context.Context, in *maintenanceInput) (*poolOutput, error) {
		pool, err := s.svc.SetMaintenance(ctx, in.Pool, in.Body.Enabled)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &poolOutput{Body: pool}, nil
	})
}
