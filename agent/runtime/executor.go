package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/audit"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// executeToolCall runs one model-requested tool call and returns the
// observation fed back to the LLM.
func (r *Runtime) executeToolCall(ctx context.Context, sess *Session, call llm.ToolCall, prompt string, approvals ApprovalGateway) string {
	args := map[string]any{}
	if call.Arguments != "" {
		if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
			args = map[string]any{}
		}
	}
	return r.executeTool(ctx, sess, call.Name, args, prompt, false, approvals)
}

// executeTool is the Agent Executor entry point (TAD §3.3). Every write is
// routed through the same Document/Workflow Engine entry points the API layer
// uses (PRD §23.1), never a separate agent path, with audit.WithAgent set
// unconditionally so agent-initiated writes are flagged via_agent=true
// (TAD §12.2, §13.3). planApproved is true only for steps of a plan that
// already passed plan-level human approval (TAD §11.2 step b).
func (r *Runtime) executeTool(ctx context.Context, sess *Session, name string, args map[string]any, prompt string, planApproved bool, approvals ApprovalGateway) string {
	id := auth.FromContext(ctx)
	r.emit(Event{Type: EventToolStart, Tool: name})

	if !r.safety.IsToolAllowed(name) {
		obs := "error: tool " + name + " is blocked by the safety policy tool allowlist"
		r.emit(Event{Type: EventToolEnd, Tool: name})
		return obs
	}

	// Human-in-the-loop gate (TAD §12.1): the reason is one of
	// always_require_approval | bulk_limit | role_override | require_approval
	// (TAD §12.3 policy_reason). Discovery tools (TAD §11.1) are read-only
	// and are classified as "read" so they never trip the fail-closed default.
	if !planApproved {
		verb := safety.VerbFor(name, args)
		if discoveryToolNames[name] {
			verb = "read"
		}
		info := safety.ToolCallInfo{
			Verb:        verb,
			ToolName:    name,
			Args:        args,
			TargetCount: sess.TargetCount(r.snakeDocTypeFor(name)),
		}
		a := r.safety.RequiresApprovalWithReason(ctx, id, info)
		if a.Required {
			obs, newArgs, ok := r.gateApproval(ctx, sess, name, args, a, approvals)
			if !ok {
				r.emit(Event{Type: EventToolEnd, Tool: name})
				return obs
			}
			args = newArgs
		}
	}

	// Agent-initiated writes carry the agent audit context (TAD §13.3:
	// via_agent, agent_session, agent_prompt).
	actx := audit.WithAgent(auth.NewContext(ctx, id), sess.ID, prompt)
	actx = safety.WithSession(actx, sess.ID)

	out, err := r.dispatch(actx, name, args)

	r.markDocTypesSeen(sess, name, args)
	// A list/search count is recorded only for a DocType operation tool
	// (TAD §12.1 step 2). Discovery tools like list_document_types return a
	// record set too, but their count must never seed the session's bulk
	// target — it does not describe records the agent will operate on.
	if count, ok := recordCount(out); ok {
		if dt := r.snakeDocTypeFor(name); dt != "" {
			sess.setTargetCount(dt, count)
		}
	}

	obs := observation(out, err)
	r.emit(Event{Type: EventToolEnd, Tool: name, Success: err == nil})
	return obs
}

// gateApproval runs the approval_required round trip (TAD §12.3). On approval
// it returns (args, true) with the human's Modify payload substituted when the
// human chose to amend the arguments (PRD §38.2). On denial it returns an
// observation describing the denial.
func (r *Runtime) gateApproval(ctx context.Context, sess *Session, name string, args map[string]any, a safety.Approval, approvals ApprovalGateway) (string, map[string]any, bool) {
	if approvals == nil {
		return "error: approval required for " + name + " but no approval gateway is configured", nil, false
	}
	verb := safety.VerbFor(name, args)
	if discoveryToolNames[name] {
		verb = "read"
	}
	payload := ApprovalPayload{
		ActionID: newApprovalID(),
		Details: ApprovalDetails{
			DocType:      r.docTypeFor(name),
			Action:       verb,
			Payload:      args,
			PolicyReason: string(a.Reason),
		},
	}
	r.emit(Event{Type: EventApprovalRequired, Approval: &payload})
	resp, err := approvals.RequestApproval(ctx, payload)
	if err != nil {
		return "error: approval flow failed: " + err.Error(), nil, false
	}
	if !resp.Approved {
		return "the user denied this operation; do not perform it", nil, false
	}
	if resp.Payload != nil {
		return "", resp.Payload, true
	}
	return "", args, true
}

