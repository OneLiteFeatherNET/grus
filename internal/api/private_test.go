package api

import (
	"net/http"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
)

const privateBody = `{"pool":"home","owner":"alex"}`

func TestCreatePrivateServerAnswers201ForANewInstance(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.privateCreated = true

	rec := fx.call(t, http.MethodPost, "/v1/private-servers", adminToken, "application/json", privateBody)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body %s", rec.Code, rec.Body)
	}
	if fx.svc.privatePool != "home" || fx.svc.privateOwner != "alex" {
		t.Errorf("service got pool %q owner %q", fx.svc.privatePool, fx.svc.privateOwner)
	}
}

func TestCreatePrivateServerAnswers200ForTheExistingInstance(t *testing.T) {
	fx := newFixture(t, Config{}, nil)

	rec := fx.call(t, http.MethodPost, "/v1/private-servers", adminToken, "application/json", privateBody)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
}

func TestCreatePrivateServerQuotaAnswers409(t *testing.T) {
	fx := newFixture(t, Config{}, nil)
	fx.svc.privateErr = controller.ErrQuota

	rec := fx.call(t, http.MethodPost, "/v1/private-servers", adminToken, "application/json", privateBody)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestCreatePrivateServerRejectsABadOwner(t *testing.T) {
	fx := newFixture(t, Config{}, nil)

	rec := fx.call(t, http.MethodPost, "/v1/private-servers", adminToken, "application/json", `{"pool":"home","owner":"a b"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestListPrivateServersPassesTheOwnerAndReturnsAnArray(t *testing.T) {
	fx := newFixture(t, Config{}, nil)

	rec := fx.call(t, http.MethodGet, "/v1/private-servers?owner=alex", adminToken, "", "")

	if rec.Code != http.StatusOK || rec.Body.String() != "[]\n" && rec.Body.String() != "[]" {
		t.Fatalf("status %d body %q, want 200 and []", rec.Code, rec.Body)
	}
	if fx.svc.privateOwnerFilter != "alex" {
		t.Errorf("owner filter = %q, want alex", fx.svc.privateOwnerFilter)
	}
}

func TestStopPrivateServerCallsTheService(t *testing.T) {
	fx := newFixture(t, Config{}, nil)

	rec := fx.call(t, http.MethodDelete, "/v1/private-servers/home-x", adminToken, "", "")

	if rec.Code != http.StatusOK || fx.svc.stopID != "home-x" {
		t.Fatalf("status %d stopID %q, want 200 and home-x", rec.Code, fx.svc.stopID)
	}
}

func TestPrivateServerRoutesNeedAToken(t *testing.T) {
	fx := newFixture(t, Config{}, nil)

	rec := fx.call(t, http.MethodPost, "/v1/private-servers", gameToken, "application/json", privateBody)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("instance token status = %d, want 403 (no instances:write)", rec.Code)
	}
}
