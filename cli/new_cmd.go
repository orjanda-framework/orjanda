package cli

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newNewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "new",
		Short: "Generate a Document or Module scaffold",
		Long:  "Generates idiomatic Go code scaffolds; the output is meant to be edited by the developer (PRD §21.3).",
	}
	cmd.AddCommand(newNewDocumentCmd(), newNewModuleCmd())
	return cmd
}

func newNewDocumentCmd() *cobra.Command {
	var module string
	var submittable bool

	cmd := &cobra.Command{
		Use:   "document <name>",
		Short: "Generate a Document scaffold",
		Long:  "Writes documents/{snake}.go from a text/template scaffold and registers it in app.go (TAD §16, PRD §21.3).",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewDocument(args[0], module, submittable)
		},
	}

	cmd.Flags().StringVar(&module, "module", "", "module (grouping) to place the Document in (default: top-level documents/)")
	cmd.Flags().BoolVar(&submittable, "submittable", false, "give the Document a submission lifecycle (DocStatus, PRD §10.2)")

	return cmd
}

func newNewModuleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "module <name>",
		Short: "Generate a Module scaffold",
		Long:  "Creates modules/{name}/{documents,hooks,workflows,api,ui}/ per PRD §11.1.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runNewModule(args[0])
		},
	}
	return cmd
}

// appDir returns the Application root when cwd is inside one (go.mod +
// .orjanda.json present), else an error telling the user to run init first.
func appDir() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	if !isOrjandaAppDir(wd) {
		return "", errf("no Application here (missing go.mod/main.go) — run `orjanda init <name>` first")
	}
	if _, err := os.Stat(filepath.Join(wd, manifestPath)); err != nil {
		return "", errf("no %q here — run `orjanda init <name>` first", manifestPath)
	}
	return wd, nil
}

func runNewDocument(name, module string, submittable bool) error {
	dir, err := appDir()
	if err != nil {
		return err
	}

	docName := pascalCase(name)
	if docName == "" || docName[0] < 'A' || docName[0] > 'Z' {
		return errf("invalid Document name %q — use a valid Go identifier", name)
	}

	m, err := loadManifest(dir)
	if err != nil {
		return errf("loading manifest: %w", err)
	}
	for _, d := range m.Documents {
		if d.Name == docName {
			return errf("Document %q already exists", docName)
		}
	}

	moduleDir := ""
	if module != "" {
		moduleDir = toSnake(module)
	}
	docDir := filepath.Join(dir, "documents")
	if moduleDir != "" {
		docDir = filepath.Join(dir, "modules", moduleDir, "documents")
	}
	if err := os.MkdirAll(docDir, 0o755); err != nil {
		return errf("creating %q: %w", docDir, err)
	}

	fileName := filepath.Join(docDir, toSnake(docName)+".go")
	if _, err := os.Stat(fileName); err == nil {
		return errf("%q already exists", fileName)
	}

	content := renderDocScaffold(docScaffoldData{
		Name:        docName,
		ModuleTitle: pascalCase(moduleDir),
		Submittable: submittable,
	})
	if err := os.WriteFile(fileName, content, 0o644); err != nil {
		return errf("writing %q: %w", fileName, err)
	}

	m.Documents = append(m.Documents, manifestDoc{
		Name:        docName,
		Module:      moduleDir,
		Submittable: submittable,
	})
	if err := m.save(dir); err != nil {
		return errf("updating manifest: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), renderAppGo(m), 0o644); err != nil {
		return errf("regenerating app.go: %w", err)
	}

	println("Created: " + relDir(dir, fileName))
	println("  → Document: " + docName)
	if moduleDir != "" {
		println("  → Module: " + moduleDir)
	} else {
		println("  → Module: (none — top-level documents/)")
	}
	println("  → Submittable: " + boolStr(submittable))
	println("  → Generated DocMeta() with default permissions")
	println("  → Generated BaseDocument embedding")
	println("  → Registered in app.go")
	return nil
}

func runNewModule(name string) error {
	dir, err := appDir()
	if err != nil {
		return err
	}

	moduleDir := toSnake(name)
	if moduleDir == "" {
		return errf("invalid module name %q", name)
	}
	base := filepath.Join(dir, "modules", moduleDir)
	subdirs := []string{"documents", "hooks", "workflows", "api", "ui"}
	for _, sub := range subdirs {
		d := filepath.Join(base, sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			return errf("creating %q: %w", d, err)
		}
		keep := filepath.Join(d, ".gitkeep")
		if _, err := os.Stat(keep); os.IsNotExist(err) {
			if err := os.WriteFile(keep, []byte(""), 0o644); err != nil {
				return errf("writing %q: %w", keep, err)
			}
		}
	}
	println("Created: modules/" + moduleDir + "/{documents,hooks,workflows,api,ui}/")
	return nil
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// relDir renders a path relative to the given directory for display.
func relDir(base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return path
	}
	return rel
}
