package schema_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// ----------------------------------------------------------------------------
// Dummy Document Definitions for testing all tags
// ----------------------------------------------------------------------------

type DummyParent struct {
	schema.BaseDocument
	Email      string          `oj:"required,unique,format=email,label=Email Address"`
	NameOption string          `oj:"options=OptionA|OptionB|OptionC,default=OptionA"`
	Age        int             `oj:"label=Age"`
	Amount     schema.Currency `oj:"precision=2,label=Amount"`
	IsActive   bool            `oj:"default=true"`
	Birthday   schema.Date     `oj:"label=Birthday"`
	LastLogin  schema.DateTime `oj:"label=Last Login"`
	LinkedDoc  schema.Link     `oj:"link=DummyTarget,label=Linked Target"`
	IPAddress  string          `oj:"validator=IPValidator,label=IP Address"`
	SecretCode string          `oj:"hidden,agent_hidden,label=Secret"`
	ReadOnly   string          `oj:"readonly,label=Read Only"`
	DerivedVal string          `oj:"computed,label=Computed Value"`
	AgentHint  string          `oj:"agent_hint=This is a hint for agent,label=Agent Hint"`
	Skills     []DummyChild    `oj:"child_table"`
}

func (d *DummyParent) DocMeta() schema.Meta {
	return schema.Meta{
		Name:        "DummyParent",
		Module:      "Core",
		Searchable:  true,
		Submittable: true,
		Icon:        "user",
		Description: "A dummy parent document for testing",
		TitleField:  "Email",
		SortField:   "CreatedAt",
		SortOrder:   schema.Descending,
		Permissions: []schema.DocPermission{
			{Role: "System Manager", Read: true, Write: true, Create: true, Delete: true},
		},
	}
}

type DummyChild struct {
	schema.BaseChild
	SkillName string `oj:"required,label=Skill Name"`
	Level     int    `oj:"label=Skill Level"`
}

type DummyTarget struct {
	schema.BaseDocument
}

func (d *DummyTarget) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "DummyTarget",
	}
}

// ----------------------------------------------------------------------------
// Circular Child Table Definitions
// ----------------------------------------------------------------------------

type CircularParent struct {
	schema.BaseDocument
	Children []CircularChild `oj:"child_table"`
}

func (c *CircularParent) DocMeta() schema.Meta {
	return schema.Meta{Name: "CircularParent"}
}

type CircularChild struct {
	schema.BaseChild
	Loop []CircularParent `oj:"child_table"`
}

// ----------------------------------------------------------------------------
// Missing Link Target Definition
// ----------------------------------------------------------------------------

type MissingLinkParent struct {
	schema.BaseDocument
	Linked schema.Link `oj:"link=NonExistentTarget"`
}

func (m *MissingLinkParent) DocMeta() schema.Meta {
	return schema.Meta{Name: "MissingLinkParent"}
}

// ----------------------------------------------------------------------------
// Tests
// ----------------------------------------------------------------------------

func TestRegistry_Success(t *testing.T) {
	reg := schema.NewRegistry()

	// Register valid docs
	err := reg.Register("test_app", &DummyParent{})
	if err != nil {
		t.Fatalf("unexpected registration error: %v", err)
	}

	err = reg.Register("test_app", &DummyTarget{})
	if err != nil {
		t.Fatalf("unexpected registration error: %v", err)
	}

	// Compile
	err = reg.Compile()
	if err != nil {
		t.Fatalf("unexpected compile error: %v", err)
	}

	// Verify target document compiled state
	doc, err := reg.Get("DummyParent")
	if err != nil {
		t.Fatalf("failed to retrieve compiled doc: %v", err)
	}

	// Check metadata
	if doc.Name != "DummyParent" {
		t.Errorf("expected doc name DummyParent, got %s", doc.Name)
	}
	if doc.TableName != "dummy_parents" {
		t.Errorf("expected table name dummy_parents, got %s", doc.TableName)
	}
	if !doc.Searchable {
		t.Errorf("expected searchable to be true")
	}
	if !doc.Submittable {
		t.Errorf("expected submittable to be true")
	}

	// Verify all annotations parsed correctly on fields
	fieldMap := make(map[string]schema.Field)
	for _, f := range doc.Fields {
		fieldMap[f.Name] = f
	}

	// Check BaseDocument prepended fields
	if _, ok := fieldMap["ID"]; !ok {
		t.Errorf("expected prepended BaseDocument field ID")
	}
	if _, ok := fieldMap["CreatedAt"]; !ok {
		t.Errorf("expected prepended BaseDocument field CreatedAt")
	}

	// Check parsed field annotations
	emailF, exists := fieldMap["Email"]
	if !exists {
		t.Fatalf("missing Email field")
	}
	if !emailF.Required || !emailF.Unique || emailF.Format != "email" || emailF.Label != "Email Address" {
		t.Errorf("invalid Email field parsing: %+v", emailF)
	}

	nameOptF := fieldMap["NameOption"]
	if nameOptF.Default != "OptionA" || len(nameOptF.Options) != 3 || nameOptF.Options[1] != "OptionB" {
		t.Errorf("invalid NameOption field parsing: %+v", nameOptF)
	}

	amountF := fieldMap["Amount"]
	if amountF.Precision != 2 || amountF.Type != schema.FieldTypeCurrency {
		t.Errorf("invalid Amount field parsing: %+v", amountF)
	}

	secretF := fieldMap["SecretCode"]
	if !secretF.Hidden || !secretF.AgentHidden {
		t.Errorf("invalid SecretCode field parsing: %+v", secretF)
	}

	readonlyF := fieldMap["ReadOnly"]
	if !readonlyF.ReadOnly {
		t.Errorf("invalid ReadOnly field parsing: %+v", readonlyF)
	}

	computedF := fieldMap["DerivedVal"]
	if !computedF.Computed {
		t.Errorf("invalid DerivedVal field parsing: %+v", computedF)
	}

	// Check child tables
	if len(doc.ChildTables) != 1 {
		t.Fatalf("expected 1 child table, got %d", len(doc.ChildTables))
	}
	child := doc.ChildTables[0]
	if child.FieldName != "Skills" || child.TypeName != "DummyChild" || child.DocType != "dummy_child" {
		t.Errorf("invalid child table metadata: %+v", child)
	}
	if child.TableName != "dummy_childs" {
		t.Errorf("child TableName = %q, want %q (TAD §1.4 plural snake_case, REVIEW-2026-08-12 finding 11)", child.TableName, "dummy_childs")
	}

	// Check base child fields prepended
	childFieldMap := make(map[string]schema.Field)
	for _, f := range child.Fields {
		childFieldMap[f.Name] = f
	}
	if _, ok := childFieldMap["ParentID"]; !ok {
		t.Errorf("expected parent_id field in child table")
	}
	if _, ok := childFieldMap["Idx"]; !ok {
		t.Errorf("expected idx field in child table")
	}

	// Check relationships
	rels := reg.Relationships("DummyParent")
	if len(rels) != 2 {
		t.Fatalf("expected 2 relationships, got %d", len(rels))
	}
	// Check link relationship
	foundLink := false
	foundChild := false
	for _, r := range rels {
		if r.ToDoc == "DummyTarget" && r.FromField == "LinkedDoc" && !r.IsChildTable {
			foundLink = true
		}
		if r.ToDoc == "dummy_child" && r.FromField == "Skills" && r.IsChildTable {
			foundChild = true
		}
	}
	if !foundLink {
		t.Errorf("missing Link relationship to DummyTarget")
	}
	if !foundChild {
		t.Errorf("missing child table relationship to dummy_child")
	}
}

