package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// maxTemplateUpload bounds one tar.gz template version upload.
const maxTemplateUpload = 256 << 20

type templatesOutput struct {
	Body map[string]map[string]string
}

type tagTemplateInput struct {
	Name string `path:"name" doc:"Template name"`
	Tag  string `path:"tag" doc:"Tag to point at the digest"`
	Body tagBody
}

type tagBody struct {
	Digest string `json:"digest" pattern:"^sha256:[0-9a-f]+$" doc:"Digest of an existing version"`
}

type archiveInput struct {
	Name   string `path:"name" doc:"Template name"`
	Digest string `path:"digest" pattern:"^sha256:[0-9a-f]+$" doc:"Version digest, e.g. sha256:<hex>"`
}

func (s *server) registerTemplates(api huma.API) {
	huma.Register(api, operation(scopeTemplatesRead, huma.Operation{
		OperationID: "list-templates", Method: http.MethodGet, Path: "/v1/templates",
		Summary: "List templates with their tags (tag -> digest)",
	}), func(ctx context.Context, _ *struct{}) (*templatesOutput, error) {
		all, err := s.tpl.List(ctx)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		if all == nil {
			all = map[string]map[string]string{}
		}
		return &templatesOutput{Body: all}, nil
	})

	huma.Register(api, operation(scopeTemplatesWrite, huma.Operation{
		OperationID: "tag-template", Method: http.MethodPut, Path: "/v1/templates/{name}/tags/{tag}",
		Summary: "Point a tag at a version", DefaultStatus: http.StatusNoContent,
	}), func(ctx context.Context, in *tagTemplateInput) (*struct{}, error) {
		if err := s.tpl.Tag(ctx, in.Name, in.Tag, in.Body.Digest); err != nil {
			return nil, mapError(s.log, err)
		}
		return nil, nil
	})

	huma.Register(api, operation(scopeTemplatesArchive, huma.Operation{
		OperationID: "get-template-archive", Method: http.MethodGet,
		Path:    "/v1/templates/{name}/versions/{digest}/archive",
		Summary: "Download a template version as tar.gz",
	}), func(ctx context.Context, in *archiveInput) (*huma.StreamResponse, error) {
		// Resolve first so an unknown version is a 404 and not a half-written stream.
		digest, err := s.tpl.Resolve(ctx, in.Name, in.Digest)
		if err != nil {
			return nil, mapError(s.log, err)
		}
		return &huma.StreamResponse{Body: func(hc huma.Context) {
			hc.SetHeader("Content-Type", "application/gzip")
			if err := s.tpl.WriteTarGz(hc.Context(), in.Name, digest, hc.BodyWriter()); err != nil {
				s.log.Error("api: template archive stream failed", "err", err)
			}
		}}, nil
	})
}

// uploadVersion stores a tar.gz template version. It is a plain route because the body is streamed
// into the store instead of being buffered by huma.
func (s *server) uploadVersion(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxTemplateUpload)
	v, err := s.tpl.ImportTarGz(r.Context(), r.PathValue("name"), r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeProblem(w, huma.Error413RequestEntityTooLarge("template version exceeds 256 MiB"))
			return
		}
		writeProblem(w, mapError(s.log, err))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(v)
}
