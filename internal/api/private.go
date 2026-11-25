package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

type privateServerInput struct {
	Body privateServerBody
}

type privateServerBody struct {
	Pool  string `json:"pool" minLength:"1" doc:"Private pool (a pool with a private section)"`
	Owner string `json:"owner" minLength:"1" maxLength:"63" pattern:"^[A-Za-z0-9][A-Za-z0-9._-]*$" doc:"Owner of the server, e.g. a player uuid"`
}

type privateServerOutput struct {
	Status int
	Body   model.Instance
}

type listPrivateServersInput struct {
	Owner string `query:"owner" doc:"Only the private servers of this owner"`
}

func (s *server) registerPrivateServers(api huma.API) {
	huma.Register(api, operation(scopeInstancesWrite, huma.Operation{
		OperationID: "create-private-server", Method: http.MethodPost, Path: "/v1/private-servers",
		Summary: "Start a private server for an owner, or return the one the owner already has",
		Description: "Answers 201 when an instance was started and 200 when the owner's live instance is returned. " +
			"409 when the owner is at the quota of the pool.",
		Errors: []int{http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}), func(ctx context.Context, in *privateServerInput) (*privateServerOutput, error) {
		inst, created, err := s.svc.CreatePrivateServer(ctx, in.Body.Pool, in.Body.Owner)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		return &privateServerOutput{Status: status, Body: inst}, nil
	})

	huma.Register(api, operation(scopeInstancesRead, huma.Operation{
		OperationID: "list-private-servers", Method: http.MethodGet, Path: "/v1/private-servers",
		Summary: "List private servers, optionally of one owner",
	}), func(ctx context.Context, in *listPrivateServersInput) (*instancesOutput, error) {
		list, err := s.svc.ListPrivateServers(ctx, in.Owner)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if list == nil {
			list = []model.Instance{}
		}
		return &instancesOutput{Body: list}, nil
	})

	huma.Register(api, operation(scopeInstancesWrite, huma.Operation{
		OperationID: "stop-private-server", Method: http.MethodDelete, Path: "/v1/private-servers/{id}",
		Summary: "Stop a private server once its players left (drain)",
	}), func(ctx context.Context, in *instancePath) (*instanceOutput, error) {
		inst, err := s.svc.StopPrivateServer(ctx, in.ID)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &instanceOutput{Body: inst}, nil
	})
}
