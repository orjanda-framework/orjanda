package planner

import (
	"testing"
)

// testSchemas is a two-tool projected schema set mirroring what the Context
// Manager would build for a session that has seen Employee and LeaveRequest.
func testSchemas() map[string]map[string]any {
	return map[string]map[string]any{
		"get_employee": {
			"type": "object",
			"properties": map[string]any{
				"id": map[string]any{"type": "string"},
			},
			"required": []any{"id"},
		},
		"create_employee": {
			"type": "object",
			"properties": map[string]any{
				"first_name": map[string]any{"type": "string"},
				"last_name":  map[string]any{"type": "string"},
				"department": map[string]any{
					"type": "string",
					"enum": []any{"Engineering", "HR", "Finance"},
				},
				"skills": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"skill_name": map[string]any{"type": "string"},
						},
						"required": []any{"skill_name"},
					},
				},
			},
			"required": []any{"first_name", "last_name"},
		},
	}
}

func TestValidate_ValidPlan(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "get_employee", Args: map[string]any{"id": "EMP-1"}},
		{
			Operation: "create_employee",
			Args:      map[string]any{"first_name": "ref:0", "last_name": "Doe"},
			DependsOn: []int{0},
		},
	}}
	if err := Validate(plan, testSchemas()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidate_UnknownOperation(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "delete_employee", Args: map[string]any{"id": "EMP-1"}},
	}}
	err := Validate(plan, testSchemas())
	if err == nil {
		t.Fatal("Validate: want error for unknown operation")
	}
	if err.Error() != `step 1 references unknown operation "delete_employee"` {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidate_MissingRequiredArgument(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "create_employee", Args: map[string]any{"first_name": "A"}},
	}}
	err := Validate(plan, testSchemas())
	if err == nil {
		t.Fatal("Validate: want error for missing required arg")
	}
	want := `step 1: operation "create_employee" missing required argument "last_name"`
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestValidate_InvalidEnum(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "create_employee", Args: map[string]any{
			"first_name": "A", "last_name": "B", "department": "Marketing",
		}},
	}}
	err := Validate(plan, testSchemas())
	if err == nil {
		t.Fatal("Validate: want error for enum violation")
	}
}

func TestValidate_TypeMismatch(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "get_employee", Args: map[string]any{"id": 123}},
	}}
	err := Validate(plan, testSchemas())
	if err == nil {
		t.Fatal("Validate: want error for type mismatch")
	}
}

func TestValidate_DependsOnCycle(t *testing.T) {
	cases := []struct {
		name  string
		steps []PlanStep
	}{
		{name: "self reference", steps: []PlanStep{{Operation: "get_employee", Args: map[string]any{"id": "x"}, DependsOn: []int{0}}}},
		{name: "forward reference", steps: []PlanStep{
			{Operation: "get_employee", Args: map[string]any{"id": "x"}, DependsOn: []int{1}},
			{Operation: "get_employee", Args: map[string]any{"id": "y"}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(&Plan{Steps: tc.steps}, testSchemas()); err == nil {
				t.Fatal("Validate: want error for invalid depends_on")
			}
		})
	}
}

func TestValidate_EmptyPlan(t *testing.T) {
	if err := Validate(nil, testSchemas()); err == nil {
		t.Fatal("Validate: want error for nil plan")
	}
	if err := Validate(&Plan{}, testSchemas()); err == nil {
		t.Fatal("Validate: want error for empty plan")
	}
}

func TestValidate_NestedChildItems(t *testing.T) {
	plan := &Plan{Steps: []PlanStep{
		{Operation: "create_employee", Args: map[string]any{
			"first_name": "A",
			"last_name":  "B",
			"skills": []any{
				map[string]any{"skill_name": "Go"},
				map[string]any{}, // missing required skill_name
			},
		}},
	}}
	if err := Validate(plan, testSchemas()); err == nil {
		t.Fatal("Validate: want error for nested missing required field")
	}
}

func TestUnmarshal_Valid(t *testing.T) {
	plan, err := Unmarshal(`{"steps":[{"operation":"get_employee","args":{"id":"x"},"depends_on":[]}]}`)
	if err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].Operation != "get_employee" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	if _, err := Unmarshal(`not json`); err == nil {
		t.Fatal("Unmarshal: want error for invalid JSON")
	}
}

func TestFormat(t *testing.T) {
	f := Format()
	if f == nil || f.Name != "plan" {
		t.Fatalf("unexpected format: %+v", f)
	}
	props, ok := f.Schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema missing properties: %+v", f.Schema)
	}
	if _, ok := props["steps"]; !ok {
		t.Fatalf("schema missing steps property: %+v", props)
	}
}
