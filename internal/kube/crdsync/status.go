package crdsync

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"github.com/OneLiteFeatherNET/grus/internal/controller"
	"github.com/OneLiteFeatherNET/grus/internal/model"
	"github.com/OneLiteFeatherNET/grus/internal/store"
)

// Reasons of the Ready condition.
const (
	ReasonApplied     = "Applied"
	ReasonProgressing = "Progressing"
	ReasonInvalid     = "Invalid"
	ReasonConflict    = "Conflict"
	ReasonError       = "Error"
)

const conditionReady = "Ready"

// result is the outcome of the last apply of one custom resource.
type result struct {
	ok      bool
	reason  string
	message string
}

func resultOf(err error) result {
	var ve *controller.ValidationError
	var de *decodeError
	switch {
	case err == nil:
		return result{ok: true, reason: ReasonApplied}
	case errors.As(err, &ve):
		return result{reason: ReasonInvalid, message: fieldMessage(ve.Fields)}
	case errors.As(err, &de):
		return result{reason: ReasonInvalid, message: de.Error()}
	case errors.Is(err, controller.ErrManagedElsewhere):
		return result{reason: ReasonConflict, message: err.Error()}
	default:
		return result{reason: ReasonError, message: err.Error()}
	}
}

// writeStatus computes the status of obj and patches it into the status subresource when it changed. With
// force false, a change that only affects replica counts is skipped while the last write is younger than
// StatusInterval. The caller holds s.mu.
func (s *Syncer) writeStatus(ctx context.Context, k kindDef, obj *unstructured.Unstructured, force bool) {
	name := obj.GetName()
	id := key(k, name)
	now := s.clock.Now()
	res, ok := s.results[id]
	if !ok {
		return
	}
	// The informer cache lags behind our own writes, so the last written status is the better baseline.
	current, ok := s.written[id]
	if !ok {
		current, _, _ = unstructured.NestedMap(obj.Object, "status")
	}
	desired, err := s.desiredStatus(ctx, k, obj, current, res, now)
	if err != nil {
		s.log.Error("crdsync: compute status", "kind", k.kind, "name", name, "error", err)
		return
	}
	if equality.Semantic.DeepEqual(current, desired) {
		return
	}
	if !force && sameButCounts(current, desired) && now.Sub(s.lastWrite[id]) < s.cfg.StatusInterval {
		return
	}
	body, err := json.Marshal(map[string]any{"status": desired})
	if err != nil {
		s.log.Error("crdsync: encode status", "kind", k.kind, "name", name, "error", err)
		return
	}
	if _, err := s.client.Resource(k.gvr).Namespace(s.cfg.Namespace).Patch(ctx, name, types.MergePatchType, body, metav1.PatchOptions{}, "status"); err != nil {
		s.log.Warn("crdsync: write status", "kind", k.kind, "name", name, "error", err)
		return
	}
	s.lastWrite[id] = now
	s.written[id] = desired
}

// desiredStatus builds the status of obj from the apply result and, for pools, the instance counts. A
// condition whose status is unchanged keeps its lastTransitionTime.
func (s *Syncer) desiredStatus(ctx context.Context, k kindDef, obj *unstructured.Unstructured, current map[string]any, res result, now time.Time) (map[string]any, error) {
	status := map[string]any{"observedGeneration": obj.GetGeneration()}
	cond := map[string]any{
		"type":               conditionReady,
		"status":             "True",
		"reason":             res.reason,
		"message":            res.message,
		"observedGeneration": obj.GetGeneration(),
	}
	if !res.ok {
		cond["status"] = "False"
	}
	if k.kind == "Pool" {
		replicas, ready, allocated, err := s.countInstances(ctx, obj.GetName())
		if err != nil {
			return nil, err
		}
		status["replicas"], status["readyReplicas"], status["allocatedReplicas"] = replicas, ready, allocated
		if res.ok {
			if min := minReplicas(obj); ready+allocated < min {
				cond["status"] = "False"
				cond["reason"] = ReasonProgressing
				cond["message"] = "fewer instances are ready than scaling.min"
			}
		}
	}
	cond["lastTransitionTime"] = transitionTime(current, cond, now)
	status["conditions"] = []any{cond}
	return status, nil
}

// transitionTime keeps the previous lastTransitionTime while the Ready status stays the same.
func transitionTime(current map[string]any, cond map[string]any, now time.Time) string {
	conds, _ := current["conditions"].([]any)
	for _, c := range conds {
		m, ok := c.(map[string]any)
		if !ok || m["type"] != conditionReady {
			continue
		}
		if t, _ := m["lastTransitionTime"].(string); t != "" && m["status"] == cond["status"] {
			return t
		}
	}
	return now.UTC().Format(time.RFC3339)
}

// minReplicas reads spec.scaling.min; a pool is Ready once that many instances are Ready or Allocated.
func minReplicas(obj *unstructured.Unstructured) int64 {
	v, _, _ := unstructured.NestedInt64(obj.Object, "spec", "scaling", "min")
	return v
}

func (s *Syncer) countInstances(ctx context.Context, pool string) (replicas, ready, allocated int64, err error) {
	insts, err := s.be.ListInstances(ctx, store.InstanceFilter{Pool: pool})
	if err != nil {
		return 0, 0, 0, err
	}
	for _, i := range insts {
		if i.State.Terminal() {
			continue
		}
		replicas++
		switch i.State {
		case model.StateReady:
			ready++
		case model.StateAllocated:
			allocated++
		}
	}
	return replicas, ready, allocated, nil
}

// sameButCounts reports whether two statuses differ in nothing but the replica counts.
func sameButCounts(a, b map[string]any) bool {
	strip := func(m map[string]any) map[string]any {
		c := make(map[string]any, len(m))
		for k, v := range m {
			switch k {
			case "replicas", "readyReplicas", "allocatedReplicas":
			default:
				c[k] = v
			}
		}
		return c
	}
	return equality.Semantic.DeepEqual(strip(a), strip(b))
}
