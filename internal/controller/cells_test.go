package controller

import (
	"errors"
	"testing"

	"github.com/OneLiteFeatherNET/grus/internal/driver"
	"github.com/OneLiteFeatherNET/grus/internal/model"
)

func shardedSpec(min int, cells ...model.Cell) model.PoolSpec {
	s := dynamicSpec(min, 0)
	s.Sharding = &model.Sharding{Cells: cells}
	return s
}

var (
	cellR0 = model.Cell{Name: "r0", MinX: 0, MinZ: 0, MaxX: 31, MaxZ: 31}
	cellR1 = model.Cell{Name: "r1", MinX: 32, MinZ: 0, MaxX: 63, MaxZ: 31}
	cellR2 = model.Cell{Name: "r2", MinX: 0, MinZ: 32, MaxX: 31, MaxZ: 63}
)

// readyInstances starts the pool's instances and brings all of them to Ready before the next reconcile, so
// the cells are spread over all of them (the first Ready instance would otherwise take every cell and keep it).
func (f *fixture) readyInstances(pool string, n int) []string {
	f.t.Helper()
	f.reconcile()
	var ids []string
	for i, s := range f.drv.Started() {
		if s.Instance.Pool == pool {
			f.drv.SetPhase(s.Instance.ID, driver.PhaseRunning, "10.0.0."+string(rune('1'+i))+":25565")
			ids = append(ids, s.Instance.ID)
		}
	}
	if len(ids) != n {
		f.t.Fatalf("started %d instances, want %d", len(ids), n)
	}
	f.reconcile()
	for _, id := range ids {
		if _, err := f.c.SelfReady(f.ctx, id); err != nil {
			f.t.Fatalf("self ready %s: %v", id, err)
		}
	}
	f.reconcile()
	return ids
}

func (f *fixture) cellOwners() map[string]string {
	f.t.Helper()
	out := map[string]string{}
	for _, inst := range f.instancesOf("world") {
		for _, cell := range cellsOf(inst) {
			out[cell] = inst.ID
		}
	}
	return out
}

func TestCellsAreSpreadOverReadyInstances(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1, cellR2))
	f.readyInstances("world", 2)

	owners := f.cellOwners()

	if len(owners) != 3 {
		t.Fatalf("assigned cells = %v, want all three", owners)
	}
	per := map[string]int{}
	for _, id := range owners {
		per[id]++
	}
	if len(per) != 2 {
		t.Errorf("cells per instance = %v, want both instances to serve cells", per)
	}
	if n := f.pub.count(model.EventPoolCells); n == 0 {
		t.Errorf("no pool.cells event published")
	}
}

func TestCellsStayWithTheirInstanceAndNoEventWithoutChange(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1, cellR2))
	f.readyInstances("world", 2)
	before := f.cellOwners()
	events := f.pub.count(model.EventPoolCells)

	f.reconcile()
	f.reconcile()

	after := f.cellOwners()
	for cell, id := range before {
		if after[cell] != id {
			t.Errorf("cell %s moved from %s to %s while the instance stayed Ready", cell, id, after[cell])
		}
	}
	if n := f.pub.count(model.EventPoolCells); n != events {
		t.Errorf("pool.cells events = %d, want %d: nothing changed", n, events)
	}
}

func TestCellsOfALostInstanceMoveToTheOneWithFewestCells(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1, cellR2))
	ids := f.readyInstances("world", 2)
	before := f.cellOwners()
	var lost, kept string
	for _, id := range ids {
		n := 0
		for _, o := range before {
			if o == id {
				n++
			}
		}
		if n == 2 {
			kept = id
		} else {
			lost = id
		}
	}
	events := f.pub.count(model.EventPoolCells)

	f.drv.SetPhase(lost, driver.PhaseFailed, "")
	f.reconcile()

	after := f.cellOwners()
	for cell, id := range after {
		if id != kept {
			t.Errorf("cell %s is on %s, want the only Ready instance %s", cell, id, kept)
		}
	}
	if len(after) != 3 {
		t.Errorf("assigned cells = %v, want all three on %s", after, kept)
	}
	if f.pub.count(model.EventPoolCells) <= events {
		t.Errorf("no pool.cells event after the reassignment")
	}
}

