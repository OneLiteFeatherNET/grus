package model

import (
	"testing"
	"time"
)

func privatePool() PoolSpec {
	return PoolSpec{
		Role: RoleGame, Mode: ModeDynamic, Driver: DriverKubernetes, Image: "x:1",
		Resources: Resources{MemoryMiB: 512},
		Scaling:   Scaling{Max: 4},
		Private:   &Private{},
	}
}

func fieldsOf(errs []FieldError) map[string]bool {
	out := map[string]bool{}
	for _, e := range errs {
		out[e.Field] = true
	}
	return out
}

func TestPrivateDefaultsDoNotMutateTheInput(t *testing.T) {
	in := privatePool()

	got := in.WithDefaults()

	if got.Private.QuotaPerOwner != 1 || got.Private.IdleTimeout.D() != 10*time.Minute {
		t.Errorf("defaults = %+v, want quota 1 and idle 10m", *got.Private)
	}
	if in.Private.QuotaPerOwner != 0 {
		t.Errorf("WithDefaults changed the caller's private block: %+v", *in.Private)
	}
}

func TestPrivatePoolValidation(t *testing.T) {
	ok := privatePool()
	if errs := ok.WithDefaults().Validate(); len(errs) != 0 {
		t.Fatalf("valid private pool rejected: %v", errs)
	}

	bad := privatePool()
	bad.Scaling.Min = 1
	bad.Scaling.Buffer = 1
	bad.Mode = ModeStatic
	bad.Private.QuotaPerOwner = -1
	got := fieldsOf(bad.WithDefaults().Validate())
	for _, f := range []string{"scaling.min", "scaling.buffer", "mode", "private.quotaPerOwner"} {
		if !got[f] {
			t.Errorf("no error for %s in %v", f, got)
		}
	}
}

func TestShardingValidation(t *testing.T) {
	spec := privatePool()
	spec.Private = nil
	spec.Sharding = &Sharding{Cells: []Cell{
		{Name: "r0", MinX: 0, MinZ: 0, MaxX: 31, MaxZ: 31},
		{Name: "r0", MinX: 40, MinZ: 0, MaxX: 50, MaxZ: 5},
		{Name: "r2", MinX: 30, MinZ: 30, MaxX: 40, MaxZ: 40},
		{Name: "bad,name", MinX: 100, MinZ: 0, MaxX: 90, MaxZ: 5},
	}}

	got := fieldsOf(spec.WithDefaults().Validate())

	for _, f := range []string{"sharding.cells[1].name", "sharding.cells[2]", "sharding.cells[3].name"} {
		if !got[f] {
			t.Errorf("no error for %s in %v", f, got)
		}
	}
}

func TestShardingDefaultsAndValidSpec(t *testing.T) {
	spec := privatePool()
	spec.Private = nil
	spec.Sharding = &Sharding{Cells: []Cell{{Name: "r0", MaxX: 31, MaxZ: 31}, {Name: "r1", MinX: 32, MaxX: 63, MaxZ: 31}}}

	got := spec.WithDefaults()

	if got.Sharding.CellSize != 32 || spec.Sharding.CellSize != 0 {
		t.Errorf("cellSize = %d (input %d), want 32 without touching the input", got.Sharding.CellSize, spec.Sharding.CellSize)
	}
	if errs := got.Validate(); len(errs) != 0 {
		t.Errorf("valid sharding rejected: %v", errs)
	}
}

func TestChunkOfFloorsNegativeBlocks(t *testing.T) {
	for block, want := range map[int]int{0: 0, 15: 0, 16: 1, -1: -1, -16: -1, -17: -2} {
		if got := ChunkOf(block); got != want {
			t.Errorf("ChunkOf(%d) = %d, want %d", block, got, want)
		}
	}
}