// newApprovalID returns a unique action id for an approval round trip
// (TAD §12.3 action_id, e.g. "req-<ulid>").
func newApprovalID() string {
	return "req-" + ulid.Make().String()
}

// dispatch routes one tool call to its handler. Discovery tools are answered
// from the Registry; operation tools call the same document.Engine /
// workflow.Engine functions the API layer calls; method and custom tools
// delegate to their registered handlers.
func (r *Runtime) dispatch(ctx context.Context, name string, args map[string]any) (any, error) {
	switch name {
	case "list_document_types":
		return r.listDocumentTypes(args)
	case "describe_document":
		return r.describeDocument(args)
	case "list_relationships":
		return r.listRelationships(args)
	}

	if verb, _ := splitOperationTool(name); verb != "" {
		canonical := r.docTypeFor(name)
		if canonical == "" {
			return nil, orjerrors.NotFound("unknown agent tool: " + name)
		}
		return r.dispatchOperation(ctx, verb, canonical, args)
	}

	if methodName, ok := r.methodTool[name]; ok {
		m, found := rpc.GetMethod(methodName)
		if !found || m.Handler == nil {
			return nil, orjerrors.NotFound("custom RPC method not found: " + methodName)
		}
		return m.Handler(ctx, args)
	}

	if ct, ok := r.customTool[name]; ok && ct.Handler != nil {
		return ct.Handler(ctx, args)
	}

	return nil, orjerrors.NotFound("unknown agent tool: " + name)
}

// dispatchOperation executes a Registry-derived CRUD/workflow tool through the
// Document/Workflow Engine (the single code path of PRD §23.1).
func (r *Runtime) dispatchOperation(ctx context.Context, verb, docType string, args map[string]any) (any, error) {
	switch verb {
	case "create":
		id, err := r.docEngine.Create(ctx, docType, args)
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	case "update":
		id := stringArg(args, "id")
		delete(args, "id")
		if err := r.docEngine.Update(ctx, docType, id, args); err != nil {
			return nil, err
		}
		return map[string]any{"id": id}, nil
	case "delete":
		id := stringArg(args, "id")
		if err := r.docEngine.Delete(ctx, docType, id); err != nil {
			return nil, err
		}
		return map[string]any{"id": id, "deleted": true}, nil
	case "read", "get":
		id := stringArg(args, "id")
		return r.docEngine.Read(ctx, docType, id)
	case "list":
		return r.docEngine.List(ctx, docType, listOpts(args))
	case "search":
		return r.docEngine.List(ctx, docType, document.ListOpts{
			Filters: map[string]any{"q": stringArg(args, "query")},
			Limit:   intArg(args, "limit"),
		})
	case "execute_action":
		if r.wfEngine == nil {
			return nil, orjerrors.NotFound("no workflow engine configured")
		}
		if err := r.wfEngine.Execute(ctx, docType, stringArg(args, "id"), stringArg(args, "action")); err != nil {
			return nil, err
		}
		return map[string]any{"id": stringArg(args, "id"), "action": stringArg(args, "action"), "success": true}, nil
	}
	return nil, orjerrors.NotFound("unknown agent operation: " + verb)
}

// listOpts converts list/search tool arguments to document.ListOpts
// (page is 1-based; offset = (page-1)*limit).
func listOpts(args map[string]any) document.ListOpts {
	opts := document.ListOpts{Limit: intArg(args, "limit")}
	if page := intArg(args, "page"); page > 1 {
		if opts.Limit <= 0 {
			opts.Limit = 50
		}
		opts.Offset = (page - 1) * opts.Limit
	}
	return opts
}

