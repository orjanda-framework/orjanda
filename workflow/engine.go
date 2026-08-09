package workflow

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/event"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
)

// Definition declares the states, transitions, and handlers for a DocType workflow.
// See TAD §8.
type Definition struct {
	DocType      string
	States       []State
	Transitions  []Transition
	OnTransition map[string]Handler // keyed by destination State.Name
}

// State defines a workflow state.
type State struct {
	Name  string
	Style string // UI hint only: "gray" | "yellow" | "green" | "red" | a hex color
}

// Transition defines a valid state change, allowed roles, and optional guard.
type Transition struct {
	From         string
	To           string
	Action       string // verb surfaced as a UI button label and an agent enum value
	AllowedRoles []string
	Guard        GuardFunc // optional; evaluated after the AllowedRoles check
}

// GuardFunc returns an error if the transition condition is not met.
type GuardFunc func(ctx context.Context, doc map[string]any) error

// Handler is a callback invoked after a transition completes.
type Handler func(ctx context.Context, doc map[string]any) error

// Engine manages workflow registration, transition discovery, and execution.
// See TAD §8.
type Engine interface {
	Register(def Definition) error
	AvailableTransitions(ctx context.Context, docType, currentState string) []Transition
	Execute(ctx context.Context, docType, id, action string) error
	// DocTypes lists every DocType with a registered workflow definition.
	// Consumed by the agent ToolRegistry at compile time to emit exactly one
	// execute_action_{doctype} tool per workflowed DocType (TAD §10.1 step 6).
	DocTypes() []string
}

type engine struct {
	mu          sync.RWMutex
	db          dal.Database
	reg         schema.Registry
	permEngine  perm.Engine
	eventBus    event.Bus
	auditLog    audit.Log
	definitions map[string]Definition
}

// NewEngine constructs a workflow Engine.
func NewEngine(db dal.Database, reg schema.Registry, permEngine perm.Engine, eventBus event.Bus, auditLog audit.Log) Engine {
	return &engine{
		db:          db,
		reg:         reg,
		permEngine:  permEngine,
		eventBus:    eventBus,
		auditLog:    auditLog,
		definitions: make(map[string]Definition),
	}
}

func (e *engine) Register(def Definition) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	compiled, err := e.reg.Get(def.DocType)
	if err != nil {
		return err
	}

	// Auto-add WorkflowState field to CompiledDoc.Fields if not present (TAD §8.1 step 1).
	hasWFState := false
	for _, f := range compiled.Fields {
		if strings.EqualFold(f.DBColumn, "workflow_state") || strings.EqualFold(f.Name, "WorkflowState") {
			hasWFState = true
			break
		}
	}
	if !hasWFState {
		compiled.Fields = append(compiled.Fields, schema.Field{
			Name:     "WorkflowState",
			DBColumn: "workflow_state",
			Type:     schema.FieldTypeString,
			Label:    "Workflow State",
		})
	}

	e.definitions[def.DocType] = def
	return nil
}

// DocTypes returns the sorted list of DocTypes with registered workflows,
// used by the agent ToolRegistry at compile time (TAD §10.1 step 6).
func (e *engine) DocTypes() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, 0, len(e.definitions))
	for docType := range e.definitions {
		out = append(out, docType)
	}
	sort.Strings(out)
	return out
}

func (e *engine) AvailableTransitions(ctx context.Context, docType, currentState string) []Transition {
	e.mu.RLock()
	def, ok := e.definitions[docType]
	e.mu.RUnlock()

	if !ok {
		return nil
	}

	id := auth.FromContext(ctx)
	var available []Transition

	for _, t := range def.Transitions {
		if !strings.EqualFold(t.From, currentState) && t.From != "" && t.From != "*" {
			continue
		}

		if len(t.AllowedRoles) > 0 {
			if !hasAnyRole(id, t.AllowedRoles) {
				continue
			}
		}

		available = append(available, t)
	}

	return available
}

