package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func cellFixture(t *testing.T) fixture {
	fx := newFixture(t, Config{}, nil)
	fx.svc.cells = []model.CellInfo{{Cell: "r0", Bounds: model.CellBounds{MinX: 0, MinZ: 0, MaxX: 31, MaxZ: 31}, Instance: "world-a", Address: "10.0.0.1:25565"}}
	return fx
}

func TestListPoolCellsReturnsTheAssignment(t *testing.T) {
	fx := cellFixture(t)

	rec := fx.call(t, http.MethodGet, "/v1/pools/world/cells", adminToken, "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	var got []model.CellInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || len(got) != 1 || got[0].Instance != "world-a" || got[0].Bounds.MaxX != 31 {
		t.Fatalf("body = %s (%v), want the cell r0 owned by world-a", rec.Body, err)
	}
	if fx.svc.cellsPool != "world" {
		t.Errorf("pool = %q, want world", fx.svc.cellsPool)
	}
}

func TestCellAtPassesBlockCoordinates(t *testing.T) {
	fx := cellFixture(t)

	rec := fx.call(t, http.MethodGet, "/v1/pools/world/cells/at?x=-100&z=250", adminToken, "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	if fx.svc.cellX != -100 || fx.svc.cellZ != 250 {
		t.Errorf("service got x=%d z=%d, want -100 and 250", fx.svc.cellX, fx.svc.cellZ)
	}
}

func TestCellAtRequiresBothCoordinates(t *testing.T) {
	fx := cellFixture(t)

	rec := fx.call(t, http.MethodGet, "/v1/pools/world/cells/at?x=1", adminToken, "", "")

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestCellAtMapsServiceErrors(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want int
	}{
		{controller.ErrNotFound, http.StatusNotFound},
		{fmt.Errorf("%w: cell r0 has no ready instance", controller.ErrInvalidState), http.StatusConflict},
	} {
		fx := cellFixture(t)
		fx.svc.cellsErr = tc.err

		rec := fx.call(t, http.MethodGet, "/v1/pools/world/cells/at?x=1&z=1", adminToken, "", "")

		if rec.Code != tc.want {
			t.Errorf("%v: status = %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}

func TestProxyTokenMayResolveCells(t *testing.T) {
	fx := cellFixture(t)

	rec := fx.call(t, http.MethodGet, "/v1/pools/world/cells/at?x=1&z=1", proxyToken, "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("proxy token status = %d, want 200 (instances:read)", rec.Code)
	}
}
