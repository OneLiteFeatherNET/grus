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

type cellsOutput struct {
	Body []model.CellInfo
}

type cellOutput struct {
	Body model.CellInfo
}

type cellAtInput struct {
	Pool string `path:"pool" doc:"Pool name"`
	X    int    `query:"x" required:"true" doc:"Block x coordinate"`
	Z    int    `query:"z" required:"true" doc:"Block z coordinate"`
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

	// Proxies resolve cells, so these routes need only instances:read, which instance tokens have.
	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "list-pool-cells", Method: http.MethodGet, Path: "/v1/pools/{pool}/cells",
		Summary: "List the cells of a sharded pool with the instance that serves each",
	}), func(ctx context.Context, in *poolPath) (*cellsOutput, error) {
		cells, err := s.svc.PoolCells(ctx, in.Pool)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &cellsOutput{Body: cells}, nil
	})

	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "get-pool-cell-at", Method: http.MethodGet, Path: "/v1/pools/{pool}/cells/at",
		Summary:     "Find the cell and instance that own a block position",
		Description: "404 when no cell covers the block, 409 when the cell has no Ready instance yet.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}), func(ctx context.Context, in *cellAtInput) (*cellOutput, error) {
		cell, err := s.svc.CellAt(ctx, in.Pool, in.X, in.Z)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &cellOutput{Body: cell}, nil
	})
}