func (e *engine) Execute(ctx context.Context, docType, id, action string) error {
	e.mu.RLock()
	def, ok := e.definitions[docType]
	e.mu.RUnlock()

	if !ok {
		return orjerrors.NotFound(fmt.Sprintf("no workflow registered for DocType %q", docType))
	}

	compiled, err := e.reg.Get(docType)
	if err != nil {
		return err
	}

	// 1. Fetch current document record to resolve currentState.
	rows, err := e.db.Query(ctx, dal.Select{
		DocType:   docType,
		TableName: compiled.TableName,
		Filters:   map[string]any{"id": id},
	})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return orjerrors.NotFound(fmt.Sprintf("record %q not found in %q", id, docType))
	}

	doc := rows[0]
	currentState, _ := doc["workflow_state"].(string)

	// 2. Resolve matching Transition.
	var targetTrans *Transition
	for i := range def.Transitions {
		t := &def.Transitions[i]
		if (strings.EqualFold(t.From, currentState) || t.From == "" || t.From == "*") && strings.EqualFold(t.Action, action) {
			targetTrans = t
			break
		}
	}

	if targetTrans == nil {
		return orjerrors.Conflict(fmt.Sprintf("no such transition from current state %q for action %q", currentState, action))
	}

	// 3. Role check via shared perm path (TAD §8.1 step 3 & TAD §13.3 perm.denied logging).
	userID := auth.FromContext(ctx).UserID
	if len(targetTrans.AllowedRoles) > 0 {
		idCtx := auth.FromContext(ctx)
		if !hasAnyRole(idCtx, targetTrans.AllowedRoles) {
			slog.Warn("perm.denied", "user", userID, "doctype", docType, "action", action, "via_agent", false)
			return orjerrors.Permission(fmt.Sprintf("permission denied: user lacks required role for transition %q on %q", action, docType))
		}
	}

	// 4-6. Execute transition inside a single dal.Tx (TAD §8.1 steps 4-6).
	return e.db.Transaction(ctx, func(tx dal.Tx) error {
		// Guard check
		if targetTrans.Guard != nil {
			if err := targetTrans.Guard(ctx, doc); err != nil {
				return err
			}
		}

		// Update state in DB
		now := time.Now()
		updateData := map[string]any{
			"workflow_state": targetTrans.To,
			"updated_at":     now,
		}

		if err := tx.Update(ctx, docType, id, updateData); err != nil {
			return err
		}

		// Build updated document payload for event & handler
		docCopy := make(map[string]any, len(doc)+2)
		for k, v := range doc {
			docCopy[k] = v
		}
		docCopy["workflow_state"] = targetTrans.To
		docCopy["updated_at"] = now

		// Emit on_workflow_transition event
		if e.eventBus != nil {
			if err := e.eventBus.Emit(ctx, docType, "on_workflow_transition", docCopy); err != nil {
				return err
			}
		}

		// Invoke OnTransition[To] handler if registered
		if handler, exists := def.OnTransition[targetTrans.To]; exists && handler != nil {
			if err := handler(ctx, docCopy); err != nil {
				return err
			}
		}

		// Audit Log entry inside same dal.Tx (TAD §8.1 step 6 & §13.1 write-path guarantee)
		if e.auditLog != nil {
			auditEntry := audit.BuildEntry(ctx, "workflow_transition", docType, id, []audit.FieldChange{
				{
					Field:    "workflow_state",
					OldValue: currentState,
					NewValue: targetTrans.To,
				},
			})
			if err := e.auditLog.Write(ctx, auditEntry); err != nil {
				return err
			}
		}

		return nil
	})
}

func hasAnyRole(id auth.Identity, allowed []string) bool {
	for _, reqRole := range allowed {
		if reqRole == "*" {
			return true
		}
		for _, userRole := range id.Roles {
			if strings.EqualFold(userRole, reqRole) {
				return true
			}
		}
	}
	return false
}
