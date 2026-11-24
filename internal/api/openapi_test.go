package api

import (
	"bytes"
	"os"
	"testing"
)

// openAPIFile is the checked-in document at the repository root (go test runs in the package directory).
const openAPIFile = "../../api/openapi.yaml"

// TestOpenAPIUpToDate fails when the checked-in api/openapi.yaml differs from OpenAPI().
// Run with GRUS_UPDATE_OPENAPI=1 to rewrite the file.
func TestOpenAPIUpToDate(t *testing.T) {
	generated, err := OpenAPI()
	if err != nil {
		t.Fatalf("generate OpenAPI document: %v", err)
	}
	if os.Getenv("GRUS_UPDATE_OPENAPI") == "1" {
		if err := os.WriteFile(openAPIFile, generated, 0o644); err != nil {
			t.Fatalf("write %s: %v", openAPIFile, err)
		}
		return
	}
	checkedIn, err := os.ReadFile(openAPIFile)
	if err != nil {
		t.Fatalf("read %s: %v (run with GRUS_UPDATE_OPENAPI=1 to create it)", openAPIFile, err)
	}
	if !bytes.Equal(checkedIn, generated) {
		t.Fatalf("%s is out of date with OpenAPI(); run GRUS_UPDATE_OPENAPI=1 go test ./internal/api/ -run TestOpenAPIUpToDate", openAPIFile)
	}
}
