package perm

import (
	"context"
	"log/slog"
	"strings"

	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

// Check is passed to Rule.Evaluate and carries the full context of a
// permission decision. See TAD §9.1.
type Check struct {
	DocType string
	Action  string
	DocID   string // empty when not operating on a specific record
	Data    map[string]any
}

// Rule is a custom ABAC predicate evaluated after the RBAC check passes.
// Multiple Rules compose with AND semantics. See TAD §9.1 and §2.7.
type Rule interface {
	Evaluate(ctx context.Context, check Check) error
}

// ErrPermissionDenied matches any CodePermission error, including the ones the
// Engine returns for RBAC denials. It is a re-export of errors.ErrPermission
// (TAD §1.1: the six ErrorCodes own every error condition; perm introduces no
// new error type) so permission checks read as perm.ErrPermissionDenied — e.g.
// the PRD §32.3 TestAgentCanSearchEmployees acceptance test.
var ErrPermissionDenied = orjerrors.ErrPermission

// Engine evaluates access control for document-level and field-level operations.
// See TAD §2.4 and §2.7.
type Engine interface {
	// CheckAction evaluates document-level CRUD permission for the caller
	// in ctx. Returns errors.CodePermission if denied.
	CheckAction(ctx context.Context, docType, action string) error

	// FilterRead returns a copy of data containing only the fields the caller
	// is allowed to read, based on field-level oj:"permission=role" tags.
	FilterRead(ctx context.Context, docType string, data map[string]any) (map[string]any, error)

	// FilterWrite strips or rejects fields the caller is not permitted to write.
	// Returns errors.CodePermission if a gated field is present in data and the
	// caller lacks the required role (not silent dropping — active rejection).
	FilterWrite(ctx context.Context, docType string, data map[string]any) (map[string]any, error)

	// AllowedFields projects field-level permission without a data payload.
	// Used by ToolRegistry.ForIdentity (Phase 7) to build per-identity schemas.
	AllowedFields(ctx context.Context, docType, action string) ([]string, error)

	// RegisterRule wires a custom ABAC Rule into evaluation, run after RBAC.
	RegisterRule(r Rule)

	// SetDatabase attaches a database instance for dynamic RolePermission queries.
	SetDatabase(db dal.Database)
}

// ---------------------------------------------------------------------------
// engine — concrete implementation
// ---------------------------------------------------------------------------

type engine struct {
	reg   schema.Registry
	db    dal.Database
	rules []Rule
}

// NewEngine creates a permission engine backed by the compiled Registry.
// The Registry must already be compiled before NewEngine is called.
func NewEngine(reg schema.Registry) Engine {
	return &engine{reg: reg}
}

func (e *engine) SetDatabase(db dal.Database) {
	e.db = db
}

func (e *engine) RegisterRule(r Rule) {
	e.rules = append(e.rules, r)
}

// ---------------------------------------------------------------------------
// CheckAction
// ---------------------------------------------------------------------------

// CheckAction performs RBAC check: caller must hold a role that grants the
// requested action on docType. If RBAC passes, all registered Rules are
// evaluated (AND composition). See TAD §2.4.
func (e *engine) CheckAction(ctx context.Context, docType, action string) error {
	id := auth.FromContext(ctx)
	compiled, err := e.reg.Get(docType)
	if err != nil {
		slog.Warn("perm.denied", "user", id.UserID, "doctype", docType, "action", action, "via_agent", false)
		return orjerrors.Permission("unknown document type: " + docType)
	}

	allowed := hasRole(id, "System Administrator")
	if !allowed && len(compiled.Permissions) > 0 {
		allowed = rbacCheck(id, compiled, action)
	}
	if !allowed {
		dbAllowed, dbErr := e.checkDBPermissions(ctx, id, docType, action)
		if dbErr == nil && dbAllowed {
			allowed = true
		}
	}

	if !allowed && (len(compiled.Permissions) > 0 || e.db != nil) {
		slog.Warn("perm.denied", "user", id.UserID, "doctype", docType, "action", action, "via_agent", false)
		return orjerrors.Permission(
			"permission denied: action " + action + " on " + docType)
	}

	// AND-compose custom rules.
	check := Check{DocType: docType, Action: action}
	for _, r := range e.rules {
		if err := r.Evaluate(ctx, check); err != nil {
			slog.Warn("perm.denied", "user", id.UserID, "doctype", docType, "action", action, "via_agent", false)
			return err
		}
	}
	return nil
}

func (e *engine) checkDBPermissions(ctx context.Context, id auth.Identity, docType, action string) (bool, error) {
	if e.db == nil || len(id.Roles) == 0 {
		return false, nil
	}

	rows, err := e.db.Query(ctx, dal.Select{
		DocType: "RolePermission",
		Filters: map[string]any{
			"doc_type": docType,
		},
	})
	if err != nil {
		return false, err
	}

	actionField := strings.ToLower(action)
	if actionField == "update" {
		actionField = "write"
	}

	for _, row := range rows {
		roleVal, _ := row["role"].(string)
		if hasRole(id, roleVal) {
			if flag, ok := row[actionField].(bool); ok && flag {
				return true, nil
			}
		}
	}

	return false, nil
}

// rbacCheck returns true if identity holds a role that grants action on compiled.
// Union-of-roles semantics: ANY matching permission entry grants access.
func rbacCheck(id auth.Identity, compiled *schema.CompiledDoc, action string) bool {
	for _, perm := range compiled.Permissions {
		if !hasRole(id, perm.Role) {
			continue
		}
		switch strings.ToLower(action) {
		case "read":
			if perm.Read {
				return true
			}
		case "write", "update":
			if perm.Write {
				return true
			}
		case "create":
			if perm.Create {
				return true
			}
		case "delete":
			if perm.Delete {
				return true
			}
		case "submit":
			if perm.Submit {
				return true
			}
		}
	}
	return false
}

// hasRole reports whether identity holds roleName (case-insensitive).
func hasRole(id auth.Identity, roleName string) bool {
	for _, r := range id.Roles {
		if strings.EqualFold(r, roleName) {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// FilterRead / FilterWrite
// ---------------------------------------------------------------------------

// FilterRead returns a copy of data with gated fields removed if the caller
// lacks the required role. See TAD §2.4 and §2.7.
func (e *engine) FilterRead(ctx context.Context, docType string, data map[string]any) (map[string]any, error) {
	id := auth.FromContext(ctx)
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, orjerrors.NotFound("unknown document type: " + docType)
	}

	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}

	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		if f.PermissionRole == "" {
			continue
		}
		if !hasRole(id, f.PermissionRole) {
			// Remove by both Go name and DB column name.
			delete(out, f.Name)
			delete(out, f.DBColumn)
		}
	}
	return out, nil
}

// FilterWrite rejects (not silently drops) any gated field the caller lacks
// the role for. Returns errors.CodePermission with the field name if violated.
// See TAD §2.7.
func (e *engine) FilterWrite(ctx context.Context, docType string, data map[string]any) (map[string]any, error) {
	id := auth.FromContext(ctx)
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, orjerrors.NotFound("unknown document type: " + docType)
	}

	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		if f.PermissionRole == "" {
			continue
		}
		if hasRole(id, f.PermissionRole) {
			continue
		}
		// Caller lacks the required role — reject if present.
		if _, byName := data[f.Name]; byName {
			return nil, orjerrors.Permission(
				"field " + f.Name + " requires role " + f.PermissionRole)
		}
		if _, byCol := data[f.DBColumn]; byCol {
			return nil, orjerrors.Permission(
				"field " + f.Name + " requires role " + f.PermissionRole)
		}
	}

	// Return a clean copy.
	out := make(map[string]any, len(data))
	for k, v := range data {
		out[k] = v
	}
	return out, nil
}

// ---------------------------------------------------------------------------
// AllowedFields
// ---------------------------------------------------------------------------

// AllowedFields returns the names (Go struct field names) of fields the caller
// may access for the given action on docType. Used by ToolRegistry (Phase 7).
func (e *engine) AllowedFields(ctx context.Context, docType, _ string) ([]string, error) {
	id := auth.FromContext(ctx)
	compiled, err := e.reg.Get(docType)
	if err != nil {
		return nil, orjerrors.NotFound("unknown document type: " + docType)
	}

	var allowed []string
	for i := range compiled.Fields {
		f := &compiled.Fields[i]
		if f.PermissionRole == "" || hasRole(id, f.PermissionRole) {
			allowed = append(allowed, f.Name)
		}
	}
	return allowed, nil
}
