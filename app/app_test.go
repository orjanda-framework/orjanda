package app_test

import (
	"context"
	"testing"

	"github.com/orjanda-framework/orjanda/app"
	"github.com/orjanda-framework/orjanda/errors"
)

// hookApp implements all three TAD §7 lifecycle interfaces and records
// invocations for ordering assertions.
type hookApp struct {
	name string
	log  *[]string
}

func (h *hookApp) OnInstall(ctx context.Context, site any) error {
	*h.log = append(*h.log, "install:"+h.name)
	return nil
}

func (h *hookApp) OnUpgrade(ctx context.Context, site any, fromVersion string) error {
	*h.log = append(*h.log, "upgrade:"+h.name+":"+fromVersion)
	return nil
}

func (h *hookApp) OnUninstall(ctx context.Context, site any, dropTables bool) error {
	*h.log = append(*h.log, "uninstall:"+h.name)
	return nil
}

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

// TestDefinition_LifecycleHookResolution proves the lifecycle hooks ride on the
// Definition's associated init type (TAD §7) and resolve through InstallHook/
// UpgradeHook/UninstallHook — the previously-dead `any(def).(app.Installable)`
// path (REVIEW-2026-08-12 finding 6).
func TestDefinition_LifecycleHookResolution(t *testing.T) {
	var log []string
	def := app.Definition{Name: "A", Hooks: &hookApp{name: "A", log: &log}}

	inst := def.InstallHook()
	if inst == nil {
		t.Fatal("InstallHook must resolve a Hooks type implementing Installable")
	}
	if err := inst.OnInstall(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	up := def.UpgradeHook()
	if up == nil {
		t.Fatal("UpgradeHook must resolve a Hooks type implementing Upgradable")
	}
	if err := up.OnUpgrade(context.Background(), nil, "0.1.0"); err != nil {
		t.Fatal(err)
	}

	un := def.UninstallHook()
	if un == nil {
		t.Fatal("UninstallHook must resolve a Hooks type implementing Uninstallable")
	}
	if err := un.OnUninstall(context.Background(), nil, true); err != nil {
		t.Fatal(err)
	}

	want := []string{"install:A", "upgrade:A:0.1.0", "uninstall:A"}
	if len(log) != len(want) {
		t.Fatalf("hook log = %v, want %v", log, want)
	}
	for i := range want {
		if log[i] != want[i] {
			t.Fatalf("hook log = %v, want %v", log, want)
		}
	}

	plain := app.Definition{Name: "Plain"}
	if plain.InstallHook() != nil || plain.UpgradeHook() != nil || plain.UninstallHook() != nil {
		t.Errorf("a Definition without Hooks must resolve no lifecycle hooks")
	}
}

// TestResolveDAG_HookInstallOrderIsDependencyOrder is the Phase 1 completion
// criterion (TAD §7.1 step 3): when two Applications with a Dependency run
// their OnInstall hooks in ResolveDAG order, the dependency's hook executes
// before its dependent's regardless of declaration order.
func TestResolveDAG_HookInstallOrderIsDependencyOrder(t *testing.T) {
	var log []string
	depA := app.Definition{Name: "A", Hooks: &hookApp{name: "A", log: &log}}
	depB := app.Definition{
		Name:         "B",
		Dependencies: []app.Dependency{{App: "A"}},
		Hooks:        &hookApp{name: "B", log: &log},
	}

	// Declared dependency-last: B before A.
	sorted, err := app.ResolveDAG([]app.Definition{depB, depA})
	if err != nil {
		t.Fatalf("ResolveDAG: %v", err)
	}
	if len(sorted) != 2 || sorted[0].Name != "A" || sorted[1].Name != "B" {
		t.Fatalf("expected [A B], got %+v", sorted)
	}

	for _, d := range sorted {
		if inst := d.InstallHook(); inst != nil {
			if err := inst.OnInstall(context.Background(), nil); err != nil {
				t.Fatal(err)
			}
		}
	}

	want := []string{"install:A", "install:B"}
	if len(log) != len(want) || log[0] != want[0] || log[1] != want[1] {
		t.Fatalf("hook install order = %v, want %v (dependencies must install first, TAD §7.1 step 3)", log, want)
	}
}
