package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/store"
	"github.com/OneLiteFeatherNET/grus/internal/templates"
)

// mapError turns a service or store error into an RFC 9457 problem. Unknown errors are logged
// and answered with a generic 500 so no internal detail leaks to the client.
func mapError(log *slog.Logger, err error) huma.StatusError {
	var ve *controller.ValidationError
	switch {
	case errors.As(err, &ve):
		details := make([]error, 0, len(ve.Fields))
		for _, f := range ve.Fields {
			details = append(details, &huma.ErrorDetail{Message: f.Message, Location: "body." + f.Field})
		}
		return huma.Error422UnprocessableEntity(ve.Error(), details...)
	case errors.Is(err, controller.ErrNotFound), errors.Is(err, store.ErrNotFound), errors.Is(err, templates.ErrNotFound):
		return huma.Error404NotFound("not found")
	case errors.Is(err, controller.ErrHasPlayers), errors.Is(err, controller.ErrInvalidState), errors.Is(err, controller.ErrLimit), errors.Is(err, controller.ErrQuota), errors.Is(err, controller.ErrManagedElsewhere):
		return huma.Error409Conflict(err.Error())
	default:
		log.Error("api: request failed", "err", err)
		return huma.Error500InternalServerError("internal server error")
	}
}

// writeProblem writes a huma error as application/problem+json on a plain net/http route.
func writeProblem(w http.ResponseWriter, se huma.StatusError) {
	body, err := json.Marshal(se)
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(se.GetStatus())
	_, _ = w.Write(body)
}
