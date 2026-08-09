package runtime

import (
	"context"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/auth"
)

// discoveryToolNames is the fixed discovery set of TAD §11.1, always present
// on every turn so the agent can discover any DocType it later wants to operate
// on. Operation tools are attached only for DocTypes that have appeared in the
// session transcript (see toolsForTurn).
var discoveryToolNames = map[string]bool{
	"list_document_types": true,
	"describe_document":   true,
	"list_relationships":  true,
}

// toolsForTurn is the Context Manager: the per-turn LLM tool list assembled
// from the identity-projected registry output (TAD §10.3) then narrowed by
// TAD §11.1 — discovery tools always, custom method/operation tools only once
// their target DocType has been seen in this session — and finally filtered by
// the Safety Layer's ToolAllowlist (TAD §10.3 step 3).
func (r *Runtime) toolsForTurn(ctx context.Context, id auth.Identity, sess *Session) []llm.ToolDefinition {
	all := r.regTools.ForIdentity(ctx, id)
	out := make([]llm.ToolDefinition, 0, len(all))
	for _, def := range all {
		if !r.safety.IsToolAllowed(def.Name) {
			continue
		}
		_, snake := splitOperationTool(def.Name)
		if discoveryToolNames[def.Name] || snake == "" || sess.seenDocType(snake) {
			out = append(out, def)
		}
	}
	return out
}

// schemasFor indexes the active tool set by name for the planner's whole-plan
// validation (TAD §11.3). It must always receive the same slice that was sent
// to the model in the same turn.
func (r *Runtime) schemasFor(tools []llm.ToolDefinition) map[string]map[string]any {
	schemas := make(map[string]map[string]any, len(tools))
	for _, t := range tools {
		schemas[t.Name] = t.Parameters
	}
	return schemas
}
