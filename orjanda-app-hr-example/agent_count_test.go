package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/orjanda-framework/orjanda/agent"
	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/document"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAgentAnswersEmployeeCountByDepartment verifies the Phase 12 criterion
// "the agent answers 'How many employees are in department X?' correctly via
// live tool calls (not a mock) against a seeded database" (Plan line 376).
//
// The LLM backend is deterministic, but the tool calls are live: every call
// executes through the real Agent Runtime against the seeded in-memory SQLite
// database via the Document Engine (PRD §23.1 — the single execution path).
// The final answer is computed from the tool observations returned by those
// live calls — never hardcoded — so a wrong seed size or a broken search/list
// path makes the assertion fail.
//
// The flow mirrors what a real model must do with the TAD §10.1 tool surface:
//  1. describe_document("Department")  — discovery turn, attaches Department ops
//  2. list_department()                — locate Engineering's record id
//  3. describe_document("Employee")    — discovery turn, attaches Employee ops
//  4. list_employee()                  — fetch live employee rows
//  5. count rows whose department == Engineering's id and answer
//
// (A single search_employee("Engineering") cannot work by construction: the
// Employee.department column stores the Department's ULID, and the MVP FTS is
// a LIKE over raw stored values — see search/search.go and TAD §9.1. The join
// above is the correct path on the current surface.)
func TestAgentAnswersEmployeeCountByDepartment(t *testing.T) {
	site := newHRSite(t)
	hr := site.CreateUser(t, "hr@test.com", "hr_manager")
	ctx := site.WithUser(hr)

	engDeptID, err := site.Document.Create(ctx, "Department", map[string]any{"Name": "Engineering"})
	require.NoError(t, err)
	mktDeptID, err := site.Document.Create(ctx, "Department", map[string]any{"Name": "Marketing"})
	require.NoError(t, err)

	seeded := []map[string]any{
		{"FirstName": "Ada", "LastName": "Lovelace", "Email": "ada@test.com", "Department": engDeptID, "JoinDate": "2021-01-01"},
		{"FirstName": "Grace", "LastName": "Hopper", "Email": "grace@test.com", "Department": engDeptID, "JoinDate": "2021-02-01"},
		{"FirstName": "Linus", "LastName": "Torvalds", "Email": "linus@test.com", "Department": engDeptID, "JoinDate": "2021-03-01"},
		{"FirstName": "Steve", "LastName": "Jobs", "Email": "steve@test.com", "Department": mktDeptID, "JoinDate": "2021-04-01"},
	}
	for _, rec := range seeded {
		_, err := site.Document.Create(ctx, "Employee", rec)
		require.NoError(t, err, "seed employee %v", rec)
	}

	resp, err := site.Agent.Execute(ctx, "How many employees are in Engineering?",
		agent.WithProvider(&employeeCountProvider{}))
	require.NoError(t, err)

	// The answer must contain the true count of Engineering employees (3) —
	// derived from live tool observations, not from a scripted string, and not
	// confused with the Marketing employee or a 1x/13x prefix.
	want := 3
	assert.Equal(t, 4, resp.ToolCalls, "exactly the four live tool calls of the join flow")
	assert.Contains(t, resp.Content, fmt.Sprintf("There are %d employees", want))

	// Belt-and-suspenders: confirm the seeded ground truth the agent saw.
	rows, err := site.Document.List(ctx, "Employee", document.ListOpts{Limit: 100})
	require.NoError(t, err)
	count := 0
	for _, row := range rows {
		if row["department"] == engDeptID {
			count++
		}
	}
	assert.Equal(t, want, count, "seeded Engineering headcount must match the seed")
}

// employeeCountProvider is a deterministic llm.Provider that drives the live
// tool-call join for the headcount question and computes the answer from the
// tool observations in the transcript. See TestAgentAnswersEmployeeCountByDepartment.
type employeeCountProvider struct {
	calls int
}

func (p *employeeCountProvider) ChatCompletion(_ context.Context, req llm.ChatRequest) (*llm.ChatResponse, error) {
	p.calls++
	switch p.calls {
	case 1:
		return toolCallResponse("describe_document", `{"doc_type":"Department"}`), nil
	case 2:
		return toolCallResponse("list_department", `{"limit":100}`), nil
	case 3:
		return toolCallResponse("describe_document", `{"doc_type":"Employee"}`), nil
	case 4:
		return toolCallResponse("list_employee", `{"limit":100}`), nil
	}
	count := countEmployeesInDepartment(req.Messages, engineeringDepartmentID(req.Messages))
	return &llm.ChatResponse{
		Content:      fmt.Sprintf("There are %d employees in the Engineering department.", count),
		FinishReason: "stop",
	}, nil
}

func toolCallResponse(name, args string) *llm.ChatResponse {
	return &llm.ChatResponse{
		ToolCalls:    []llm.ToolCall{{Name: name, Arguments: args}},
		FinishReason: "tool_calls",
	}
}

func (p *employeeCountProvider) StreamChatCompletion(context.Context, llm.ChatRequest) (<-chan llm.ChatChunk, error) {
	return nil, errors.New("employeeCountProvider does not implement streaming")
}

func (p *employeeCountProvider) SupportsToolCalling() bool      { return true }
func (p *employeeCountProvider) SupportsStructuredOutput() bool { return true }
func (p *employeeCountProvider) ModelInfo() llm.ModelInfo       { return llm.ModelInfo{Name: "employee-count-provider"} }

// engineeringDepartmentID extracts the Engineering department's id from the
// list_department tool observation in the transcript, or "" when absent.
func engineeringDepartmentID(msgs []llm.Message) string {
	for _, m := range msgs {
		if m.Role != "tool" || m.Name != "list_department" {
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal([]byte(m.Content), &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if row["name"] == "Engineering" {
				id, _ := row["id"].(string)
				return id
			}
		}
	}
	return ""
}

// countEmployeesInDepartment counts the live employee rows whose department
// link equals deptID, using the list_employee tool observation. A missing
// deptID yields 0 so a broken upstream step fails the test's content assert.
func countEmployeesInDepartment(msgs []llm.Message, deptID string) int {
	if deptID == "" {
		return 0
	}
	count := 0
	for _, m := range msgs {
		if m.Role != "tool" || m.Name != "list_employee" {
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal([]byte(m.Content), &rows); err != nil {
			continue
		}
		for _, row := range rows {
			if row["department"] == deptID {
				count++
			}
		}
	}
	return count
}
