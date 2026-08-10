// Package agent is the embedded AI Agent Runtime package (PRD §23). It owns
// the agent.Tool extension point (TAD §2.6) and exposes custom tool
// registration; the full execute loop, context/planning, safety, and approval
// machinery are implemented in Phase 8 (agent/runtime, agent/planner,
// agent/safety).
package agent

import (
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
	"github.com/orjanda-framework/orjanda/agent/tools"
)

// Tool is a hand-authored custom agent tool. It is re-exported from
// agent/tools (where ToolRegistry lives) so callers reference it as agent.Tool
// exactly as TAD §2.6 declares. Custom tools bypass Registry-derived
// generation and are merged into ToolRegistry.ForIdentity filtered only by
// their own AllowedRoles (TAD §10.4). The Handler is executed by the Agent
// Executor in Phase 8.
type Tool = tools.Tool

// RegisterTool registers a custom agent tool (PRD §24.3). Registrations are
// merged into every ToolRegistry's ForIdentity output per TAD §10.4. Mirrors
// the package-level registration patterns of api.RegisterMethod and
// schema.RegisterValidator; call from package init or main.
func RegisterTool(tool Tool) {
	tools.RegisterCustomTool(tool)
}

// ExecuteOption, WithProvider, and WithApprovals are re-exported from
// agent/runtime (TAD §3.3). They let a caller override the LLM provider and
// approval gateway for a single Execute turn — which is the hook
// orjanda/testing.MockLLM uses to script agent turns from a test (TAD §17).
type ExecuteOption = runtime.ExecuteOption

func WithProvider(p llm.Provider) runtime.ExecuteOption {
	return runtime.WithProvider(p)
}

func WithApprovals(a runtime.ApprovalGateway) runtime.ExecuteOption {
	return runtime.WithApprovals(a)
}
