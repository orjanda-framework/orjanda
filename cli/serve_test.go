package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/config"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// testSite builds a compiled, DB-backed core site the same way the core-only
// CLI does (cli/main.go: siteBuilder{} registers the core Documents, then the
// caller compiles). extra Documents are registered before Compile.
func testSite(t *testing.T, extra ...schema.Document) *orjanda.Site {
	t.Helper()
	cfg := config.Config{
		Server:   config.ServerConfig{},
		Database: config.DatabaseConfig{Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "codegen.db")},
		Auth:     config.AuthConfig{JWTSecret: strings.Repeat("x", 32)},
	}
	site, err := (siteBuilder{}).newSite(cfg)
	if err != nil {
		t.Fatalf("newSite: %v", err)
	}
	for _, d := range extra {
		if err := site.Registry.Register("test_app", d); err != nil {
			t.Fatalf("register extra: %v", err)
		}
	}
	if err := site.Compile(); err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return site
}

// serveExtraDoc is a Document added to a compiled site to change the Registry's
// content hash mid-test.
type serveExtraDoc struct {
	schema.BaseDocument
	Extra string `oj:"required"`
}

func (d *serveExtraDoc) DocMeta() schema.Meta { return schema.Meta{Name: "ServeExtraDoc"} }

// TestServeRegeneratesCodegenOnHashChange proves the serve-time codegen wiring
// (REVIEW-2026-08-12 finding 5): the pass runs on a Registry whose content hash
// is not yet recorded, skips an unchanged Registry, and reruns when a Document
// is added (TAD §6.3 step 3).
func TestServeRegeneratesCodegenOnHashChange(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not installed")
	}
	site := testSite(t)
	outDir := t.TempDir()
	opts := ui.RegenerateOptions{OutputDir: outDir, ScriptPath: ui.DefaultScriptPath()}

	ran, err := serveCodegen(context.Background(), site, opts)
	if err != nil {
		t.Fatalf("serveCodegen: %v", err)
	}
	if !ran {
		t.Fatalf("first pass must regenerate (no marker recorded yet)")
	}
	for _, name := range []string{"schema.json", "types.ts", "documents.ts", ".registry-hash"} {
		if _, err := os.Stat(filepath.Join(outDir, name)); err != nil {
			t.Errorf("expected generated %s: %v", name, err)
		}
	}

	ran, err = serveCodegen(context.Background(), site, opts)
	if err != nil {
		t.Fatalf("serveCodegen (unchanged): %v", err)
	}
	if ran {
		t.Errorf("unchanged registry must skip regeneration")
	}

	// A second site whose Registry includes an extra Document must re-trigger
	// regeneration (different content hash). Registering into a compiled
	// Registry is locked, so the extra Document is added before Compile.
	site2 := testSite(t, &serveExtraDoc{})
	ran, err = serveCodegen(context.Background(), site2, opts)
	if err != nil {
		t.Fatalf("serveCodegen (changed): %v", err)
	}
	if !ran {
		t.Errorf("registry change must trigger regeneration")
	}
}

// TestBenchFailFastOnStaleCodegen proves bench's codegen gate (TAD §16): a
// committed payload matching the compiled Registry passes without node; a stale
// or missing payload fails fast.
func TestBenchFailFastOnStaleCodegen(t *testing.T) {
	site := testSite(t)
	path := filepath.Join(t.TempDir(), "schema.json")

	if err := benchCodegen(site, ui.RegenerateOptions{InputPath: path}); err == nil {
		t.Fatalf("missing payload must fail bench")
	}

	fresh, err := ui.MarshalInput(site.Registry)
	if err != nil {
		t.Fatalf("MarshalInput: %v", err)
	}
	if err := os.WriteFile(path, fresh, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := benchCodegen(site, ui.RegenerateOptions{InputPath: path}); err != nil {
		t.Errorf("fresh payload must pass bench: %v", err)
	}

	if err := os.WriteFile(path, []byte(`[{"name":"Stale"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := benchCodegen(site, ui.RegenerateOptions{InputPath: path}); err == nil {
		t.Errorf("stale payload must fail bench")
	}
}