func TestRegistry_Lock(t *testing.T) {
	reg := schema.NewRegistry()
	_ = reg.Register("test_app", &DummyTarget{})
	_ = reg.Compile()

	// Registering post-compile should fail
	err := reg.Register("test_app", &DummyParent{})
	if err == nil {
		t.Errorf("expected error registering document after compile")
	}
	if !errors.Is(err, errors.ErrConflict) {
		t.Errorf("expected conflict error, got %v", err)
	}

	// Compiling again should fail
	err = reg.Compile()
	if err == nil {
		t.Errorf("expected error compiling registry twice")
	}
	if !errors.Is(err, errors.ErrConflict) {
		t.Errorf("expected conflict error, got %v", err)
	}
}

func TestRegistry_MissingLinkTarget(t *testing.T) {
	reg := schema.NewRegistry()
	_ = reg.Register("test_app", &MissingLinkParent{})

	err := reg.Compile()
	if err == nil {
		t.Fatalf("expected compile to fail due to missing link target")
	}
	if !errors.Is(err, errors.ErrValidation) {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegistry_CircularChildTable(t *testing.T) {
	reg := schema.NewRegistry()
	_ = reg.Register("test_app", &CircularParent{})

	err := reg.Compile()
	if err == nil {
		t.Fatalf("expected compile to fail due to circular child table definition")
	}
	if !errors.Is(err, errors.ErrValidation) {
		t.Errorf("expected validation error, got %v", err)
	}
}

func TestRegistry_DependencyOrder(t *testing.T) {
	// Scenario 1: Correct order (AppA registered before AppB which depends on AppA)
	{
		reg := schema.NewRegistry()
		schema.RegisterDependency(reg, "AppB", "AppA")

		_ = reg.Register("AppA", &DummyTarget{})
		_ = reg.Register("AppB", &DummyParent{})

		err := reg.Compile()
		if err != nil {
			t.Errorf("expected compile to succeed in correct dependency order, got %v", err)
		}
	}

	// Scenario 2: Incorrect order (AppB registered before AppA)
	{
		reg := schema.NewRegistry()
		schema.RegisterDependency(reg, "AppB", "AppA")

		_ = reg.Register("AppB", &DummyParent{})
		_ = reg.Register("AppA", &DummyTarget{})

		err := reg.Compile()
		if err == nil {
			t.Fatalf("expected compile to fail when registered out of dependency order")
		}
		if !errors.Is(err, errors.ErrValidation) {
			t.Errorf("expected validation error, got %v", err)
		}
	}
}

type dynamicDoc struct {
	schema.BaseDocument
	name string
}

func (d *dynamicDoc) DocMeta() schema.Meta {
	return schema.Meta{Name: d.name}
}

func TestRegistry_CompilePerformance(t *testing.T) {
	reg := schema.NewRegistry()
	for i := 0; i < 100; i++ {
		err := reg.Register("perf_app", &dynamicDoc{name: fmt.Sprintf("Doc%d", i)})
		if err != nil {
			t.Fatalf("failed to register doc %d: %v", i, err)
		}
	}

	start := time.Now()
	err := reg.Compile()
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("failed to compile registry: %v", err)
	}

	t.Logf("Compiled 100 docs in %s", elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("compile took %s, which is slower than the 2s target threshold", elapsed)
	}
}
