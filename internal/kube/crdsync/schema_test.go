package crdsync

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"

	"github.com/OneLiteFeatherNET/grus/internal/model"
)

// schemaNode is the part of an OpenAPI v3 schema these tests look at.
type schemaNode struct {
	Type                 string                `json:"type"`
	Properties           map[string]schemaNode `json:"properties"`
	Items                *schemaNode           `json:"items"`
	AdditionalProperties *schemaNode           `json:"additionalProperties"`
}

type crdFile struct {
	Spec struct {
		Group    string `json:"group"`
		Scope    string `json:"scope"`
		Names    struct{ Kind, Plural string }
		Versions []struct {
			Name         string `json:"name"`
			Served       bool   `json:"served"`
			Storage      bool   `json:"storage"`
			Subresources struct {
				Status *struct{} `json:"status"`
			} `json:"subresources"`
			Schema struct {
				OpenAPIV3Schema schemaNode `json:"openAPIV3Schema"`
			} `json:"schema"`
		} `json:"versions"`
	} `json:"spec"`
}

func loadCRD(t *testing.T, name string) crdFile {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "crds", name+".grus.onelitefeather.net.yaml"))
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}
	var crd crdFile
	if err := yaml.Unmarshal(raw, &crd); err != nil {
		t.Fatalf("parse CRD %s: %v", name, err)
	}
	return crd
}

// jsonFields maps the JSON field names of a struct type to the type of the field.
func jsonFields(t reflect.Type) map[string]reflect.Type {
	out := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		out[name] = f.Type
	}
	return out
}

// element strips pointers, slices and maps so that a struct inside them is found.
func element(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice || t.Kind() == reflect.Map {
		t = t.Elem()
	}
	return t
}

// checkSchema compares a Go struct with a schema in both directions: every JSON field needs a schema
// property and every schema property a JSON field, recursively through nested structs.
func checkSchema(t *testing.T, path string, typ reflect.Type, node schemaNode) {
	t.Helper()
	fields := jsonFields(typ)
	for name, ft := range fields {
		prop, ok := node.Properties[name]
		if !ok {
			t.Errorf("%s.%s is a JSON field of %s but missing from the CRD schema", path, name, typ.Name())
			continue
		}
		if inner := element(ft); inner.Kind() == reflect.Struct && inner.PkgPath() == typ.PkgPath() {
			sub := prop
			if prop.Items != nil {
				sub = *prop.Items
			}
			checkSchema(t, path+"."+name, inner, sub)
		}
	}
	for name := range node.Properties {
		if _, ok := fields[name]; !ok {
			t.Errorf("%s.%s is in the CRD schema but not a JSON field of %s", path, name, typ.Name())
		}
	}
}

func TestCRDSchemasMatchTheDomainSpecs(t *testing.T) {
	for _, tc := range []struct {
		plural, kind string
		spec         reflect.Type
	}{
		{"environments", "Environment", reflect.TypeOf(model.EnvironmentSpec{})},
		{"groups", "Group", reflect.TypeOf(model.GroupSpec{})},
		{"pools", "Pool", reflect.TypeOf(model.PoolSpec{})},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			crd := loadCRD(t, tc.plural)
			if crd.Spec.Group != Group || crd.Spec.Scope != "Namespaced" || crd.Spec.Names.Kind != tc.kind || crd.Spec.Names.Plural != tc.plural {
				t.Fatalf("CRD identity = %+v", crd.Spec)
			}
			if len(crd.Spec.Versions) != 1 {
				t.Fatalf("versions = %d, want 1", len(crd.Spec.Versions))
			}
			v := crd.Spec.Versions[0]
			if v.Name != Version || !v.Served || !v.Storage || v.Subresources.Status == nil {
				t.Errorf("version = %+v, want served and stored %s with a status subresource", v, Version)
			}
			spec, ok := v.Schema.OpenAPIV3Schema.Properties["spec"]
			if !ok {
				t.Fatal("schema has no spec")
			}
			checkSchema(t, "spec", tc.spec, spec)
		})
	}
}

// strictDecode decodes a YAML document with unknown fields rejected.
func strictDecode(t *testing.T, raw []byte, into any) {
	t.Helper()
	js, err := yaml.YAMLToJSON(raw)
	if err != nil {
		t.Fatalf("yaml: %v", err)
	}
	var doc struct {
		Spec json.RawMessage `json:"spec"`
	}
	if err := json.Unmarshal(js, &doc); err != nil {
		t.Fatalf("json: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(doc.Spec))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		t.Fatalf("decode spec: %v", err)
	}
}

func TestExampleCustomResourcesAreValid(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "examples", "crds")
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return raw
	}
	var env model.EnvironmentSpec
	strictDecode(t, read("environment-minestom.yaml"), &env)
	if errs := env.WithDefaults().Validate(); len(errs) > 0 {
		t.Errorf("environment-minestom: %v", errs)
	}
	var grp model.GroupSpec
	strictDecode(t, read("group-lobby.yaml"), &grp)
	if errs := grp.Validate(); len(errs) > 0 {
		t.Errorf("group-lobby: %v", errs)
	}
	for _, name := range []string{"pool-lobby.yaml", "pool-mc01-lobby.yaml"} {
		var pool model.PoolSpec
		strictDecode(t, read(name), &pool)
		if errs := pool.WithDefaults().Validate(); len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
}