func TestACellWithoutAReadyInstanceStaysUnassigned(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(1, cellR0))
	f.reconcile() // the instance is only Requested/Preparing

	if owners := f.cellOwners(); len(owners) != 0 {
		t.Fatalf("assigned cells = %v, want none before an instance is Ready", owners)
	}
	if _, err := f.c.CellAt(f.ctx, "world", 5, 5); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("cell at without an instance = %v, want ErrInvalidState", err)
	}
}

func TestPoolCellsAndCellAtResolveBlockCoordinates(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1))
	f.readyInstances("world", 2)

	cells, err := f.c.PoolCells(f.ctx, "world")
	if err != nil || len(cells) != 2 || cells[0].Instance == "" || cells[0].Address == "" {
		t.Fatalf("cells = %+v (%v), want two assigned cells with addresses", cells, err)
	}
	// Block 520 is chunk 32, the first chunk of r1.
	at, err := f.c.CellAt(f.ctx, "world", 520, 10)
	if err != nil || at.Cell != "r1" {
		t.Fatalf("cell at block 520,10 = %+v (%v), want r1", at, err)
	}
	// Block -1 is chunk -1: outside every cell.
	if _, err := f.c.CellAt(f.ctx, "world", -1, 0); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cell at block -1,0 = %v, want ErrNotFound", err)
	}
}

func TestPoolCellsOfAnUnshardedPoolIsNotFound(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("lobby", dynamicSpec(1, 0))

	if _, err := f.c.PoolCells(f.ctx, "lobby"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cells of an unsharded pool = %v, want ErrNotFound", err)
	}
	if _, err := f.c.PoolCells(f.ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cells of an unknown pool = %v, want ErrNotFound", err)
	}
}

func TestRemovedCellsLoseTheirLabel(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(1, cellR0, cellR1))
	f.readyInstances("world", 1)

	f.putPool("world", shardedSpec(1, cellR1))
	f.reconcile()

	if owners := f.cellOwners(); len(owners) != 1 || owners["r1"] == "" {
		t.Fatalf("owners = %v, want only r1", owners)
	}
}

func TestLateReadyInstanceGetsOneOfTwoCells(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1))
	f.reconcile()
	started := f.drv.Started()
	first, second := started[0].Instance.ID, started[1].Instance.ID
	f.bringUp(first, "10.0.0.1:25565")
	f.reconcile()
	if owners := f.cellOwners(); owners["r0"] != first || owners["r1"] != first {
		t.Fatalf("owners = %v, want the only Ready instance to hold both cells", owners)
	}

	f.bringUp(second, "10.0.0.2:25565")
	f.reconcile()

	owners := f.cellOwners()
	if owners["r0"] != first || owners["r1"] != second {
		t.Fatalf("owners = %v, want r0 kept by %s and r1 (last by name) moved to %s", owners, first, second)
	}
	events := f.pub.count(model.EventPoolCells)
	f.reconcile()
	if f.pub.count(model.EventPoolCells) != events {
		t.Errorf("pool.cells published again although nothing changed")
	}
}

func TestBalancedCellsAreNotMoved(t *testing.T) {
	f := newFixture(t, Config{})
	f.putPool("world", shardedSpec(2, cellR0, cellR1, cellR2))
	f.readyInstances("world", 2)
	before := f.cellOwners()

	f.reconcile()

	for cell, id := range f.cellOwners() {
		if before[cell] != id {
			t.Errorf("cell %s moved from %s to %s although counts differ by one", cell, before[cell], id)
		}
	}
}
