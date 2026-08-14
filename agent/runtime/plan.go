package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/planner"
	"github.com/orjanda-framework/orjanda/agent/safety"
	"github.com/orjanda-framework/orjanda/auth"
)

// executePlanMode runs the Plan-and-Execute path of TAD §11.2 steps b–c. It is
// only entered after hasDataDependency detected inter-call data flow in the
// model's first response (step 2). The plan is validated in full BEFORE any
// step executes (step b + TAD §11.3), so a rejected plan has zero side
// effects.
func (r *Runtime) executePlanMode(ctx context.Context, sess *Session, userMessage string, tools []llm.ToolDefinition, schemas map[string]map[string]any, provider llm.Provider, approvals ApprovalGateway) (*Response, error) {
	plan, err := r.requestPlan(ctx, sess, tools, provider)
	if err != nil {
		return r.planFailure(ctx, sess, "The model could not produce a parseable plan. "+err.Error())
	}
	if vErr := planner.Validate(plan, schemas); vErr != nil {
		// One correction turn (TAD §11.3): the whole plan is rejected before
		// any step has run.
		plan, err = r.requestPlanWith(ctx, sess, tools,
			"Your previous plan was rejected before any step executed: "+vErr.Error()+" Produce a corrected plan that satisfies all constraints.", provider)
		if err != nil {
			return r.planFailure(ctx, sess, "The model could not produce a parseable plan. "+err.Error())
		}
		if vErr = planner.Validate(plan, schemas); vErr != nil {
			return r.planFailure(ctx, sess, "The model could not produce a valid plan. No operations were performed. Validation: "+vErr.Error())
		}
	}
	return r.runValidatedPlan(ctx, sess, userMessage, plan, schemas, provider, approvals)
}

// planFailure records a terminal response explaining that nothing ran and the
// plan was rejected pre-execution (completion criterion: zero side effects).
func (r *Runtime) planFailure(ctx context.Context, sess *Session, msg string) (*Response, error) {
	sess.addMessage(llm.Message{Role: "assistant", Content: msg})
	return &Response{Content: msg, SessionID: sess.ID}, nil
}

// requestPlan asks the model for a structured Plan (TAD §11.3). The response
// format pins output to the planner's "plan" schema.
func (r *Runtime) requestPlan(ctx context.Context, sess *Session, tools []llm.ToolDefinition, provider llm.Provider) (*planner.Plan, error) {
	return r.requestPlanWith(ctx, sess, tools, "", provider)
}

func (r *Runtime) requestPlanWith(ctx context.Context, sess *Session, tools []llm.ToolDefinition, extraPrompt string, provider llm.Provider) (*planner.Plan, error) {
	if extraPrompt != "" {
		sess.addMessage(llm.Message{Role: "user", Content: extraPrompt})
	}
	resp, err := r.chat(ctx, provider, sess, tools, planner.Format())
	if err != nil {
		return nil, err
	}
	plan, err := planner.Unmarshal(resp.Content)
	if err != nil {
		return nil, err
	}
	// Record the plan as an assistant content message so the transcript stays
	// well-formed for real providers (REVIEW-2026-08-12 finding 4). The plan
	// is produced under a constrained ResponseFormat (TAD §11.3), so it is
	// plain content — not a tool call.
	sess.addMessage(llm.Message{Role: "assistant", Content: resp.Content})
	return plan, nil
}

// runValidatedPlan executes a validated plan: plan-level human confirmation
// when any step requires approval (TAD §11.2 step b), then each step in order
// with ref:<i> references resolved from earlier results (step c), then a
// synthesized final summary.
func (r *Runtime) runValidatedPlan(ctx context.Context, sess *Session, userMessage string, plan *planner.Plan, schemas map[string]map[string]any, provider llm.Provider, approvals ApprovalGateway) (*Response, error) {
	if approved, needs := r.planApproval(ctx, sess, plan, schemas, approvals); needs {
		if !approved {
			msg := "The plan was not approved; no steps were executed."
			sess.addMessage(llm.Message{Role: "assistant", Content: msg})
			return &Response{Content: msg, SessionID: sess.ID}, nil
		}
	}

	results := make(map[int]string)
	steps := 0
	for i, step := range plan.Steps {
		callID := planCallID()

		args, err := r.resolveRefs(step.Args, results)
		if err != nil {
			appendPlanStep(sess, callID, step.Operation, step.Args, "error: "+err.Error())
			continue
		}
		if vErr := planner.ValidateArgs(step.Operation, args, schemas[step.Operation]); vErr != nil {
			appendPlanStep(sess, callID, step.Operation, args, "error: "+vErr.Error())
			continue
		}
		obs := r.executeTool(ctx, sess, step.Operation, args, userMessage, true, approvals)
		appendPlanStep(sess, callID, step.Operation, args, obs)
		results[i] = obs
		steps++
	}

	final := r.synthesizeSummary(ctx, sess, provider)
	sess.addMessage(llm.Message{Role: "assistant", Content: final})
	return &Response{Content: final, SessionID: sess.ID, ToolCalls: steps}, nil
}

