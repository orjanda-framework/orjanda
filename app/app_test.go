package app_test

import (
	"testing"

	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/errors"
)

func TestResolveDAG_Success(t *testing.T) {
	apps := []app.Definition{
		{
			Name: "AppC",
			Dependencies: []app.Dependency{
				{App: "AppB"},
				{App: "AppA"},
			},
		},
		{
			Name: "AppB",
			Dependencies: []app.Dependency{
				{App: "AppA"},
			},
		},
		{
			Name: "AppA",
		},
	}

	sorted, err := app.ResolveDAG(apps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 sorted apps, got %d", len(sorted))
	}

	// Verify AppA is first, AppB is second, AppC is third
	if sorted[0].Name != "AppA" {
		t.Errorf("expected AppA to be first, got %s", sorted[0].Name)
	}
	if sorted[1].Name != "AppB" {
		t.Errorf("expected AppB to be second, got %s", sorted[1].Name)
	}
	if sorted[2].Name != "AppC" {
		t.Errorf("expected AppC to be third, got %s", sorted[2].Name)
	}
}

func TestResolveDAG_CircularDependency(t *testing.T) {
	apps := []app.Definition{
		{
			Name: "AppA",
			Dependencies: []app.Dependency{
				{App: "AppB"},
			},
		},
		{
			Name: "AppB",
			Dependencies: []app.Dependency{
				{App: "AppC"},
			},
		},
		{
			Name: "AppC",
			Dependencies: []app.Dependency{
				{App: "AppA"},
			},
		},
	}

	_, err := app.ResolveDAG(apps)
	if err == nil {
		t.Fatal("expected error due to circular dependency")
	}

	if !errors.Is(err, errors.ErrInternal) {
		t.Errorf("expected internal error, got %v", err)
	}
}

func TestResolveDAG_MissingDependency(t *testing.T) {
	apps := []app.Definition{
		{
			Name: "AppA",
			Dependencies: []app.Dependency{
				{App: "AppB"},
			},
		},
	}

	_, err := app.ResolveDAG(apps)
	if err == nil {
		t.Fatal("expected error due to missing dependency")
	}

	if !errors.Is(err, errors.ErrValidation) {
		t.Errorf("expected validation error, got %v", err)
	}
}
