package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/orjanda-framework/orjanda/internal/version"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	var module, replace, dir, frameworkVersion, replaceLocal string

	cmd := &cobra.Command{
		Use:   "init <name>",
		Short: "Scaffold a new Orjanda Application",
		Long:  "Scaffolds go.mod + main.go importing orjanda-core, an orjanda.yaml, and the modules/migrations layout (TAD §16, PRD §21.2).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInit(args[0], module, replace, dir, frameworkVersion, replaceLocal)
		},
	}

	cmd.Flags().StringVar(&module, "module", "", "Go module path (defaults to the app name)")
	cmd.Flags().StringVar(&replace, "replace", "", "local path to the orjanda framework for a go.mod replace directive (defaults to ORJANDA_FRAMEWORK_PATH or a discovered sibling checkout)")
	cmd.Flags().StringVar(&dir, "dir", "", "destination directory for the Application (defaults to the app name); the app name stays the first argument")
	cmd.Flags().StringVar(&frameworkVersion, "framework-version", "", "explicit framework version to write in go.mod (e.g. v1.2.3); takes precedence over auto-detection")
	cmd.Flags().StringVar(&replaceLocal, "replace-local", "", "local path to the orjanda framework for a go.mod replace directive with v0.0.0 require (alternative to --replace)")

	return cmd
}

// runInit scaffolds an Application. dir separates the app name from where the
// files land (Django startproject-style): the first argument is always the app
// name, --dir chooses the destination. When dir is empty it defaults to the
// app name, mirroring Django's `startproject name` creating ./<name>.
func runInit(appName, module, replace, dir, frameworkVersion, replaceLocal string) error {
	return runInitScaffold(appName, module, replace, dir, frameworkVersion, replaceLocal, tidyAppModule)
}

// runInitScaffold is runInit with an injectable dependency resolver so unit
// tests can exercise the full scaffold without shelling out to `go mod tidy`.
func runInitScaffold(appName, module, replace, dir, frameworkVersion, replaceLocal string, tidy func(dir string) error) error {
	if err := validateAppName(appName); err != nil {
		return err
	}
	if dir == "" {
		dir = appName
	}
	if _, err := os.Stat(dir); err == nil {
		return errf("directory %q already exists", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errf("creating %q: %w", dir, err)
	}

	modulePath := module
	if modulePath == "" {
		modulePath = appName
	}

	// Determine the framework version and replace directive based on flag precedence
	// See TAD §18.4 for the full contract.
	var effectiveReplace string
	var effectiveVersion string

	switch {
	case frameworkVersion != "":
		// --framework-version takes highest precedence
		effectiveVersion = frameworkVersion
		// If --replace-local is also set, use it; otherwise use --replace if set
		if replaceLocal != "" {
			effectiveReplace = replaceLocal
		} else if replace != "" {
			effectiveReplace = replace
		}
	case replaceLocal != "":
		// --replace-local takes medium precedence
		effectiveVersion = "v0.0.0"
		effectiveReplace = replaceLocal
	default:
		// Auto-detect from build info (lowest precedence)
		info := version.Current()
		if info.IsRelease && info.ModulePath == "github.com/orjanda-framework/orjanda" {
			effectiveVersion = info.Version
		} else {
			// Non-release or module path mismatch: warn and use placeholder
			effectiveVersion = "v0.0.0 // TODO: set to a released Orjanda version (see --framework-version / --replace-local)"
			if info.ModulePath != "github.com/orjanda-framework/orjanda" {
				fmt.Fprintf(os.Stderr, "orjanda init: warning: detected module path %q does not match expected github.com/orjanda-framework/orjanda\n", info.ModulePath)
			}
			fmt.Fprintf(os.Stderr, "orjanda init: warning: detected version %q is not a release build\n", info.Version)
			fmt.Fprintf(os.Stderr, "orjanda init: use --framework-version vX.Y.Z to set an explicit version, or --replace-local <path> for local development\n")
		}
		if replace == "" {
			replace = discoverFrameworkPath(dir)
		}
		effectiveReplace = replace
	}

	m := &manifest{AppName: appName, ModulePath: modulePath}
	if err := scaffoldApp(dir, m, effectiveVersion, effectiveReplace); err != nil {
		return err
	}

	// Resolve the app's dependency graph so the first `go run .` (delegated
	// commands) and `go build` work immediately.
	if err := tidy(dir); err != nil {
		return errf("resolving module dependencies: %w", err)
	}

	printInitSummary(dir, m, effectiveReplace, effectiveVersion)
	return nil
}

// scaffoldApp writes the Application's starter files and migrations/ inside
// dir. go.mod, app.go, and the manifest derive from the manifest's AppName and
// ModulePath, never from the destination dir.
func scaffoldApp(dir string, m *manifest, frameworkVersion, replace string) error {
	files := map[string][]byte{
		"go.mod":       renderGoMod(m, frameworkVersion, replace),
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
	return nil
}

// validateAppName rejects values that cannot be a single application name:
// empty, `.`/`..`, or anything containing a path separator. A path-like value
// is ambiguous — Django rejects it too (startproject validates the name as an
// identifier) — so `orjanda init playground/myapp` fails loudly and points the
// user at --dir rather than silently treating the path as the app name.
func validateAppName(name string) error {
	if name == "" {
		return errf("application name required (e.g. `orjanda init myapp`)")
	}
	if name == "." || name == ".." {
		return errf("application name %q is not valid — pass the app name and use --dir to choose the destination directory", name)
	}
	if strings.ContainsAny(name, `/\`) {
		if app, dir := splitNameHints(name); app != "" {
			return errf("application name %q looks like a path — pass the app name alone and use --dir for the destination, e.g. `orjanda init %s --dir %s`", name, app, dir)
		}
		return errf("application name %q looks like a path — pass the app name alone and use --dir for the destination directory", name)
	}
	return nil
}

// splitNameHints suggests an app name and destination directory from a
// path-like value; used only for error guidance.
func splitNameHints(name string) (app, dir string) {
	i := strings.LastIndexAny(name, `/\`)
	if i < 0 || i == len(name)-1 {
		return "", ""
	}
	return name[i+1:], name
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

func renderGoMod(m *manifest, frameworkVersion, replace string) []byte {
	var b strings.Builder
	_ = goModTemplate.Execute(&b, map[string]string{
		"ModulePath":       m.ModulePath,
		"FrameworkVersion": frameworkVersion,
		"Replace":          replace,
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

func printInitSummary(dir string, m *manifest, replace, frameworkVersion string) {
	println("Created", dir+"/")
	println("  → go.mod        (module " + m.ModulePath + ")")
	println("  → framework version: " + frameworkVersion)
	if replace != "" {
		println("  → replace github.com/orjanda-framework/orjanda => " + replace)
	}
	println("  → main.go, app.go (Documents are registered in app.go; `new document` keeps it in sync)")
	println("  → orjanda.yaml  (dev defaults: env: development, sqlite, :8080)")
	println("  → migrations/")
	println()
	println("Next:")
	println("  cd " + dir)
	println("  orjanda new document Department --module=org")
	println("  orjanda serve    # development by default; ORJANDA_ENV=production selects production")
}
