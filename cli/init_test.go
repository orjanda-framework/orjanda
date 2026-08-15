package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda/config"
)

// noopTidy replaces `go mod tidy` in init unit tests so they never shell out.
func noopTidy(string) error { return nil }

func TestValidateAppName(t *testing.T) {
	valid := []string{"myapp", "my-hr-system", "my_app", "MyApp", "myapp2"}
	for _, name := range valid {
		if err := validateAppName(name); err != nil {
			t.Errorf("validateAppName(%q) unexpected error: %v", name, err)
		}
	}

	cases := []struct {
		name string
		want string
	}{
		{"", "application name required"},
		{".", "use --dir"},
		{"..", "use --dir"},
		{"playground/myapp", "looks like a path"},
		{"/abs/path/myapp", "looks like a path"},
		{`playground\myapp`, "looks like a path"},
		{"trailing/", "looks like a path"},
		{"a/b/c", "orjanda init c --dir a/b/c"},
	}
	for _, tc := range cases {
		err := validateAppName(tc.name)
		if err == nil {
			t.Errorf("validateAppName(%q) expected an error", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("validateAppName(%q) error = %q, want substring %q", tc.name, err, tc.want)
		}
	}
}

func TestRunInitDefaultDir(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	assertInitApp(t, "myapp", "myapp", "myapp", "")
}

func TestRunInitDirFlag(t *testing.T) {
	t.Chdir(t.TempDir())

	// App name stays "myapp"; files land under playground/myapp. The dir path
	// must never leak into go.mod/app.go/the manifest.
	if err := runInitScaffold("myapp", "", "", "playground/myapp", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	assertInitApp(t, "playground/myapp", "myapp", "myapp", "")
	assertNoLeak(t, "playground/myapp", "playground")
}

func TestRunInitModuleFlagPreserved(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "example.com/acme/myapp", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	assertInitApp(t, "myapp", "myapp", "example.com/acme/myapp", "")
}

func TestRunInitReplaceFlagPreserved(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "vendor/orjanda", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	assertInitApp(t, "myapp", "myapp", "myapp", "vendor/orjanda")
}

func TestRunInitNoReplaceLine(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	if gomod := readFile(t, "myapp/go.mod"); strings.Contains(gomod, "replace github.com/orjanda-framework/orjanda") {
		t.Errorf("go.mod should have no replace directive:\n%s", gomod)
	}
}

func TestRunInitExistingDirRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("myapp", 0o755); err != nil {
		t.Fatal(err)
	}

	err := runInitScaffold("myapp", "", "", "", noopTidy)
	if err == nil || !strings.Contains(err.Error(), `"myapp" already exists`) {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestRunInitDirFlagExistingDirRejected(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("playground/myapp", 0o755); err != nil {
		t.Fatal(err)
	}

	err := runInitScaffold("myapp", "", "", "playground/myapp", noopTidy)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("expected 'already exists' error, got: %v", err)
	}
}

func TestRunInitPathLikeNameRejected(t *testing.T) {
	t.Chdir(t.TempDir())

	err := runInitScaffold("playground/myapp", "", "", "", noopTidy)
	if err == nil {
		t.Fatal("expected an error for a path-like app name")
	}
	if !strings.Contains(err.Error(), "looks like a path") || !strings.Contains(err.Error(), "--dir") {
		t.Errorf("error should explain path handling and --dir, got: %v", err)
	}
	// Nothing must have been created on disk.
	if _, statErr := os.Stat("playground"); !os.IsNotExist(statErr) {
		t.Errorf("no directory should have been created for a rejected name")
	}
}

func TestRunInitModulePathDefaultsToAppName(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("my-hr-system", "", "", "playground", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	// Default module path comes from the app name, not the destination dir.
	assertInitApp(t, "playground", "my-hr-system", "my-hr-system", "")
}

// TestRunInitDirFlagThenNewDocument proves the whole flow stays consistent:
// after init with --dir, `new document` writes under the app dir and regenerates
// app.go using the module path (never the destination path).
func TestRunInitDirFlagThenNewDocument(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "playground/myapp", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	t.Chdir("playground/myapp")

	if err := runNewDocument("Department", "org", false); err != nil {
		t.Fatalf("runNewDocument: %v", err)
	}
	if !fileExists(t, "modules/org/documents/department.go") {
		t.Errorf("expected modules/org/documents/department.go under the app dir")
	}

	appgo := readFile(t, "app.go")
	if !strings.Contains(appgo, `myapp/modules/org/documents`) {
		t.Errorf("app.go should import the module-path-based package, got:\n%s", appgo)
	}
	if !strings.Contains(appgo, `registerDocs(reg, "myapp", userDocs)`) {
		t.Errorf("app.go should register under the app name, got:\n%s", appgo)
	}

	m := testManifest(t, ".")
	if m.AppName != "myapp" || m.ModulePath != "myapp" {
		t.Errorf("manifest drifted: app_name=%q module_path=%q", m.AppName, m.ModulePath)
	}
}

func TestInitCmdHasDirFlag(t *testing.T) {
	cmd := newInitCmd()
	if cmd.Use != "init <name>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "init <name>")
	}
	flag := cmd.Flag("dir")
	if flag == nil {
		t.Fatal("init command must expose a --dir flag")
	}
	if !strings.Contains(flag.Usage, "destination directory") {
		t.Errorf("--dir usage should explain it is the destination: %q", flag.Usage)
	}
}

// assertInitApp verifies the scaffolded app in dir: manifest app_name /
// module_path, go.mod module + replace, and presence of the starter files.
func assertInitApp(t *testing.T, dir, wantAppName, wantModulePath, wantReplace string) {
	t.Helper()

	gomod := readFile(t, filepath.Join(dir, "go.mod"))
	if !strings.Contains(gomod, "module "+wantModulePath) {
		t.Errorf("go.mod missing module %q:\n%s", wantModulePath, gomod)
	}
	if wantReplace != "" {
		if !strings.Contains(gomod, "replace github.com/orjanda-framework/orjanda => "+wantReplace) {
			t.Errorf("go.mod missing replace directive for %q:\n%s", wantReplace, gomod)
		}
	}

	m := testManifest(t, dir)
	if m.AppName != wantAppName {
		t.Errorf("manifest app_name = %q, want %q", m.AppName, wantAppName)
	}
	if m.ModulePath != wantModulePath {
		t.Errorf("manifest module_path = %q, want %q", m.ModulePath, wantModulePath)
	}

	for _, f := range []string{"go.mod", "main.go", "app.go", "orjanda.yaml", manifestPath} {
		if !fileExists(t, filepath.Join(dir, f)) {
			t.Errorf("missing %s in %s", f, dir)
		}
	}
	if fi, err := os.Stat(filepath.Join(dir, "migrations")); err != nil || !fi.IsDir() {
		t.Errorf("missing migrations/ directory in %s: %v", dir, err)
	}
}

// assertNoLeak verifies a destination path does not leak into the app's files.
func assertNoLeak(t *testing.T, dir, forbidden string) {
	t.Helper()
	for _, f := range []string{"go.mod", "app.go", manifestPath} {
		if content := readFile(t, filepath.Join(dir, f)); strings.Contains(content, forbidden) {
			t.Errorf("%s must not reference destination path %q:\n%s", f, forbidden, content)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	return string(b)
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

func testManifest(t *testing.T, dir string) *manifest {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, manifestPath))
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("parsing manifest: %v", err)
	}
	return &m
}

// TestScaffoldOrjandaYamlDocumentsDevSecret proves the scaffolded orjanda.yaml
// tells the operator about the dev-secret behavior (golden-path: init →
// serve must work without a configured secret, while a real secret remains the
// production requirement).
func TestScaffoldOrjandaYamlDocumentsDevSecret(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}

	cfgText := readFile(t, "myapp/orjanda.yaml")
	if !strings.Contains(cfgText, "jwt_secret") {
		t.Errorf("scaffolded orjanda.yaml must mention auth.jwt_secret:\n%s", cfgText)
	}
	if !strings.Contains(cfgText, "orjanda serve") {
		t.Errorf("scaffolded orjanda.yaml should reference the serve dev-secret behavior:\n%s", cfgText)
	}
	if !strings.Contains(cfgText, "ORJANDA_ENV") {
		t.Errorf("scaffolded orjanda.yaml should document ORJANDA_ENV:\n%s", cfgText)
	}
}

// TestScaffoldedAppConfigEnvServes proves the full golden path at the unit
// level: a freshly scaffolded app (no jwt_secret) passes config.Load in the
// development environment (the default `orjanda serve`), and fails in
// production (ORJANDA_ENV=production).
func TestScaffoldedAppConfigEnvServes(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}

	t.Setenv("ORJANDA_ENV", config.EnvDevelopment)
	cfg, generated, err := config.Load("myapp/orjanda.yaml")
	if err != nil {
		t.Fatalf("Load (development) on scaffolded app: %v", err)
	}
	if generated == "" {
		t.Fatal("scaffolded app has no jwt_secret; development Load must generate one")
	}
	if err := config.ValidateJWTSecret(cfg.Auth.JWTSecret); err != nil {
		t.Errorf("generated secret invalid: %v", err)
	}

	t.Setenv("ORJANDA_ENV", config.EnvProduction)
	if _, _, err := config.Load("myapp/orjanda.yaml"); err == nil {
		t.Error("config.Load (production) on scaffolded app without a secret must fail (production strictness)")
	}
}

// TestScaffoldedExampleDocumentMatchesREADME keeps the README's Getting Started
// example in sync with the `new document` scaffold: the example struct, Get,
// and Set must reference only fields the README declares (no unresolved
// Link targets like the old Employee/LeaveType example).
func TestScaffoldedExampleDocumentMatchesREADME(t *testing.T) {
	t.Chdir(t.TempDir())

	if err := runInitScaffold("myapp", "", "", "", noopTidy); err != nil {
		t.Fatalf("runInitScaffold: %v", err)
	}
	t.Chdir("myapp")

	if err := runNewDocument("LeaveRequest", "", false); err != nil {
		t.Fatalf("runNewDocument: %v", err)
	}

	doc := readFile(t, "documents/leave_request.go")
	// The scaffold must not introduce unresolved Link references.
	if strings.Contains(doc, "schema.Link") || strings.Contains(doc, "link=") {
		t.Errorf("scaffold must not reference Links the README golden path doesn't define:\n%s", doc)
	}
	// The scaffold registers the document and regenerates app.go.
	appgo := readFile(t, "app.go")
	if !strings.Contains(appgo, "appdocs.LeaveRequest") {
		t.Errorf("app.go should register the generated Document:\n%s", appgo)
	}
}
