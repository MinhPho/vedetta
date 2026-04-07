package api

import (
	"os"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

func TestOpenAPISpecIsValid(t *testing.T) {
	data, err := os.ReadFile("openapi.yaml")
	if err != nil {
		t.Fatalf("read openapi.yaml: %v", err)
	}

	loader := openapi3.NewLoader()
	spec, err := loader.LoadFromData(data)
	if err != nil {
		t.Fatalf("parse openapi.yaml: %v", err)
	}

	if err := spec.Validate(loader.Context); err != nil {
		t.Fatalf("validate openapi.yaml: %v", err)
	}

	expectedPaths := []string{
		"/api/health",
		"/api/health/live",
		"/api/health/ready",
		"/api/system",
		"/api/openapi.json",
	}
	for _, p := range expectedPaths {
		if spec.Paths.Find(p) == nil {
			t.Errorf("expected path %s not found in spec", p)
		}
	}
}
