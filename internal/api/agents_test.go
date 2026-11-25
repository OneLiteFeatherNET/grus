package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/driver/remote"
	"github.com/OneLiteFeatherNET/grus/internal/driver/router"
)

const agentToken = "mc01-agent-token-secret"

func hashOf(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func agentFixture(t *testing.T) fixture {
	t.Helper()
	reg := remote.NewRegistry(router.New(nil), remote.Config{})
	return newFixture(t, Config{
		AgentTokens: map[string]string{"mc01": hashOf(agentToken), "other": hashOf("other-agent-token")},
		Agents:      reg,
	}, nil)
}

func TestAgentRegistersItsOwnHostAndAdminSeesIt(t *testing.T) {
	fx := agentFixture(t)
	body := `{"version":"0.1.0","address":"10.200.2.35","portRange":"30000-30100","capacity":{"memoryMiB":8192}}`

	rec := fx.call(t, http.MethodPut, "/v1/agents/mc01", agentToken, "application/json", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("register status = %d, want 200; body %s", rec.Code, rec.Body)
	}

	rec = fx.call(t, http.MethodGet, "/v1/hosts/mc01", adminToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get host status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var info remote.HostInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("decode host: %v", err)
	}
	if !info.Online || info.Address != "10.200.2.35" || info.Capacity.MemoryMiB != 8192 || info.Version != "0.1.0" {
		t.Errorf("host = %+v", info)
	}
	rec = fx.call(t, http.MethodGet, "/v1/hosts", adminToken, "", "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"mc01"`) {
		t.Errorf("list hosts = %d %s", rec.Code, rec.Body)
	}
}

func TestAgentTokenCannotUseAnotherHostsRoutes(t *testing.T) {
	fx := agentFixture(t)
	for _, target := range []string{"/v1/agents/other", "/v1/agents/other/desired?wait=0s", "/v1/agents/other/observed"} {
		method, body := http.MethodGet, ""
		if !strings.Contains(target, "desired") {
			method, body = http.MethodPut, `{"instances":[]}`
		}
		rec := fx.call(t, method, target, agentToken, "application/json", body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with the mc01 token = %d, want 403", method, target, rec.Code)
		}
	}
}

func TestAgentTokenHasNoOtherScopes(t *testing.T) {
	fx := agentFixture(t)
	for _, c := range []struct{ method, target string }{
		{http.MethodGet, "/v1/pools"},
		{http.MethodGet, "/v1/instances"},
		{http.MethodGet, "/v1/hosts"},
		{http.MethodGet, "/v1/templates"},
	} {
		if rec := fx.call(t, c.method, c.target, agentToken, "", ""); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s with an agent token = %d, want 403", c.method, c.target, rec.Code)
		}
	}
}

func TestAgentTokenMayDownloadTemplateArchives(t *testing.T) {
	fx := agentFixture(t)
	digest := fx.tpl.addArchive(t, "content")
	rec := fx.call(t, http.MethodGet, "/v1/templates/base/versions/"+digest+"/archive", agentToken, "", "")
	if rec.Code != http.StatusOK {
		t.Errorf("archive status = %d, want 200", rec.Code)
	}
}

func TestWrongAgentTokenReturns401(t *testing.T) {
	fx := agentFixture(t)
	if rec := fx.call(t, http.MethodPut, "/v1/agents/mc01", "not-the-token", "application/json", `{}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestDesiredReturnsAtOnceForAStaleRevisionAndPutObservedAnswers204(t *testing.T) {
	fx := agentFixture(t)
	rec := fx.call(t, http.MethodGet, "/v1/agents/mc01/desired?wait=0s&revision=-1", agentToken, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("desired status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var d remote.Desired
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("decode desired: %v", err)
	}
	if d.Instances == nil || d.Commands == nil {
		t.Errorf("desired = %+v, want empty lists and not null", d)
	}

	rec = fx.call(t, http.MethodPut, "/v1/agents/mc01/observed", agentToken, "application/json",
		`{"revision":1,"instances":[{"id":"stub-1","phase":"Running","address":"10.200.2.35:30000"}],"logs":{"stub-1":["hi"]}}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("observed status = %d, want 204; body %s", rec.Code, rec.Body)
	}
	rec = fx.call(t, http.MethodGet, "/v1/hosts/mc01", adminToken, "", "")
	if !strings.Contains(rec.Body.String(), `"instances":1`) {
		t.Errorf("host after report = %s, want one instance", rec.Body)
	}
}

func TestInvalidWaitIsRejected(t *testing.T) {
	fx := agentFixture(t)
	if rec := fx.call(t, http.MethodGet, "/v1/agents/mc01/desired?wait=soon", agentToken, "", ""); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestUnknownHostIs404(t *testing.T) {
	fx := agentFixture(t)
	if rec := fx.call(t, http.MethodGet, "/v1/hosts/nope", adminToken, "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}
