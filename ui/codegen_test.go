package ui_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	orjandacore "github.com/orjanda-framework/orjanda/orjanda-core"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// compileCoreRegistry compiles the actual core Registry (User, Role, RolePermission)
// to match the committed orjanda-ui/src/generated/schema.json output.
func compileCoreRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	if err := reg.Register("core", &orjandacore.User{}); err != nil {
		t.Fatalf("register User: %v", err)
	}
	if err := reg.Register("core", &orjandacore.Role{}); err != nil {
		t.Fatalf("register Role: %v", err)
	}
	if err := reg.Register("core", &orjandacore.RolePermission{}); err != nil {
		t.Fatalf("register RolePermission: %v", err)
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile core registry: %v", err)
	}
	return reg
}

// TestCommittedCodegenOutputConsistent is the commit-time generated-output
// consistency check (REVIEW-2026-08-12 finding 5 remediation): the checked-in
// orjanda-ui/src/generated/ output must match what the reference Registry
// produces. schema.json is compared without node; the TypeScript files are
// regenerated into a scratch dir and byte-compared when node is available.
func TestCommittedCodegenOutputConsistent(t *testing.T) {
	reg := compileCoreRegistry(t)
	committedDir := filepath.Join("..", "orjanda-ui", "src", "generated")

	committed, err := os.ReadFile(filepath.Join(committedDir, "schema.json"))
	if err != nil {
		t.Fatalf("committed schema.json missing: %v", err)
	}
	fresh, err := ui.MarshalInput(reg)
	if err != nil {
		t.Fatalf("MarshalInput: %v", err)
	}
	if !bytes.Equal(committed, fresh) {
		t.Errorf("committed orjanda-ui/src/generated/schema.json is stale: run `npm run codegen` (or `orjanda serve` once) and commit the regenerated output")
	}

	if _, err := exec.LookPath("node"); err != nil {
		t.Log("node not installed; skipping generated TypeScript comparison")
		return
	}
	outDir := t.TempDir()
	ran, err := ui.Regenerate(context.Background(), ui.RegenerateOptions{
		Registry:   reg,
		OutputDir:  outDir,
		ScriptPath: ui.DefaultScriptPath(),
	})
	if err != nil {
		t.Fatalf("Regenerate: %v", err)
	}
	if !ran {
		t.Fatalf("expected regeneration into an empty scratch dir")
	}
	for _, name := range []string{"types.ts", "documents.ts"} {
		got, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("regenerated %s missing: %v", name, err)
		}
		want, err := os.ReadFile(filepath.Join(committedDir, name))
		if err != nil {
			t.Fatalf("committed %s missing: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("committed orjanda-ui/src/generated/%s is stale: run `npm run codegen` (or `orjanda serve` once) and commit the regenerated output", name)
		}
	}
}

// TestVerifyCommittedSchema covers the node-free half of the generated-output
// consistency check directly: a fresh payload verifies, a stale or missing
// payload fails.
func TestVerifyCommittedSchema(t *testing.T) {
	reg := compileTestRegistry(t)
	path := filepath.Join(t.TempDir(), "schema.json")

	fresh, err := ui.MarshalInput(reg)
	if err != nil {
		t.Fatalf("MarshalInput: %v", err)
	}
	if err := os.WriteFile(path, fresh, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ui.VerifyCommittedSchema(reg, path); err != nil {
		t.Errorf("fresh payload should verify: %v", err)
	}

	if err := os.WriteFile(path, []byte(`[{"name":"Stale"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ui.VerifyCommittedSchema(reg, path); err == nil {
		t.Errorf("stale payload must fail verification")
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := ui.VerifyCommittedSchema(reg, path); err == nil {
		t.Errorf("missing payload must fail verification")
	}
}
