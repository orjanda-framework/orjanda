package testing

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/runtime"
)

// MockStep is one entry in a MockLLM step queue: a scripted ChatCompletion
// response (ToolCall/TextResponse) or an approval round trip (ApprovalPrompt).
// See TAD §17.
type MockStep interface {
	isMockStep()
}

type toolCallStep struct {
	calls []llm.ToolCall
}

func (toolCallStep) isMockStep() {}

type textStep struct {
	text string
}

func (textStep) isMockStep() {}

type approvalStep struct {
	approved bool
}

func (approvalStep) isMockStep() {}

// ToolCall scripts a single ChatCompletion response asking the executor to
// invoke name with the given arguments (TAD §17). Arguments are JSON-encoded
// into the tool-call payload exactly as a real model would emit them.
func ToolCall(name string, args map[string]any) MockStep {
	raw, err := json.Marshal(args)
	if err != nil {
		panic("testing: ToolCall arguments are not JSON-marshalable: " + err.Error())
	}
	return toolCallStep{calls: []llm.ToolCall{{Name: name, Arguments: string(raw)}}}
}

// ToolCalls merges multiple ToolCall steps into a single ChatCompletion
// response. Plan-and-Execute tests need this to script the data-dependency
// escalation signal of TAD §11.2 step 2: a response whose later call
// references an earlier call's result via a "ref:<i>" argument.
func ToolCalls(steps ...MockStep) MockStep {
	var calls []llm.ToolCall
	for _, s := range steps {
		tc, ok := s.(toolCallStep)
		if !ok {
			panic("testing: ToolCalls expects only ToolCall steps, got " + fmt.Sprintf("%T", s))
		}
		calls = append(calls, tc.calls...)
	}
	return toolCallStep{calls: calls}
}

// TextResponse scripts a single ChatCompletion response containing only text
// (TAD §17). It also scripts structured-output plan responses in
// Plan-and-Execute mode: the text is handed to planner.Unmarshal verbatim.
func TextResponse(text string) MockStep {
	return textStep{text: text}
}

// ApprovalPrompt scripts the client-side approval round trip (TAD §12.3): the
// next RequestApproval call is answered with Approved. Place it in the queue
// where the human's decision belongs — for a plan approval that is between the
// plan JSON response and the final summary synthesis (TAD §11.2 step b).
func ApprovalPrompt() MockStep {
	return approvalStep{approved: true}
}

// mockLLM is the concrete scripted provider returned by MockLLM. It
// implements llm.Provider and runtime.ApprovalGateway on a single shared step
// queue, so a full Plan-and-Execute + approval exchange is scripted as one
// ordered MockStep sequence (TAD §17.1 guarantee 4). Steps are consumed in
// call order by successive ChatCompletion invocations; approval round trips
// pop ApprovalPrompt entries.
type mockLLM struct {
	mu        sync.Mutex
	t         *testing.T
	steps     []MockStep
	requests  []llm.ChatRequest
	approvals []runtime.ApprovalPayload
	callSeq   int
}

// MockLLM builds a scripted llm.Provider whose responses are deterministic
// and require no network access or API keys (PRD §32.1). Running out of
// steps — or consuming a step from the wrong call type — fails the test with
// a descriptive message, so an over- or under-scripted exchange cannot pass
// silently.
func MockLLM(t *testing.T, steps ...MockStep) llm.Provider {
	return &mockLLM{t: t, steps: steps}
}

func (m *mockLLM) ChatCompletion(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)

	head := m.popStep("ChatCompletion", "ToolCall/ToolCalls/TextResponse")
	switch h := head.(type) {
	case toolCallStep:
		calls := make([]llm.ToolCall, len(h.calls))
		for i, c := range h.calls {
			c.ID = fmt.Sprintf("call-%d", m.callSeq)
			m.callSeq++
			calls[i] = c
		}
		return &llm.ChatResponse{ToolCalls: calls, FinishReason: "tool_calls"}, nil
	case textStep:
		return &llm.ChatResponse{Content: h.text, FinishReason: "stop"}, nil
	}
	m.t.Fatalf("testing: MockLLM.ChatCompletion consumed step of type %T; expected ToolCall/ToolCalls or TextResponse", head)
	return nil, fmt.Errorf("testing: MockLLM consumed an invalid step type %T", head)
}

// RequestApproval implements runtime.ApprovalGateway (TAD §12.3). It consumes
// the next ApprovalPrompt step; encountering any other step here means the
// approval exchange was scripted out of order.
func (m *mockLLM) RequestApproval(_ context.Context, req runtime.ApprovalPayload) (runtime.ApprovalResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.approvals = append(m.approvals, req)

	head := m.popStep("RequestApproval", "ApprovalPrompt")
	ap, ok := head.(approvalStep)
	if !ok {
		m.t.Fatalf("testing: MockLLM.RequestApproval consumed step of type %T; expected ApprovalPrompt", head)
	}
	return runtime.ApprovalResponse{ActionID: req.ActionID, Approved: ap.approved}, nil
}

func (m *mockLLM) StreamChatCompletion(context.Context, llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, fmt.Errorf("testing: MockLLM does not implement streaming; script non-streaming steps")
}

func (m *mockLLM) SupportsToolCalling() bool      { return true }
func (m *mockLLM) SupportsStructuredOutput() bool { return true }
func (m *mockLLM) ModelInfo() llm.ModelInfo       { return llm.ModelInfo{Name: "mock"} }

// popStep pops the next step or fails the test on exhaustion.
func (m *mockLLM) popStep(caller, want string) MockStep {
	if len(m.steps) == 0 {
		m.t.Fatalf("testing: MockLLM ran out of scripted steps during %s (expected a %s step)", caller, want)
		return nil
	}
	head := m.steps[0]
	m.steps = m.steps[1:]
	return head
}
