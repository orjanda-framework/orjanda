package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var module, replace string

	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new Orjanda Application",
		Long:  "Scaffolds go.mod + main.go importing orjanda-core, an orjanda.yaml, and the modules/migrations layout (TAD §16, PRD §21.2).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(args[0], module, replace)
		},
	}

	cmd.Flags().StringVar(&module, "module", "", "Go module path (defaults to the app name)")
	cmd.Flags().StringVar(&replace, "replace", "", "local path to the orjanda framework for a go.mod replace directive (defaults to ORJANDA_FRAMEWORK_PATH or a discovered sibling checkout)")

	return cmd
}

func runInit(name, module, replace string) error {
	dir := name
	if _, err := os.Stat(dir); err == nil {
		return errf("directory %q already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errf("creating %q: %w", dir, err)
	}

	modulePath := module
	if modulePath == "" {
		modulePath = name
	}
	if replace == "" {
		replace = discoverFrameworkPath(dir)
	}

	m := &manifest{AppName: name, ModulePath: modulePath}

	files := map[string][]byte{
		"go.mod":       renderGoMod(m, replace),
		"main.go":      renderMainGo(),
		"app.go":       renderAppGo(m),
		"orjanda.yaml": []byte(orjandaYAMLTemplate),
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
			return errf("writing %q: %w", name, err)
		}
	}
	if err := m.save(dir); err != nil {
		return errf("writing %q: %w", manifestPath, err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "migrations"), 0o755); err != nil {
		return errf("creating migrations/: %w", err)
	}

	// Resolve the app's dependency graph so the first `go run .` (delegated
	// commands) and `go build` work immediately.
	if err := tidyAppModule(dir); err != nil {
		return errf("resolving module dependencies: %w", err)
	}

	printInitSummary(dir, m, replace)
	return nil
}

// tidyAppModule runs `go mod tidy` inside dir, resolving the orjanda replace
// directive and writing go.sum.
func tidyAppModule(dir string) error {
	cmd := exec.CommandContext(context.Background(), "go", "mod", "tidy")
	cmd.Dir = dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return errf("go mod tidy in %q failed (is GOFLAGS/-mod=mod set?): %w", dir, err)
	}
	return nil
}

func renderGoMod(m *manifest, replace string) []byte {
	var b strings.Builder
	_ = goModTemplate.Execute(&b, map[string]string{
		"ModulePath": m.ModulePath,
		"Replace":    replace,
	})
	return []byte(b.String())
}

func renderMainGo() []byte {
	var b strings.Builder
	_ = mainGoTemplate.Execute(&b, nil)
	return []byte(b.String())
}

// discoverFrameworkPath finds a local orjanda framework checkout to point the
// app's go.mod replace directive at: ORJANDA_FRAMEWORK_PATH, else an ancestor
// of cwd whose go.mod declares module github.com/orjanda-framework/orjanda.
func discoverFrameworkPath(appDir string) string {
	if p := os.Getenv("ORJANDA_FRAMEWORK_PATH"); p != "" {
		abs, err := filepath.Abs(p)
		if err == nil {
			if rel, err := filepath.Rel(appDir, abs); err == nil {
				return rel
			}
		}
		return p
	}

	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	for cur := wd; ; cur = filepath.Dir(cur) {
		raw, err := os.ReadFile(filepath.Join(cur, "go.mod"))
		if err == nil && strings.Contains(string(raw), "module github.com/orjanda-framework/orjanda") {
			if rel, err := filepath.Rel(appDir, cur); err == nil {
				return rel
			}
		}
		if cur == filepath.Dir(cur) {
			return ""
		}
	}
}

func printInitSummary(dir string, m *manifest, replace string) {
	println("Created", dir+"/")
	println("  → go.mod        (module " + m.ModulePath + ")")
	if replace != "" {
		println("  → replace github.com/orjanda-framework/orjanda => " + replace)
	} else {
		println("  → note: set a go.mod replace directive (or ORJANDA_FRAMEWORK_PATH) so the framework resolves locally")
	}
	println("  → main.go, app.go (Documents are registered in app.go; `new document` keeps it in sync)")
	println("  → orjanda.yaml  (dev defaults: sqlite, :8080)")
	println("  → migrations/")
	println()
	println("Next:")
	println("  cd " + dir)
	println("  orjanda new document Department --module=org")
	println("  orjanda serve")
}
