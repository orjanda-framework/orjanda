package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"unicode"
)

// manifestPath is the file recording the Application's generated Documents so
// `new document` can regenerate app.go deterministically. It is an
// implementation detail of the CLI, not a framework schema.
const manifestPath = ".orjanda.json"

// manifestDoc is one generated Document entry in the manifest.
type manifestDoc struct {
	Name        string `json:"name"`
	Module      string `json:"module"`
	Submittable bool   `json:"submittable"`
}

// manifest describes a scaffolded Application (TAD §16 `init` output).
type manifest struct {
	AppName    string        `json:"app_name"`
	ModulePath string        `json:"module_path"`
	Documents  []manifestDoc `json:"documents"`
}

func loadManifest(dir string) (*manifest, error) {
	raw, err := os.ReadFile(filepath.Join(dir, manifestPath))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *manifest) save(dir string) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestPath), append(raw, '\n'), 0o644)
}

// importGroup groups Documents by their Go package so app.go imports each
// package exactly once.
type importGroup struct {
	Alias string
	Path  string
	Refs  []string
}

// groups returns the import groups for all user Documents.
func (m *manifest) groups() []importGroup {
	byPkg := make(map[string]*importGroup)
	var order []string
	for _, d := range m.Documents {
		alias, pkgPath := m.packageFor(d)
		key := alias + "|" + pkgPath
		g, ok := byPkg[key]
		if !ok {
			g = &importGroup{Alias: alias, Path: pkgPath}
			byPkg[key] = g
			order = append(order, key)
		}
		g.Refs = append(g.Refs, alias+"."+d.Name)
	}

	out := make([]importGroup, 0, len(order))
	for _, key := range order {
		out = append(out, *byPkg[key])
	}
	return out
}

// packageFor returns the import alias and Go import path for a Document.
// Module Documents live in modules/{module}/documents (package name
// "documents", aliased {module}docs); module-less Documents live in the
// top-level documents/ package (aliased appdocs) per PRD §21.2/§21.3.
func (m *manifest) packageFor(d manifestDoc) (alias, pkgPath string) {
	if d.Module == "" {
		return "appdocs", m.ModulePath + "/documents"
	}
	return moduleAlias(d.Module), m.ModulePath + "/modules/" + d.Module + "/documents"
}

func moduleAlias(module string) string {
	return toSnake(module) + "docs"
}

// goModTemplate renders the Application go.mod with an optional replace
// directive pointing at the local framework source (dev-only convenience).
var goModTemplate = template.Must(template.New("go.mod").Parse(
	`module {{.ModulePath}}

go 1.26.5

require github.com/orjanda-framework/orjanda {{.FrameworkVersion}}
{{- if .Replace}}

replace github.com/orjanda-framework/orjanda => {{.Replace}}
{{- end}}
`,
))

// mainGoTemplate is the Application entry point; `configure` lives in app.go.
var mainGoTemplate = template.Must(template.New("main.go").Parse(
	`package main

import "github.com/orjanda-framework/orjanda/cli"

func main() {
	cli.Main(configure)
}
`,
))

// appGoTemplate regenerates the Application's registration root (TAD §7.1):
// every `new document` re-emits it from the manifest.
var appGoTemplate = template.Must(template.New("app.go").Parse(
	`package main

import (
	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/schema"
	core "github.com/orjanda-framework/orjanda/orjanda-core"
{{- range .Groups}}
	{{.Alias}} "{{.Path}}"
{{- end}}
)

func configure(site *orjanda.Site) error {
	site.Install(core.App)
	reg := site.Registry
	coreDocs := []schema.Document{&core.User{}, &core.Role{}, &core.RolePermission{}}
	if err := registerDocs(reg, "core", coreDocs); err != nil {
		return err
	}
{{- if .UserDocs}}
	userDocs := []schema.Document{
		{{- range .Groups}}
		{{- range .Refs}}
		&{{.}}{},
		{{- end}}
		{{- end}}
	}
	if err := registerDocs(reg, "{{.AppName}}", userDocs); err != nil {
		return err
	}
{{- end}}
	return nil
}

func registerDocs(reg schema.Registry, appName string, docs []schema.Document) error {
	for _, d := range docs {
		if err := reg.Register(appName, d); err != nil {
			return err
		}
	}
	return nil
}
`,
))

type appGoData struct {
	AppName  string
	Groups   []importGroup
	UserDocs bool
}

// renderAppGo emits app.go for the manifest.
func renderAppGo(m *manifest) []byte {
	var sb strings.Builder
	_ = appGoTemplate.Execute(&sb, appGoData{
		AppName:  m.AppName,
		Groups:   m.groups(),
		UserDocs: len(m.Documents) > 0,
	})
	return []byte(sb.String())
}

// orjandaYAMLTemplate is the default dev configuration scaffold.
const orjandaYAMLTemplate = `# Deployment environment: development (default) or production.
# Also settable via ORJANDA_ENV=production. Production is fail-fast: it
# requires a real auth.jwt_secret, pre-applied migrations, and committed
# frontend codegen (TAD §16).
env: development

server:
  host: 127.0.0.1
  port: 8080

database:
  driver: sqlite
  dsn: orjanda.db

auth:
  # JWT signing key. In development 'orjanda serve' generates an ephemeral
  # random key when this is absent, so local sessions will not survive a
  # restart. Set a real key (at least 32 characters) to keep sessions across
  # restarts; production (ORJANDA_ENV=production) requires it.
  # Set via env: ORJANDA_AUTH_JWT_SECRET
  # jwt_secret: change-me-to-a-long-random-string
`

type docScaffoldData struct {
	Name        string
	ModuleTitle string
	Submittable bool
}

// docScaffoldTemplate renders documents/{snake}.go (PRD §21.3).
var docScaffoldTemplate = template.Must(template.New("document").Parse(
	`package documents

import (
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// {{.Name}} is a {{.Name}} business entity.
type {{.Name}} struct {
	schema.BaseDocument
	// TODO: declare business fields, e.g.
	// Code string ` + "`oj:\"required,unique\"`" + `
	// Amount schema.Currency ` + "`oj:\"required\"`" + `
}

func (d *{{.Name}}) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "{{.Name}}",
		Module:      "{{.ModuleTitle}}",
		Submittable: {{.Submittable}},
		Description: "{{.Name}} business entity",
	}
}

func (d *{{.Name}}) Get(field string) any {
	return d.BaseDocument.Get(field)
}

func (d *{{.Name}}) Set(field string, value any) orjerrors.Error {
	return d.BaseDocument.Set(field, value)
}
`,
))

// toSnake converts a PascalCase or CamelCase identifier to snake_case, matching
// the naming rules of TAD §1.4 (same algorithm as schema.camelToSnake).
func toSnake(s string) string {
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			if unicode.IsLower(prev) || unicode.IsDigit(prev) {
				b.WriteByte('_')
			} else if i+1 < len(runes) && unicode.IsLower(runes[i+1]) {
				b.WriteByte('_')
			}
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// pascalCase converts a name to an exported PascalCase identifier.
func pascalCase(s string) string {
	var b strings.Builder
	upper := true
	for _, r := range s {
		if r == '_' || r == '-' || r == ' ' {
			upper = true
			continue
		}
		if upper {
			b.WriteRune(unicode.ToUpper(r))
			upper = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}

func renderDocScaffold(data docScaffoldData) []byte {
	var b strings.Builder
	_ = docScaffoldTemplate.Execute(&b, data)
	return []byte(b.String())
}
