// Package tools implements the ToolRegistry: Compile (run once after
// Registry.Compile) and ForIdentity (run per agent turn) following the
// deterministic O(len(CompiledDocs)) generation algorithm in TAD §10.
//
// See TAD §10 and PRD §24 for the full specification.
// Implemented in Phase 7.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// Tool is a hand-authored custom agent tool (TAD §2.6). Custom tools bypass
// the §10.1–§10.3 Registry-derived generation entirely: they are merged into
// the per-identity tool list by ForIdentity, filtered only by their own
// AllowedRoles, and are exempt from the agent_hidden/GatedFields machinery
// since their Parameters are hand-authored (TAD §10.4). The Handler is
// executed by the Agent Executor in Phase 8; Phase 7 only publishes the
// definition.
type Tool struct {
	Name         string
	Description  string
	Parameters   map[string]any // JSON Schema
	AllowedRoles []string
	Handler      func(ctx context.Context, args map[string]any) (any, error)
}

// ToolTemplate is the identity-independent, Registry-derived tool definition
// produced at compile time. BaseSchema always includes gated fields so the
// template is safely cacheable; ForIdentity projects it per caller. See
// TAD §10.
type ToolTemplate struct {
	DocType string
	// Verb is one of "search"|"list"|"read"|"create"|"update"|"delete"|
	// "execute_action"|"method".
	Verb string
	// Name is snake_case(Verb) + "_" + snake_case(DocType), e.g.
	// "create_employee". The read tool is named "get_{doctype}".
	Name string
	// BaseSchema is the JSON Schema built from ALL fields, including gated
	// ones (TAD §10).
	BaseSchema map[string]any
	// GatedFields lists fields whose oj:"permission=role" tag is non-empty
	// (Go struct field names, as returned by perm.Engine.AllowedFields).
	GatedFields []string
	// Description is the human/agent-readable tool summary.
	Description string

	// allowedRoles gates an RPC method tool (TAD §10.1 step 8) to callers
	// holding at least one of these roles. Empty for CRUD/workflow tools.
	allowedRoles []string
}

// ToolRegistry generates and projects agent tool definitions. See TAD §10.
type ToolRegistry interface {
	// Compile runs once, immediately after Registry.Compile (initialization
	// step 9, TAD §5.3), generating one identity-independent template per
	// CompiledDoc plus one per role-gated RPC method (TAD §10.1).
	Compile(reg schema.Registry) error

	// ForIdentity runs once per Agent Runtime turn (TAD §10.3), projecting
	// each template through the caller's permissions and merging custom tools
	// (TAD §10.4) to produce the tool list actually sent to the LLM.
	ForIdentity(ctx context.Context, id auth.Identity) []llm.ToolDefinition
}

type toolRegistry struct {
	permEngine perm.Engine
	wfEngine   workflow.Engine

	mu        chan struct{} // closed once Compile has run (compiled latch)
	reg       schema.Registry
	templates []ToolTemplate
}

// NewToolRegistry constructs a ToolRegistry. permEngine is required for
// per-identity projection (TAD §10.3); wfEngine may be nil (no workflowed
// DocTypes → no execute_action tools).
func NewToolRegistry(permEngine perm.Engine, wfEngine workflow.Engine) ToolRegistry {
	return &toolRegistry{
		permEngine: permEngine,
		wfEngine:   wfEngine,
		mu:         make(chan struct{}),
	}
}

// --- Custom tool registration (TAD §10.4, PRD §24.3) ------------------------

var (
	customMu       sync.RWMutex
	customToolList []Tool
)

// RegisterCustomTool records a hand-authored tool merged into every
// ToolRegistry's ForIdentity output (TAD §10.4). Custom tools bypass the
// Registry-derived generation: they are filtered only by their own
// AllowedRoles. Mirrors the package-level registration pattern of
// api.RegisterMethod and schema.RegisterValidator.
func RegisterCustomTool(tool Tool) {
	customMu.Lock()
	defer customMu.Unlock()
	customToolList = append(customToolList, tool)
}