// --- Discovery tool handlers (TAD §11.1) -------------------------------------

func (r *Runtime) listDocumentTypes(args map[string]any) (any, error) {
	module, _ := args["module"].(string)
	out := []map[string]any{}
	for _, d := range r.schemaReg.List() {
		if d.AgentHidden {
			continue
		}
		if module != "" && !strings.EqualFold(d.Module, module) {
			continue
		}
		out = append(out, map[string]any{
			"name":        d.Name,
			"module":      d.Module,
			"description": d.Description,
		})
	}
	return out, nil
}

// describeBaseFields are the system-managed auto fields (PRD §10.2) excluded
// from the describe_document field list — the agent operates on business
// fields only.
var describeBaseFields = map[string]bool{
	"ID": true, "Name": true, "Owner": true, "CreatedAt": true,
	"UpdatedAt": true, "ModifiedBy": true, "DocStatus": true, "Deleted": true,
}

func (r *Runtime) describeDocument(args map[string]any) (any, error) {
	docType, _ := args["doc_type"].(string)
	doc, err := r.schemaReg.Get(docType)
	if err != nil {
		return nil, err
	}
	if doc.AgentHidden {
		return nil, orjerrors.NotFound("document type " + docType + " is not exposed to agents")
	}
	fields := make([]map[string]any, 0, len(doc.Fields))
	for _, f := range doc.Fields {
		if f.AgentHidden || describeBaseFields[f.Name] {
			continue
		}
		fm := map[string]any{
			"name":     snakeCase(f.Name),
			"type":     string(f.Type),
			"required": f.Required,
		}
		if len(f.Options) > 0 {
			fm["options"] = f.Options
		}
		if f.LinkTarget != "" {
			fm["link"] = f.LinkTarget
		}
		if f.Label != "" {
			fm["label"] = f.Label
		}
		if f.AgentHint != "" {
			fm["hint"] = f.AgentHint
		}
		fields = append(fields, fm)
	}
	return map[string]any{
		"name":        doc.Name,
		"description": doc.Description,
		"module":      doc.Module,
		"searchable":  doc.Searchable,
		"fields":      fields,
	}, nil
}

func (r *Runtime) listRelationships(args map[string]any) (any, error) {
	docType, _ := args["doc_type"].(string)
	if _, err := r.schemaReg.Get(docType); err != nil {
		return nil, err
	}
	return r.schemaReg.Relationships(docType), nil
}

// --- Observation and result helpers ------------------------------------------

// markDocTypesSeen records DocTypes the agent referenced so their operation
// tools attach to subsequent turns (TAD §11.1).
func (r *Runtime) markDocTypesSeen(sess *Session, name string, args map[string]any) {
	if name == "describe_document" {
		if dt, ok := args["doc_type"].(string); ok && dt != "" {
			sess.markSeen(snakeCase(dt))
		}
		return
	}
	if _, snake := splitOperationTool(name); snake != "" {
		sess.markSeen(snake)
	}
}

// recordCount reports the record count of a list/search result for the bulk
// approval check (TAD §12.1 step 2).
func recordCount(out any) (int, bool) {
	switch v := out.(type) {
	case []map[string]any:
		return len(v), true
	case []any:
		return len(v), true
	}
	return 0, false
}

// observation converts a dispatch result into the LLM-visible string. Errors
// are rendered from the safe Message() of an errors.Error when available.
func observation(out any, err error) string {
	if err != nil {
		var oe orjerrors.Error
		if orjerrors.As(err, &oe) {
			return "error: " + oe.Message()
		}
		return "error: " + err.Error()
	}
	if out == nil {
		return "ok"
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf("result: %v", out)
	}
	s := string(raw)
	if len(s) > 2000 {
		s = s[:2000] + "... (truncated)"
	}
	return s
}

func stringArg(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]any, key string) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case float32:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}
