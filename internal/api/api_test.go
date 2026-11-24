package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

const validPoolBody = `{"role":"game","mode":"dynamic","driver":"process","command":["java"],
"resources":{"memoryMiB":768},"scaling":{"min":1,"max":4,"scaleDownDelay":"2m"}}`

func TestHealthzReturnsOKWithoutToken(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/healthz", "", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
}

func TestReadyzReturns200WhenReady(t *testing.T) {
	fx := newFixture(t, Config{Ready: func() bool { return true }}, nil)
	rec := fx.call(t, http.MethodGet, "/readyz", "", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("readyz status = %d, want 200", rec.Code)
	}
}

func TestReadyzReturns503WhenNotReady(t *testing.T) {
	fx := newFixture(t, Config{Ready: func() bool { return false }}, nil)
	rec := fx.call(t, http.MethodGet, "/readyz", "", "", "")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("readyz status = %d, want 503", rec.Code)
	}
}

func TestRequestWithoutTokenReturns401Problem(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/v1/pools", "", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Fatalf("Content-Type = %q, want application/problem+json", ct)
	}
}

func TestRequestWithUnknownTokenReturns401(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/v1/pools", "not-a-token", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestInstanceTokenWithoutPoolsWriteScopeReturns403(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/pools/lobby", gameToken, "application/json", validPoolBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAdminTokenPutsPoolAndReturnsPool(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/pools/lobby", adminToken, "application/json", validPoolBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var pool model.Pool
	if err := json.Unmarshal(rec.Body.Bytes(), &pool); err != nil {
		t.Fatalf("decode pool: %v", err)
	}
	if pool.Name != "lobby" {
		t.Fatalf("pool name = %q, want lobby", pool.Name)
	}
	if fx.svc.putName != "lobby" {
		t.Fatalf("service got pool name %q, want lobby", fx.svc.putName)
	}
	if got := fx.svc.putSpec.Scaling.ScaleDownDelay.D().String(); got != "2m0s" {
		t.Fatalf("scaleDownDelay decoded as %s, want 2m0s", got)
	}
}

func TestInvalidPoolSpecReturns422WithFieldLocation(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.putErr = &controller.ValidationError{Fields: []model.FieldError{
		{Field: "role", Message: "must be proxy, game or other"},
	}}
	rec := fx.call(t, http.MethodPut, "/v1/pools/lobby", adminToken, "application/json", validPoolBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", rec.Code, rec.Body)
	}
	var problem struct {
		Errors []struct {
			Location string `json:"location"`
			Message  string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &problem); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if len(problem.Errors) != 1 || problem.Errors[0].Location != "body.role" {
		t.Fatalf("errors = %+v, want one error at body.role", problem.Errors)
	}
}

func TestListInstancesReturnsTopLevelArrayAndPassesFilters(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.listed = []model.Instance{{ID: "lobby-1", Pool: "lobby", Role: model.RoleGame, State: model.StateReady}}

	rec := fx.call(t, http.MethodGet, "/v1/instances?pool=lobby&state=Ready&role=game", adminToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	body := strings.TrimSpace(rec.Body.String())
	if !strings.HasPrefix(body, "[") {
		t.Fatalf("body must be a top-level JSON array, got %s", body)
	}
	want := store.InstanceFilter{Pool: "lobby", State: model.StateReady, Role: model.RoleGame}
	if fx.svc.listFilter != want {
		t.Fatalf("filter = %+v, want %+v", fx.svc.listFilter, want)
	}
}

func TestListInstancesWithoutFiltersReturnsEmptyArray(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/v1/instances", adminToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("body = %s, want []", got)
	}
}

func TestStopInstanceWithPlayersReturns409(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.stopErr = controller.ErrHasPlayers
	rec := fx.call(t, http.MethodPost, "/v1/instances/lobby-1/stop", adminToken, "application/json", `{}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body %s", rec.Code, rec.Body)
	}
}

func TestSelfStatusUsesPrincipalInstanceID(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/self/status", gameToken, "application/json",
		`{"players":3,"maxPlayers":20}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if fx.svc.statusID != "lobby-1" {
		t.Fatalf("SelfStatus called with %q, want the token's instance lobby-1", fx.svc.statusID)
	}
}

func TestStopInstanceWithoutBodyIsAPlainStop(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPost, "/v1/instances/lobby-1/stop", adminToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 without a body; body %s", rec.Code, rec.Body)
	}
	if fx.svc.stopID != "lobby-1" {
		t.Fatalf("StopInstance called with %q, want lobby-1", fx.svc.stopID)
	}
}

func TestAdminTokenCannotUseSelfEndpoints(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/v1/self", adminToken, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestDebugEndpointIsAbsentWithoutFlag(t *testing.T) {
	fx := newFixture(t, Config{DebugEndpoints: false}, nil)
	rec := fx.call(t, http.MethodPost, "/v1/instances/lobby-1/debug/status", adminToken, "application/json",
		`{"players":1,"maxPlayers":2}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 when debug endpoints are off", rec.Code)
	}
	if fx.svc.statusID != "" {
		t.Fatalf("SelfStatus must not run without the flag, got %q", fx.svc.statusID)
	}
}

func TestDebugEndpointIsPresentWithFlagForAdminOnly(t *testing.T) {
	fx := newFixture(t, Config{DebugEndpoints: true}, nil)
	rec := fx.call(t, http.MethodPost, "/v1/instances/lobby-1/debug/status", adminToken, "application/json",
		`{"players":1,"maxPlayers":2}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if fx.svc.statusID != "lobby-1" {
		t.Fatalf("SelfStatus called with %q, want lobby-1", fx.svc.statusID)
	}
	rec = fx.call(t, http.MethodPost, "/v1/instances/lobby-1/debug/status", gameToken, "application/json",
		`{"players":1,"maxPlayers":2}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("instance status = %d, want 403", rec.Code)
	}
}

func TestInstanceLogsReturnsPlainText(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.logsBody = "line one\nline two\n"
	rec := fx.call(t, http.MethodGet, "/v1/instances/lobby-1/logs", adminToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", ct)
	}
	if got := rec.Body.String(); got != "line one\nline two\n" {
		t.Fatalf("body = %q", got)
	}
	if fx.svc.logsTail != 200 {
		t.Fatalf("default tail = %d, want 200", fx.svc.logsTail)
	}
}

func TestTemplateArchiveWithColonDigestReturnsGzip(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.tpl.archives["sha256:abc"] = gzipBytes(t, "template-content")
	rec := fx.call(t, http.MethodGet, "/v1/templates/lobby-config/versions/sha256:abc/archive", gameToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/gzip" {
		t.Fatalf("Content-Type = %q, want application/gzip", ct)
	}
	if got := gunzipOrFail(t, rec.Body.Bytes()); got != "template-content" {
		t.Fatalf("archive content = %q", got)
	}
}

func TestUnknownTemplateVersionReturns404(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodGet, "/v1/templates/lobby-config/versions/sha256:fff/archive", adminToken, "", "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUploadTemplateVersionReturns201WithDigest(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	body := string(gzipBytes(t, "upload"))
	rec := fx.call(t, http.MethodPost, "/v1/templates/lobby-config/versions", adminToken, "application/gzip", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	var v struct {
		Name   string `json:"name"`
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode version: %v", err)
	}
	if v.Name != "lobby-config" || !strings.HasPrefix(v.Digest, "sha256:") || v.Size != int64(len(body)) {
		t.Fatalf("version = %+v", v)
	}
}

func TestEventsRouteDelegatesForProxyToken(t *testing.T) {
	delegate := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("id: 1\nevent: resync\ndata: {}\n\n"))
	})
	fx := newFixture(t, Config{}, delegate)
	rec := fx.call(t, http.MethodGet, "/v1/events", proxyToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "event: resync") {
		t.Fatalf("event handler was not delegated to, body = %q", rec.Body.String())
	}
}

func TestEventsRouteRejectsGameTokenWith403(t *testing.T) {
	delegate := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("event handler must not run for a game token")
	})
	fx := newFixture(t, Config{}, delegate)
	rec := fx.call(t, http.MethodGet, "/v1/events", gameToken, "", "")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestEventsRouteRequiresToken(t *testing.T) {
	fx := newFixture(t, Config{}, http.NotFoundHandler())
	rec := fx.call(t, http.MethodGet, "/v1/events", "", "", "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

// gzipBytes returns the gzip encoding of content, for fixtures that need a known digest.
func gzipBytes(t *testing.T, content string) []byte {
	t.Helper()
	fx := newFakeTemplates()
	digest := fx.addArchive(t, content)
	return fx.archives[digest]
}