// registeredCustomTools returns a snapshot of all registered custom tools.
func registeredCustomTools() []Tool {
	customMu.RLock()
	defer customMu.RUnlock()
	return append([]Tool(nil), customToolList...)
}

// CustomTools returns a snapshot of every registered custom tool, exposing
// their Handlers to the Agent Executor (agent/runtime, Phase 8). It mirrors
// the rpc.Methods accessor pattern; the ToolRegistry interface itself does not
// need them because ForIdentity already merges the definitions.
func CustomTools() []Tool {
	return registeredCustomTools()
}

// ResetCustomTools clears all custom tool registrations (test helper).
func ResetCustomTools() {
	customMu.Lock()
	defer customMu.Unlock()
	customToolList = nil
}

// --- Compile ----------------------------------------------------------------

func (t *toolRegistry) Compile(reg schema.Registry) error {
	select {
	case <-t.mu:
		return orjerrors.Conflict("tool registry already compiled")
	default:
	}

	compiledDocs := reg.List()

	// TAD §10.1: skip Documents that are themselves agent_hidden.
	for _, doc := range compiledDocs {
		if doc.AgentHidden {
			continue
		}
		templates := t.generateDocTemplates(doc)
		t.templates = append(t.templates, templates...)
	}

	// TAD §10.1 step 8: one tool per role-gated RPC method, dots → underscores.
	var methodNames []string
	methodRoles := make(map[string][]string)
	for _, m := range rpc.Methods() {
		if len(m.Opts.AllowedRoles) == 0 {
			continue
		}
		name := strings.ReplaceAll(m.Name, ".", "_")
		methodNames = append(methodNames, name)
		methodRoles[name] = m.Opts.AllowedRoles
	}
	sort.Strings(methodNames)
	for _, name := range methodNames {
		t.templates = append(t.templates, ToolTemplate{
			Name:         name,
			Verb:         "method",
			Description:  "Call the " + name + " custom RPC method.",
			BaseSchema:   map[string]any{"type": "object", "properties": map[string]any{}},
			allowedRoles: methodRoles[name],
		})
	}

	t.reg = reg
	close(t.mu)
	return nil
}

// generateDocTemplates emits the per-CompiledDoc templates of TAD §10.1
// steps 1–6. It never emits a template for a child table (step 7): child
// tables are only reachable nested inside the parent's create/update payload,
// which keeps the tool count at O(len(CompiledDocs)).
func (t *toolRegistry) generateDocTemplates(doc *schema.CompiledDoc) []ToolTemplate {
	var out []ToolTemplate

	if doc.Searchable {
		out = append(out, ToolTemplate{
			DocType:     doc.Name,
			Verb:        "search",
			Name:        "search_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Search %s records.", doc.Name),
			BaseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "Full-text search query"},
					"limit": map[string]any{"type": "integer", "description": "Maximum number of results", "default": 50},
				},
				"required": []any{"query"},
			},
		})
	}

	// list_{doctype} and get_{doctype}: read tools (TAD §10.1 step 2).
	out = append(out,
		ToolTemplate{
			DocType:     doc.Name,
			Verb:        "list",
			Name:        "list_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("List %s records.", doc.Name),
			BaseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"page":  map[string]any{"type": "integer", "description": "Page number", "default": 1},
					"limit": map[string]any{"type": "integer", "description": "Records per page", "default": 50},
				},
			},
		},
		ToolTemplate{
			DocType:     doc.Name,
			Verb:        "read",
			Name:        "get_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Get a %s record by ID.", doc.Name),
			BaseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "ID of the " + doc.Name + " record"},
				},
				"required": []any{"id"},
			},
		},
	)

	// Gated fields are the identity-independent marker list for §10.3.
	gated := gatedFields(doc)

	if anyPermission(doc, "create") {
		out = append(out, ToolTemplate{
			DocType:     doc.Name,
			Verb:        "create",
			Name:        "create_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Create a new %s record. %s", doc.Name, doc.Description),
			BaseSchema:  buildPayloadSchema(doc, false, gated),
			GatedFields: gated,
		})
	}
	if anyPermission(doc, "write") {
		out = append(out, ToolTemplate{
			DocType:     doc.Name,
			Verb:        "update",
			Name:        "update_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Update an existing %s record. %s", doc.Name, doc.Description),
			BaseSchema:  buildPayloadSchema(doc, true, gated),
			GatedFields: gated,
		})
	}
	if anyPermission(doc, "delete") {
		out = append(out, ToolTemplate{
			DocType:     doc.Name,
			Verb:        "delete",
			Name:        "delete_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Delete a %s record.", doc.Name),
			BaseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{"type": "string", "description": "ID of the " + doc.Name + " record"},
				},
				"required": []any{"id"},
			},
		})
	}

	// execute_action_{doctype}: exactly one per workflowed DocType (TAD §8.2
	// and §10.1 step 6). The action enum is empty at compile time and
	// populated per call from AvailableTransitions.
	if t.docHasWorkflow(doc.Name) {
		out = append(out, ToolTemplate{
			DocType:     doc.Name,
			Verb:        "execute_action",
			Name:        "execute_action_" + snakeCase(doc.Name),
			Description: fmt.Sprintf("Execute a workflow action on a %s record.", doc.Name),
			BaseSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":     map[string]any{"type": "string", "description": "ID of the " + doc.Name + " record"},
					"action": map[string]any{"type": "string", "enum": []any{}, "description": "Workflow action to execute"},
				},
				"required": []any{"id", "action"},
			},
		})
	}

	return out
}

