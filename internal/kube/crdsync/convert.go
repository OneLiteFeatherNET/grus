package crdsync

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// decodeSpec converts the spec of a custom resource into the domain spec through a JSON round trip. Unknown
// fields are ignored on purpose (the CRD schema prunes them anyway); a field of the wrong type is a
// decoding error that is reported to the user like a validation error.
func decodeSpec(obj *unstructured.Unstructured, into any) error {
	spec, found, err := unstructured.NestedFieldNoCopy(obj.Object, "spec")
	if err != nil || !found || spec == nil {
		return nil
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("spec: %w", err)
	}
	return nil
}

// fieldMessage joins field errors into one condition message.
func fieldMessage(fields []model.FieldError) string {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		parts = append(parts, f.Field+": "+f.Message)
	}
	return strings.Join(parts, "; ")
}
