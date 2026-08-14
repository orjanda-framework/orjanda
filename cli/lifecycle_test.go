package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/app"
)

// lifecycleRecord captures hook invocations so tests can assert that
// runInstall/runUninstall actually executed them (REVIEW-2026-08-12 finding 6:
// the Installable assertion was dead code before the fix).
type lifecycleRecord struct {
	installCalls   int
	uninstallCalls int
	lastDropTables bool
}

// testHookApp implements the TAD §7 lifecycle hooks against a shared record.
type testHookApp struct {
	record *lifecycleRecord
}

func (a *testHookApp) OnInstall(ctx context.Context, site any) error {
	a.record.installCalls++
	return nil
}

func (a *testHookApp) OnUninstall(ctx context.Context, site any, dropTables bool) error {
	a.record.uninstallCalls++
	a.record.lastDropTables = dropTables
	return nil
}

// hookedBuilder returns a siteBuilder whose configure installs an Application
// carrying rec as its Hooks init type.
func hookedBuilder(rec *lifecycleRecord) siteBuilder {
	return siteBuilder{configure: func(s *orjanda.Site) error {
		s.Install(app.Definition{Name: "hooked_app", Hooks: &testHookApp{record: rec}})
		return nil
	}}
}

// writeLifecycleConfig writes a valid orjanda.yaml for CLI lifecycle tests.
func writeLifecycleConfig(t *testing.T) string {
	t.Helper()
	cfgFile := filepath.Join(t.TempDir(), "orjanda.yaml")
	dsn := filepath.Join(t.TempDir(), "lifecycle.db")
	cfg := "auth:\n  jwt_secret: cli-lifecycle-test-secret-0123456789-0123456789\ndatabase:\n  driver: sqlite\n  dsn: " + dsn + "\n"
	if err := os.WriteFile(cfgFile, []byte(cfg), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfgFile
}

// TestRunInstall_ExecutesOnInstallHook proves `orjanda install` runs the app's
// OnInstall hook: each invocation calls it once against a freshly compiled
// site.
func TestRunInstall_ExecutesOnInstallHook(t *testing.T) {
	rec := &lifecycleRecord{}
	cfgFile := writeLifecycleConfig(t)

	if err := runInstall(context.Background(), hookedBuilder(rec), cfgFile, "hooked_app"); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if rec.installCalls != 1 {
		t.Errorf("OnInstall called %d times, want 1", rec.installCalls)
	}

	if err := runInstall(context.Background(), hookedBuilder(rec), cfgFile, "hooked_app"); err != nil {
		t.Fatalf("runInstall (second): %v", err)
	}
	if rec.installCalls != 2 {
		t.Errorf("OnInstall called %d times after second install, want 2", rec.installCalls)
	}
}

// TestRunUninstall_ExecutesOnUninstallHook proves `orjanda uninstall` runs the
// app's OnUninstall hook with the --drop-tables flag forwarded, and does not
// run OnInstall.
func TestRunUninstall_ExecutesOnUninstallHook(t *testing.T) {
	rec := &lifecycleRecord{}
	cfgFile := writeLifecycleConfig(t)

	if err := runUninstall(context.Background(), hookedBuilder(rec), cfgFile, "hooked_app", true); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
	if rec.uninstallCalls != 1 {
		t.Errorf("OnUninstall called %d times, want 1", rec.uninstallCalls)
	}
	if !rec.lastDropTables {
		t.Errorf("OnUninstall must receive dropTables=true")
	}
	if rec.installCalls != 0 {
		t.Errorf("OnInstall must not run during uninstall")
	}
}

// TestRunInstall_NoHookIsNoop proves a Definition with no Hooks installs and
// uninstalls cleanly — the optional-interfaces contract of TAD §7.
func TestRunInstall_NoHookIsNoop(t *testing.T) {
	cfgFile := writeLifecycleConfig(t)
	b := siteBuilder{configure: func(s *orjanda.Site) error {
		s.Install(app.Definition{Name: "plain_app"})
		return nil
	}}

	if err := runInstall(context.Background(), b, cfgFile, "plain_app"); err != nil {
		t.Fatalf("runInstall: %v", err)
	}
	if err := runUninstall(context.Background(), b, cfgFile, "plain_app", false); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}
}

// TestRunInstall_UnknownAppFails proves an uninstalled app name errors rather
// than silently succeeding.
func TestRunInstall_UnknownAppFails(t *testing.T) {
	cfgFile := writeLifecycleConfig(t)
	if err := runInstall(context.Background(), siteBuilder{}, cfgFile, "no_such_app"); err == nil {
		t.Errorf("expected error for unknown app")
	}
}