// --- Field Schema Mapping (TAD §10.2) ---------------------------------------

// baseFieldNames are the system-managed auto fields (PRD §10.2) that are
// never exposed to the agent. Children get their own auto set (PRD §10.1).
var baseFieldNames = map[string]bool{
	"ID": true, "Name": true, "Owner": true, "CreatedAt": true,
	"UpdatedAt": true, "ModifiedBy": true, "DocStatus": true, "Deleted": true,
}

var childAutoFieldNames = map[string]bool{
	"ID": true, "ParentID": true, "Idx": true,
}

// buildPayloadSchema builds the create/update JSON Schema from a CompiledDoc.
// onUpdate excludes readonly and computed fields (they are not modifiable).
// agent_hidden fields are excluded entirely for every identity (TAD §12.2);
// hidden fields stay as properties but never land in "required"
// (TAD §10.1 step 3: required params are fields where Required && !Hidden).
func buildPayloadSchema(doc *schema.CompiledDoc, onUpdate bool, gated []string) map[string]any {
	props := make(map[string]any)
	var required []any

	for i := range doc.Fields {
		f := &doc.Fields[i]
		if baseFieldNames[f.Name] || f.AgentHidden || f.Computed {
			continue
		}
		if onUpdate && f.ReadOnly {
			continue
		}

		props[snakeCase(f.Name)] = fieldSchema(*f)

		if f.Required && !f.Hidden {
			required = append(required, snakeCase(f.Name))
		}
	}

	// Child tables nest inside the parent payload (TAD §10.1 step 7) — they
	// never receive their own tools.
	for _, child := range doc.ChildTables {
		props[snakeCase(child.FieldName)] = childTableSchema(child)
	}

	return map[string]any{
		"type":       "object",
		"properties": props,
		"required":   required,
	}
}

// childTableSchema maps a child table to an array-of-objects JSON Schema
// (TAD §10.1 step 7 nested payload).
func childTableSchema(child schema.CompiledChild) map[string]any {
	props := make(map[string]any)
	for i := range child.Fields {
		f := &child.Fields[i]
		if childAutoFieldNames[f.Name] || f.AgentHidden || f.Computed {
			continue
		}
		props[snakeCase(f.Name)] = fieldSchema(*f)
	}
	return map[string]any{
		"type":  "array",
		"items": map[string]any{"type": "object", "properties": props},
	}
}

