package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

const validEnvironmentBody = `{"kind":"jvm","jvm":{"jar":"paper.jar","flags":["-XX:+UseG1GC"]},"readiness":"tcp","gracePeriod":"45s"}`

func TestAdminPutsEnvironmentAndGetsItBack(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/environments/paper", adminToken, "application/json", validEnvironmentBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var env model.Environment
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode environment: %v", err)
	}
	if env.Name != "paper" || env.Source != model.SourceAPI || fx.svc.envSpec.JVM == nil || fx.svc.envSpec.JVM.Jar != "paper.jar" {
		t.Errorf("environment = %+v, spec passed to service = %+v", env, fx.svc.envSpec)
	}
	if got := fx.call(t, http.MethodGet, "/v1/environments/paper", adminToken, "", ""); got.Code != http.StatusOK {
		t.Errorf("get status = %d, want 200", got.Code)
	}
	if got := fx.call(t, http.MethodGet, "/v1/environments", adminToken, "", ""); got.Body.String() != "[]\n" {
		t.Errorf("empty list body = %q, want a top-level empty array", got.Body.String())
	}
}

func TestPutGroupAndDeleteRoutes(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/groups/base", adminToken, "application/json", `{"env":{"A":"1"}}`)
	if rec.Code != http.StatusOK || fx.svc.groupName != "base" {
		t.Fatalf("put group status = %d name = %q; body %s", rec.Code, fx.svc.groupName, rec.Body)
	}
	if got := fx.call(t, http.MethodDelete, "/v1/groups/base", adminToken, "", ""); got.Code != http.StatusNoContent {
		t.Errorf("delete group status = %d, want 204", got.Code)
	}
	if got := fx.call(t, http.MethodDelete, "/v1/environments/paper", adminToken, "", ""); got.Code != http.StatusNoContent || fx.svc.deletedEnv != "paper" {
		t.Errorf("delete environment status = %d deleted = %q", got.Code, fx.svc.deletedEnv)
	}
}

func TestKubernetesManagedObjectReturns409(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.putErr = controller.ErrManagedElsewhere
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/v1/environments/paper", validEnvironmentBody},
		{http.MethodPut, "/v1/groups/base", `{}`},
		{http.MethodPut, "/v1/pools/lobby", validPoolBody},
		{http.MethodDelete, "/v1/groups/base", ""},
	} {
		ct := ""
		if tc.body != "" {
			ct = "application/json"
		}
		if rec := fx.call(t, tc.method, tc.path, adminToken, ct, tc.body); rec.Code != http.StatusConflict {
			t.Errorf("%s %s status = %d, want 409; body %s", tc.method, tc.path, rec.Code, rec.Body)
		}
	}
}

func TestInvalidEnvironmentReturns422(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.putErr = &controller.ValidationError{Kind: "environment", Fields: []model.FieldError{{Field: "kind", Message: "must be jvm, native or container"}}}
	rec := fx.call(t, http.MethodPut, "/v1/environments/paper", adminToken, "application/json", `{"kind":"wasm"}`)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body %s", rec.Code, rec.Body)
	}
}

func TestInstanceTokenCannotWriteEnvironments(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	rec := fx.call(t, http.MethodPut, "/v1/environments/paper", gameToken, "application/json", validEnvironmentBody)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestNetworkIsReadableByProxyButNotByGameInstances(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.network = model.NetworkStatus{Players: 12, MaxPlayers: 200, Proxies: 2}

	rec := fx.call(t, http.MethodGet, "/v1/network", proxyToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("proxy status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got model.NetworkStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got != fx.svc.network {
		t.Errorf("network = %+v err=%v, want %+v", got, err, fx.svc.network)
	}
	if rec := fx.call(t, http.MethodGet, "/v1/network", gameToken, "", ""); rec.Code != http.StatusForbidden {
		t.Errorf("game instance status = %d, want 403", rec.Code)
	}
	if rec := fx.call(t, http.MethodGet, "/v1/network", adminToken, "", ""); rec.Code != http.StatusOK {
		t.Errorf("admin status = %d, want 200", rec.Code)
	}
}
