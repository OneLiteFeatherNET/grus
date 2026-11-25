package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

type nameInput struct {
	Name string `path:"name" doc:"Object name"`
}

type environmentOutput struct{ Body model.Environment }
type environmentsOutput struct{ Body []model.Environment }
type putEnvironmentInput struct {
	Name string `path:"name" doc:"Environment name"`
	Body model.EnvironmentSpec
}

type groupOutput struct{ Body model.Group }
type groupsOutput struct{ Body []model.Group }
type putGroupInput struct {
	Name string `path:"name" doc:"Group name"`
	Body model.GroupSpec
}

type networkOutput struct{ Body model.NetworkStatus }

const managedNote = " Objects owned by Kubernetes answer 409."

func (s *server) registerDeclarations(api huma.API) {
	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "list-environments", Method: http.MethodGet, Path: "/v1/environments", Summary: "List environments",
	}), func(ctx context.Context, _ *struct{}) (*environmentsOutput, error) {
		list, err := s.svc.ListEnvironments(ctx)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if list == nil {
			list = []model.Environment{}
		}
		return &environmentsOutput{Body: list}, nil
	})
	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "get-environment", Method: http.MethodGet, Path: "/v1/environments/{name}", Summary: "Get an environment",
	}), func(ctx context.Context, in *nameInput) (*environmentOutput, error) {
		e, err := s.svc.GetEnvironment(ctx, in.Name)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &environmentOutput{Body: e}, nil
	})
	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "put-environment", Method: http.MethodPut, Path: "/v1/environments/{name}",
		Summary:     "Create or replace an environment",
		Description: "Idempotent. Generation increases when the spec changes." + managedNote,
	}), func(ctx context.Context, in *putEnvironmentInput) (*environmentOutput, error) {
		e, err := s.svc.PutEnvironment(ctx, in.Name, in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &environmentOutput{Body: e}, nil
	})
	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "delete-environment", Method: http.MethodDelete, Path: "/v1/environments/{name}",
		Summary: "Delete an environment", Description: "Pools that still name it stop starting instances." + managedNote,
		DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *nameInput) (*struct{}, error) {
		if err := s.svc.DeleteEnvironment(ctx, in.Name); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "list-groups", Method: http.MethodGet, Path: "/v1/groups", Summary: "List groups",
	}), func(ctx context.Context, _ *struct{}) (*groupsOutput, error) {
		list, err := s.svc.ListGroups(ctx)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if list == nil {
			list = []model.Group{}
		}
		return &groupsOutput{Body: list}, nil
	})
	huma.Register(api, operation(scopePoolsRead, huma.Operation{
		OperationID: "get-group", Method: http.MethodGet, Path: "/v1/groups/{name}", Summary: "Get a group",
	}), func(ctx context.Context, in *nameInput) (*groupOutput, error) {
		g, err := s.svc.GetGroup(ctx, in.Name)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &groupOutput{Body: g}, nil
	})
	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "put-group", Method: http.MethodPut, Path: "/v1/groups/{name}",
		Summary:     "Create or replace a group",
		Description: "Idempotent. Generation increases when the spec changes." + managedNote,
	}), func(ctx context.Context, in *putGroupInput) (*groupOutput, error) {
		g, err := s.svc.PutGroup(ctx, in.Name, in.Body)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &groupOutput{Body: g}, nil
	})
	huma.Register(api, operation(scopePoolsWrite, huma.Operation{
		OperationID: "delete-group", Method: http.MethodDelete, Path: "/v1/groups/{name}",
		Summary: "Delete a group", Description: "Pools that still name it stop starting instances." + managedNote,
		DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *nameInput) (*struct{}, error) {
		if err := s.svc.DeleteGroup(ctx, in.Name); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopePlayersRead, huma.Operation{
		OperationID: "get-network", Method: http.MethodGet, Path: "/v1/network", Summary: "Get the network player count",
		Description: "Players and capacity summed over all Ready and Allocated proxy instances. " +
			"The SSE event network.players is published whenever the sum changes.",
	}), func(ctx context.Context, _ *struct{}) (*networkOutput, error) {
		n, err := s.svc.Network(ctx)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &networkOutput{Body: n}, nil
	})
}