// fieldSchema maps one schema.Field to its JSON Schema per the single shared
// field-type mapping table (TAD §10.2, PRD §10.3). agent_hint is appended
// verbatim to the description.
func fieldSchema(f schema.Field) map[string]any {
	sch := map[string]any{"type": fieldJSONType(f.Type)}

	switch f.Type {
	case schema.FieldTypeDate:
		sch["format"] = "date"
	case schema.FieldTypeDateTime:
		sch["format"] = "date-time"
	case schema.FieldTypeCurrency:
		sch["type"] = "number"
	case schema.FieldTypeJSON:
		sch["type"] = "object"
	}

	if len(f.Options) > 0 {
		sch["enum"] = f.Options
	}
	if f.Default != "" {
		sch["default"] = f.Default
	}
	if f.Format != "" {
		sch["format"] = f.Format
	}

	// Description: link fields describe their target; everything else starts
	// from the human label (PRD §24.2 example).
	desc := f.Label
	if f.Type == schema.FieldTypeLink {
		desc = fmt.Sprintf("Reference to a %s document", f.LinkTarget)
	}
	switch {
	case f.Required && f.Unique:
		desc += " (required, must be unique)"
	case f.Required:
		desc += " (required)"
	case f.Unique:
		desc += ", must be unique"
	}
	if f.AgentHint != "" {
		desc += " " + f.AgentHint
	}
	sch["description"] = desc
	return sch
}

func fieldJSONType(t schema.FieldType) string {
	switch t {
	case schema.FieldTypeInt, schema.FieldTypeInt64:
		return "integer"
	case schema.FieldTypeFloat64, schema.FieldTypeCurrency:
		return "number"
	case schema.FieldTypeBool:
		return "boolean"
	case schema.FieldTypeJSON:
		return "object"
	default:
		return "string"
	}
}

// gatedFields returns the Go field names whose oj:"permission=role" tag is
// non-empty (TAD §10.3: a field is added to GatedFields iff that tag is set).
func gatedFields(doc *schema.CompiledDoc) []string {
	var gated []string
	for i := range doc.Fields {
		if doc.Fields[i].PermissionRole != "" {
			gated = append(gated, doc.Fields[i].Name)
		}
	}
	return gated
}

// anyPermission reports whether any DocPermission grants the action.
func anyPermission(doc *schema.CompiledDoc, action string) bool {
	for _, p := range doc.Permissions {
		switch action {
		case "create":
			if p.Create {
				return true
			}
		case "write":
			if p.Write {
				return true
			}
		case "delete":
			if p.Delete {
				return true
			}
		}
	}
	return false
}

// --- ForIdentity (TAD §10.3) -------------------------------------------------

func (t *toolRegistry) ForIdentity(ctx context.Context, id auth.Identity) []llm.ToolDefinition {
	select {
	case <-t.mu:
	default:
		return nil
	}
	ctx = auth.NewContext(ctx, id)

	out := discoveryTools()

	for _, tmpl := range t.templates {
		if !t.identityMayUse(ctx, id, tmpl) {
			continue
		}
		def := llm.ToolDefinition{
			Name:        tmpl.Name,
			Description: tmpl.Description,
			Parameters:  t.projectSchema(ctx, tmpl, id),
		}
		out = append(out, def)
	}

	for _, c := range registeredCustomTools() {
		if t.permEngine.CheckRoles(ctx, "custom:"+c.Name, "call", c.AllowedRoles) != nil {
			continue
		}
		out = append(out, llm.ToolDefinition{
			Name:        c.Name,
			Description: c.Description,
			Parameters:  c.Parameters,
		})
	}
	return out
}

// identityMayUse decides per-identity tool inclusion. Read/search/list/get
// require the document-level Read check; create/update/delete require their
// own verb check — all through the same perm.Engine path the API layer uses
// (PRD §25.1). RPC method and custom tools are role-gated by AllowedRoles
// through the same perm.Engine path (TAD §9.2 / §10.4).
func (t *toolRegistry) identityMayUse(ctx context.Context, id auth.Identity, tmpl ToolTemplate) bool {
	if len(tmpl.allowedRoles) > 0 {
		return t.permEngine.CheckRoles(ctx, "method:"+tmpl.Name, "call", tmpl.allowedRoles) == nil
	}
	switch tmpl.Verb {
	case "execute_action":
		return true
	case "create", "update", "delete", "search", "list", "read":
		action := tmpl.Verb
		if action == "search" || action == "list" || action == "read" {
			action = "read"
		}
		return t.permEngine.CheckAction(ctx, tmpl.DocType, action) == nil
	}
	return true
}

