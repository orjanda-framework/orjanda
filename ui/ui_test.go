package ui_test

import (
	"testing"

	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// DummyParent mirrors the Phase 1 reference Document used by the codegen
// acceptance test: scalar fields, an options list, a Link, and a child table.
type DummyParent struct {
	schema.BaseDocument
	Name   string          `oj:"required,label=Name"`
	Status string          `oj:"options=Draft|Active"`
	Target schema.Link     `oj:"link=DummyTarget"`
	Amount schema.Currency `oj:"precision=2"`
	Skills []DummyChild    `oj:"child_table"`
}

func (d *DummyParent) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "DummyParent",
		Module:      "Core",
		Searchable:  true,
		TitleField:  "Name",
		Submittable: true,
		Permissions: []schema.DocPermission{
			{Role: "System Administrator", Read: true, Write: true, Create: true, Delete: true},
			{Role: "Viewer", Read: true, Write: false, Create: false, Delete: false},
		},
	}
}

type DummyChild struct {
	schema.BaseChild
	Skill string `oj:"required"`
}

func (d *DummyChild) DocMeta() schema.Meta { return schema.Meta{Name: "DummyChild"} }

type DummyTarget struct {
	schema.BaseDocument
	Label string `oj:"required"`
}

func (d *DummyTarget) DocMeta() schema.Meta { return schema.Meta{Name: "DummyTarget"} }

type DummyExtra struct {
	schema.BaseDocument
	Extra string `oj:"required"`
}

func (d *DummyExtra) DocMeta() schema.Meta { return schema.Meta{Name: "DummyExtra"} }

func compileTestRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	if err := reg.Register("test_app", &DummyParent{}); err != nil {
		t.Fatalf("register DummyParent: %v", err)
	}
	if err := reg.Register("test_app", &DummyTarget{}); err != nil {
		t.Fatalf("register DummyTarget: %v", err)
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return reg
}

func TestRegistry_RegisterPageAndPages(t *testing.T) {
	reg := ui.NewRegistry()
	if got := len(reg.Pages()); got != 0 {
		t.Fatalf("expected empty pages, got %d", got)
	}

	p := ui.Page{Path: "/app/hr/org-chart", Title: "Org Chart", Component: "hr/OrgChart", Icon: "sitemap", Menu: "HR"}
	reg.RegisterPage(p)
	reg.RegisterPage(ui.Page{Path: "/app/hr/org-chart", Title: "Org Chart v2"}) // same path → replace

	pages := reg.Pages()
	if len(pages) != 1 {
		t.Fatalf("expected 1 page, got %d", len(pages))
	}
	if pages[0].Title != "Org Chart v2" {
		t.Errorf("expected replaced page title, got %q", pages[0].Title)
	}
}

func TestCodegenInput_Shape(t *testing.T) {
	reg := compileTestRegistry(t)
	input, err := ui.CodegenInput(reg)
	if err != nil {
		t.Fatalf("CodegenInput: %v", err)
	}

	var parent *ui.DocMetaJSON
	for i := range input {
		if input[i].Name == "DummyParent" {
			parent = &input[i]
		}
	}
	if parent == nil {
		t.Fatalf("DummyParent missing from codegen input")
	}

	if parent.TitleField != "Name" || !parent.Searchable || !parent.Submittable {
		t.Errorf("meta flags wrong: %+v", parent)
	}
	if parent.Permissions.CanRead != true || parent.Permissions.CanDelete != true {
		t.Errorf("any-grant permissions wrong: %+v", parent.Permissions)
	}

	byCol := map[string]ui.FieldJSON{}
	for _, f := range parent.Fields {
		byCol[f.Column] = f
	}
	if f := byCol["amount"]; f.Type != "currency" {
		t.Errorf("currency field mapping wrong: %+v", f)
	}
	if f := byCol["target"]; f.Link != "DummyTarget" {
		t.Errorf("link field mapping wrong: %+v", f)
	}
	if f := byCol["status"]; len(f.Options) != 2 {
		t.Errorf("options field mapping wrong: %+v", f)
	}

	if len(parent.ChildTables) != 1 {
		t.Fatalf("expected 1 child table, got %d", len(parent.ChildTables))
	}
	c := parent.ChildTables[0]
	if c.FieldName != "Skills" || c.TypeName != "DummyChild" || c.DocType != "dummy_child" {
		t.Errorf("child table mapping wrong: %+v", c)
	}
}

func TestCodegenInput_HashStabilityAndSensitivity(t *testing.T) {
	reg := compileTestRegistry(t)
	h1, err := ui.ContentHash(reg)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}
	if len(h1) != 64 {
		t.Errorf("expected sha256 hex hash, got %q", h1)
	}
	// Deterministic across calls.
	h2, err := ui.ContentHash(reg)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}
	if h1 != h2 {
		t.Errorf("hash not deterministic: %q vs %q", h1, h2)
	}

	// A different registry (an extra Document) must change the hash.
	reg2 := schema.NewRegistry()
	if err := reg2.Register("test_app", &DummyParent{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg2.Register("test_app", &DummyTarget{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := reg2.Register("test_app", &DummyExtra{}); err != nil {
		t.Fatalf("register extra: %v", err)
	}
	if err := reg2.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}
	h3, err := ui.ContentHash(reg2)
	if err != nil {
		t.Fatalf("ContentHash: %v", err)
	}
	if h3 == h1 {
		t.Errorf("hash must change when a Document is added")
	}
}
