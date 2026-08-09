// Package planner implements the Plan-and-Execute planning contract
// (TAD §11.3): the structured Plan/PlanStep output the LLM produces under a
// constrained ResponseFormat, and whole-plan validation against the projected
// ToolTemplate schemas before any step executes.
//
// The validation guarantee is the load-bearing one: an invalid Plan — an
// unknown Operation, a missing required field, a DependsOn cycle — is rejected
// wholesale and returned to the LLM as a single correction turn, never
// partially executed. This is what prevents a plan with a bad step 3 from
// having already executed steps 1–2 with real side effects (TAD §11.3,
// Plan Phase 8 completion criterion).
package planner

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/orjanda-framework/orjanda/agent/llm"
)

// RefPrefix is the in-argument reference marker the executor uses to resolve
// one step's Args from a prior step's result (TAD §11.2 step c: "feeding each
// result into the next step's argument resolution"). A string value of
// "ref:0" is replaced by step 0's result when the step runs. The same marker
// is what the mode classifier scans for in ReAct tool-call arguments to
// detect a data dependency (§11.2 step 2).
const RefPrefix = "ref:"

// Plan is the structured output of Plan-and-Execute mode (TAD §11.3).
type Plan struct {
	Steps []PlanStep `json:"steps"`
}

// PlanStep is one unit of a Plan. Operation is a tool name from this turn's
// ForIdentity() result; DependsOn holds the indices of prior steps whose
// results this step's Args reference.
type PlanStep struct {
	Operation string         `json:"operation"`
	Args      map[string]any `json:"args"`
	DependsOn []int          `json:"depends_on,omitempty"`
}

// Format returns the JSON Schema format constraining the LLM to emit a Plan
// (TAD §11.3). Pass it as llm.ChatRequest.ResponseFormat in Plan-and-Execute
// mode; providers that lack SupportsStructuredOutput() ignore it.
func Format() *llm.JSONSchemaFormat {
	step := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"operation": map[string]any{"type": "string"},
			"args": map[string]any{
				"type":                 "object",
				"additionalProperties": true,
			},
			"depends_on": map[string]any{
				"type":  "array",
				"items": map[string]any{"type": "integer"},
			},
		},
		"required": []any{"operation", "args"},
	}
	return &llm.JSONSchemaFormat{
		Name: "plan",
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"steps": map[string]any{
					"type":  "array",
					"items": step,
				},
			},
			"required": []any{"steps"},
		},
	}
}

// Unmarshal decodes a Plan from the LLM's structured text output. It returns
// a descriptive error (safe to feed back to the LLM as one correction turn,
// TAD §11.3) when the output is not a valid Plan.
func Unmarshal(text string) (*Plan, error) {
	p := &Plan{}
	if err := json.Unmarshal([]byte(text), p); err != nil {
		return nil, fmt.Errorf("model output is not a valid plan: %v", err)
	}
	return p, nil
}

// Validate performs whole-plan pre-execution validation (TAD §11.3) against
// the projected tool schemas for this turn: every Operation must exist in
// schemas (keyed by tool name), every step's Args must satisfy its
// operation's JSON Schema (required presence, type, enum), and DependsOn must
// reference strictly earlier steps (cycles/self-references are rejected).
//
// The validation is intentionally a JSON Schema subset — full schema
// validation lives in the Document Engine — but it is strong enough to catch
// the failure modes TAD §11.3 names (unknown operation, missing required
// field, dependency cycle). Any non-nil return means zero steps have executed.
func Validate(p *Plan, schemas map[string]map[string]any) error {
	if p == nil {
		return fmt.Errorf("plan is empty: no steps were produced")
	}
	if len(p.Steps) == 0 {
		return fmt.Errorf("plan is empty: no steps were produced")
	}

	seen := make(map[string]bool, len(schemas))
	for name := range schemas {
		seen[name] = true
	}

	for i, s := range p.Steps {
		if s.Operation == "" {
			return fmt.Errorf("step %d has an empty operation", i+1)
		}
		if !seen[s.Operation] {
			return fmt.Errorf("step %d references unknown operation %q", i+1, s.Operation)
		}

		if err := validateArgs(s.Operation, s.Args, schemas[s.Operation]); err != nil {
			return fmt.Errorf("step %d: %v", i+1, err)
		}

		for _, dep := range s.DependsOn {
			if dep < 0 || dep >= i {
				return fmt.Errorf("step %d depends_on %d is not a prior step (cycle or self-reference)", i+1, dep)
			}
		}
	}
	return nil
}