// projectSchema copies BaseSchema and deletes any GatedFields property the
// caller lacks (TAD §10.3 steps 1–2), keeping the LLM from even seeing gated
// field names it cannot use.
func (t *toolRegistry) projectSchema(ctx context.Context, tmpl ToolTemplate, id auth.Identity) map[string]any {
	if len(tmpl.GatedFields) == 0 {
		return tmpl.BaseSchema
	}
	allowed, err := t.allowedFieldsFor(ctx, tmpl)
	if err != nil {
		return tmpl.BaseSchema
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}

	copied := deepCopySchema(tmpl.BaseSchema)
	props, _ := copied["properties"].(map[string]any)
	required, _ := copied["required"].([]any)

	for _, gated := range tmpl.GatedFields {
		if allowedSet[gated] {
			continue
		}
		key := snakeCase(gated)
		delete(props, key)
		for i, r := range required {
			if r == key {
				required = append(required[:i], required[i+1:]...)
				break
			}
		}
	}
	copied["properties"] = props
	copied["required"] = required
	return copied
}

// allowedFieldsFor delegates to perm.Engine.AllowedFields (TAD §10.3 step 1).
func (t *toolRegistry) allowedFieldsFor(ctx context.Context, tmpl ToolTemplate) ([]string, error) {
	verb := tmpl.Verb
	if verb == "search" || verb == "list" || verb == "read" {
		verb = "read"
	}
	return t.permEngine.AllowedFields(ctx, tmpl.DocType, verb)
}

// deepCopySchema returns an independent copy of a JSON Schema via a JSON
// round-trip (schemas are small; this is compile-time-cheap per call).
func deepCopySchema(in map[string]any) map[string]any {
	raw, err := json.Marshal(in)
	if err != nil {
		return in
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// --- Discovery tools (TAD §11.1) --------------------------------------------

// discoveryTools is the fixed, hand-written discovery set included on every
// ForIdentity result: list_document_types, describe_document,
// list_relationships. They are gated only by the document-level agent_hidden
// flag (enforced by the executor in Phase 8), never by role.
func discoveryTools() []llm.ToolDefinition {
	return []llm.ToolDefinition{
		{
			Name:        "list_document_types",
			Description: "List all Document types registered in the system, optionally filtered by module. Use describe_document on a Document type to activate its operation tools.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"module": map[string]any{"type": "string", "description": "Optional module name to filter by"},
				},
			},
		},
		{
			Name:        "describe_document",
			Description: "Describe a Document type's fields, permissions, and relationships. Describing a Document type activates its operation tools (list, get, search, create, update, delete, execute_action) for this conversation.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"doc_type": map[string]any{"type": "string", "description": "The Document type name"},
				},
				"required": []any{"doc_type"},
			},
		},
		{
			Name:        "list_relationships",
			Description: "List the relationships (links and child tables) of a Document type.",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"doc_type": map[string]any{"type": "string", "description": "The Document type name"},
				},
				"required": []any{"doc_type"},
			},
		},
	}
}

// --- Helpers ----------------------------------------------------------------

// docHasWorkflow reports whether a workflow.Definition targets docType.
func (t *toolRegistry) docHasWorkflow(docType string) bool {
	if t.wfEngine == nil {
		return false
	}
	for _, dt := range t.wfEngine.DocTypes() {
		if dt == docType {
			return true
		}
	}
	return false
}

func snakeCase(s string) string {
	var out strings.Builder
	for i, r := range s {
		if i > 0 && r >= 'A' && r <= 'Z' {
			out.WriteByte('_')
		}
		if r >= 'A' && r <= 'Z' {
			out.WriteRune(r + ('a' - 'A'))
		} else {
			out.WriteRune(r)
		}
	}
	return out.String()
}
