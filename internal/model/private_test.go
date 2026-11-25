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