// ValidateArgs validates a single step's (already resolved) args against one
// tool's projected JSON Schema. The Executor re-validates after reference
// resolution and before the step runs (TAD §11.2 step c / §11.3) so a
// resolved value never reaches the Document Engine's transaction boundary
// with a missing required field.
func ValidateArgs(operation string, args map[string]any, sch map[string]any) error {
	if sch == nil {
		return fmt.Errorf("operation %q has no projected schema for this turn", operation)
	}
	return validateArgs(operation, args, sch)
}

// validateArgs checks args against a single tool's JSON Schema: required
// property presence, scalar type, and enum membership. Missing required args
// are the primary failure mode; type/enum mismatches are caught here too so a
// bad value never reaches the Document Engine's transaction boundary.
func validateArgs(operation string, args map[string]any, sch map[string]any) error {
	if args == nil {
		args = map[string]any{}
	}

	required, _ := sch["required"].([]any)
	for _, r := range required {
		key, _ := r.(string)
		if key == "" {
			continue
		}
		if _, ok := args[key]; !ok || isEmptyValue(args[key]) {
			return fmt.Errorf("operation %q missing required argument %q", operation, key)
		}
	}

	props, _ := sch["properties"].(map[string]any)
	for k, v := range args {
		if isEmptyValue(v) {
			continue
		}
		prop, ok := props[k].(map[string]any)
		if !ok {
			continue // unknown extras are permitted (permissive JSON Schema default)
		}
		if err := checkValue(operation, k, prop, v); err != nil {
			return err
		}
	}
	return nil
}

// checkValue validates one argument against its property schema. Arrays with
// an items schema are validated recursively against their elements.
func checkValue(operation, key string, prop map[string]any, v any) error {
	if enums, ok := prop["enum"].([]any); ok && len(enums) > 0 {
		matched := false
		sv := fmt.Sprintf("%v", v)
		for _, e := range enums {
			if fmt.Sprintf("%v", e) == sv {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("operation %q argument %q = %q is not one of the allowed options", operation, key, sv)
		}
	}

	if typ, _ := prop["type"].(string); typ != "" {
		if err := checkType(operation, key, typ, v); err != nil {
			return err
		}
	}

	if items, ok := prop["items"].(map[string]any); ok {
		if arr, ok := v.([]any); ok {
			for _, elem := range arr {
				elemMap, ok := elem.(map[string]any)
				if !ok {
					continue
				}
				if err := validateArgs(operation, elemMap, items); err != nil {
					return fmt.Errorf("operation %q argument %q: %v", operation, key, err)
				}
			}
		}
	}
	return nil
}

// checkType asserts the Go value's runtime type matches the JSON Schema type.
// Numbers are checked leniently (float64/int both satisfy "number" and
// "integer" in schema terms).
func checkType(operation, key, typ string, v any) error {
	ok := false
	switch typ {
	case "string":
		_, ok = v.(string)
	case "integer":
		switch v.(type) {
		case int, int64, float64:
			ok = true
		}
	case "number":
		switch v.(type) {
		case int, int64, float64:
			ok = true
		}
	case "boolean":
		_, ok = v.(bool)
	case "object":
		_, ok = v.(map[string]any)
	case "array":
		_, ok = v.([]any)
	}
	if !ok {
		return fmt.Errorf("operation %q argument %q has type %T, want %q", operation, key, v, typ)
	}
	return nil
}

func isEmptyValue(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case map[string]any:
		return len(t) == 0
	case []any:
		return len(t) == 0
	}
	return false
}

// SortedOperations returns the known operation names sorted, for stable
// messages and tests.
func SortedOperations(schemas map[string]map[string]any) []string {
	names := make([]string, 0, len(schemas))
	for name := range schemas {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