// planApproval performs the single combined confirmation of TAD §11.2 step b:
// one approval_required round trip whose reason is the strictest reason among
// the plan's steps, after which every step executes without per-step prompts.
// Returns (approved, needsApproval).
func (r *Runtime) planApproval(ctx context.Context, sess *Session, plan *planner.Plan, schemas map[string]map[string]any, approvals ApprovalGateway) (bool, bool) {
	strict := ""
	found := false
	for _, step := range plan.Steps {
		a := r.safety.RequiresApprovalWithReason(ctx, auth.FromContext(ctx), safety.ToolCallInfo{
			Verb:     safety.VerbFor(step.Operation, step.Args),
			ToolName: step.Operation,
			Args:     step.Args,
		})
		if !a.Required {
			continue
		}
		found = true
		if stricter(a.Reason, strict) {
			strict = string(a.Reason)
		}
	}
	if !found {
		return true, false
	}

	if approvals == nil {
		return false, true
	}
	payload := ApprovalPayload{
		ActionID: newApprovalID(),
		Details: ApprovalDetails{
			Action:       "plan",
			Payload:      planSummary(plan),
			PolicyReason: strict,
		},
	}
	r.emit(Event{Type: EventApprovalRequired, Approval: &payload})
	resp, err := approvals.RequestApproval(ctx, payload)
	if err != nil {
		return false, true
	}
	return resp.Approved, true
}

// stricter reports whether candidate reason takes precedence over current
// (the TAD §12.1 evaluation order; empty current = any reason is stricter).
func stricter(candidate safety.ApprovalReason, current string) bool {
	if current == "" {
		return true
	}
	order := map[string]int{
		string(safety.ReasonAlwaysRequireApproval): 3,
		string(safety.ReasonBulkLimit):             2,
		string(safety.ReasonRoleOverride):          1,
		string(safety.ReasonRequireApproval):       0,
	}
	return order[string(candidate)] > order[current]
}

// planSummary renders the plan as a JSON payload for the approval details.
func planSummary(plan *planner.Plan) map[string]any {
	steps := make([]map[string]any, 0, len(plan.Steps))
	for _, s := range plan.Steps {
		steps = append(steps, map[string]any{"operation": s.Operation, "depends_on": s.DependsOn})
	}
	return map[string]any{"step_count": len(plan.Steps), "steps": steps}
}

// planCallID returns a fresh, well-formed tool-call id for a plan step's
// assistant tool_calls entry. Real providers require non-empty, unique ids
// (OpenAI convention "call_...") that the matching tool message echoes back.
func planCallID() string {
	return "call_" + ulid.Make().String()
}

// appendPlanStep records one executed plan step as a well-formed tool-call
// pair: the assistant message declaring the invocation (with a generated id)
// is appended before the tool message that references it. Real providers
// reject a tool message that has no matching assistant tool_calls entry
// (HTTP 400) — the malformed transcript that broke plan turns before
// REVIEW-2026-08-12 finding 4.
func appendPlanStep(sess *Session, callID, toolName string, args map[string]any, obs string) {
	sess.addMessage(llm.Message{
		Role:      "assistant",
		ToolCalls: []llm.ToolCall{{ID: callID, Name: toolName, Arguments: marshalArgs(args)}},
	})
	sess.addMessage(llm.Message{Role: "tool", ToolCallID: callID, Name: toolName, Content: obs})
}

// marshalArgs renders tool-call arguments as JSON for the assistant message.
func marshalArgs(args map[string]any) string {
	raw, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// resolveRefs substitutes every "ref:<i>[.field]" argument from the results of
// earlier plan steps (TAD §11.2 step c).
func (r *Runtime) resolveRefs(args map[string]any, results map[int]string) (map[string]any, error) {
	if len(results) == 0 {
		return args, nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		s, ok := v.(string)
		if !ok || !strings.HasPrefix(s, planner.RefPrefix) {
			out[k] = v
			continue
		}
		ref := strings.TrimPrefix(s, planner.RefPrefix)
		idxStr, field := ref, ""
		if dot := strings.IndexByte(ref, '.'); dot >= 0 {
			idxStr, field = ref[:dot], ref[dot+1:]
		}
		idx, err := strconv.Atoi(idxStr)
		if err != nil {
			return nil, fmt.Errorf("invalid result reference %q", ref)
		}
		res, ok := results[idx]
		if !ok {
			return nil, fmt.Errorf("step references result %q that was not produced by an earlier step", ref)
		}
		resolved, err := extractResult(res, field)
		if err != nil {
			return nil, err
		}
		out[k] = resolved
	}
	return out, nil
}

// extractResult pulls a field out of a prior step's JSON observation.
func extractResult(res, field string) (any, error) {
	if field == "" {
		return res, nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(res), &m); err != nil {
		return nil, fmt.Errorf("cannot extract %q from a non-object result", field)
	}
	v, ok := m[field]
	if !ok {
		return nil, fmt.Errorf("result has no field %q", field)
	}
	return v, nil
}

// synthesizeSummary asks the model to summarize the executed plan's results
// into the final answer, falling back to a deterministic summary when that
// call fails.
func (r *Runtime) synthesizeSummary(ctx context.Context, sess *Session, provider llm.Provider) string {
	resp, err := r.chat(ctx, provider, sess, nil, nil)
	if err != nil {
		return deterministicSummary(sess)
	}
	return resp.Content
}

// deterministicSummary is a non-LLM fallback that restates the last tool
// observations so the turn still returns a useful result.
func deterministicSummary(sess *Session) string {
	msgs := sess.Transcript()
	var obs []string
	for i := len(msgs) - 1; i >= 0 && len(obs) < 3; i-- {
		if msgs[i].Role == "tool" {
			obs = append(obs, msgs[i].Content)
		}
	}
	return "The plan completed. Last results:\n- " + strings.Join(obs, "\n- ")
}
