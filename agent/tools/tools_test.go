package tools_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/orjanda-framework/orjanda/agent/llm"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/api/rpc"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/perm"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/workflow"
)

// --- Fixtures (mirror the PRD §24.2 Employee example) ------------------------

type EmployeeSkill struct {
	schema.BaseChild
	SkillName string `oj:"required,label=Skill"`
	Level     int    `oj:"label=Level"`
}

type Department struct {
	schema.BaseDocument
	Name string `oj:"required"`
}

func (d *Department) DocMeta() schema.Meta {
	return schema.Meta{Name: "Department"}
}

type Employee struct {
	schema.BaseDocument
	FirstName  string          `oj:"required,label=First Name"`
	LastName   string          `oj:"required,label=Last Name"`
	Email      string          `oj:"required,unique,format=email,label=Email address"`
	Salary     schema.Currency `oj:"precision=2,permission=hr_manager,label=Salary"`
	JoinDate   schema.Date     `oj:"required,label=Join Date"`
	Department schema.Link     `oj:"link=Department"`
	Status     string          `oj:"options=Active|Inactive|On Leave,default=Active,label=Status"`
	Internal   string          `oj:"agent_hidden,label=Internal Note"`
	Skills     []EmployeeSkill `oj:"child_table,label=Skills"`
}

func (e *Employee) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       "Employee",
		Searchable: true,
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Write: true, Create: true, Delete: true},
			{Role: "recruiter", Read: true, Write: true, Create: true},
			{Role: "employee", Read: true},
		},
	}
}

type LeaveRequest struct {
	schema.BaseDocument
	Reason string `oj:"required"`
}

func (l *LeaveRequest) DocMeta() schema.Meta {
	return schema.Meta{
		Name: "LeaveRequest",
		Permissions: []schema.DocPermission{
			{Role: "hr_manager", Read: true, Write: true, Create: true, Delete: true},
			{Role: "employee", Read: true, Create: true},
		},
	}
}

type HiddenDoc struct {
	schema.BaseDocument
	Secret string `oj:"required"`
}

func (h *HiddenDoc) DocMeta() schema.Meta {
	return schema.Meta{Name: "HiddenDoc", AgentHidden: true}
}

// BulkDoc/BulkItem let us register N Documents cheaply (all identical shape).
type BulkItem struct {
	schema.BaseChild
	Label string `oj:"required"`
}

type BulkDoc struct {
	schema.BaseDocument
	MetaName string     `oj:"-"`
	Title    string     `oj:"required"`
	Items    []BulkItem `oj:"child_table"`
}

func (d *BulkDoc) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       d.MetaName,
		Searchable: true,
		Permissions: []schema.DocPermission{
			{Role: "System Administrator", Read: true, Write: true, Create: true, Delete: true},
		},
	}
}

type BulkItem2 struct {
	schema.BaseChild
	Label2 string `oj:"required"`
}

type BulkItem3 struct {
	schema.BaseChild
	Label3 string `oj:"required"`
}

type BulkDocMany struct {
	schema.BaseDocument
	MetaName string      `oj:"-"`
	Title    string      `oj:"required"`
	Items    []BulkItem  `oj:"child_table"`
	Extra1   []BulkItem2 `oj:"child_table"`
	Extra2   []BulkItem3 `oj:"child_table"`
}

func (d *BulkDocMany) DocMeta() schema.Meta {
	return schema.Meta{
		Name:       d.MetaName,
		Searchable: true,
		Permissions: []schema.DocPermission{
			{Role: "System Administrator", Read: true, Write: true, Create: true, Delete: true},
		},
	}
}

// --- Helpers ----------------------------------------------------------------

func newTestRegistry(t *testing.T) schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	for _, doc := range []schema.Document{
		&Department{}, &Employee{}, &LeaveRequest{}, &HiddenDoc{},
	} {
		if err := reg.Register("test", doc); err != nil {
			t.Fatalf("register: %v", err)
		}
	}
	if err := reg.Compile(); err != nil {
		t.Fatalf("compile: %v", err)
	}
	return reg
}

func newTestToolRegistry(t *testing.T, reg schema.Registry) tools.ToolRegistry {
	t.Helper()
	permEngine := perm.NewEngine(reg)

	// LeaveRequest has a workflow with several transitions (TAD §8).
	wfEngine := workflow.NewEngine(nil, reg, permEngine, nil, nil)
	if err := wfEngine.Register(workflow.Definition{
		DocType: "LeaveRequest",
		States: []workflow.State{
			{Name: "Draft"}, {Name: "Submitted"}, {Name: "Approved"},
			{Name: "Completed"}, {Name: "Cancelled"},
		},
		Transitions: []workflow.Transition{
			{From: "Draft", To: "Submitted", Action: "Submit", AllowedRoles: []string{"employee"}},
			{From: "Submitted", To: "Approved", Action: "Approve", AllowedRoles: []string{"hr_manager"}},
			{From: "Submitted", To: "Rejected", Action: "Reject", AllowedRoles: []string{"hr_manager"}},
			{From: "Approved", To: "Completed", Action: "Complete", AllowedRoles: []string{"hr_manager"}},
			{From: "*", To: "Cancelled", Action: "Cancel", AllowedRoles: []string{"employee"}},
		},
	}); err != nil {
		t.Fatalf("register workflow: %v", err)
	}

	tr := tools.NewToolRegistry(permEngine, wfEngine)
	if err := tr.Compile(reg); err != nil {
		t.Fatalf("tool compile: %v", err)
	}
	return tr
}

func findTool(defs []llm.ToolDefinition, name string) *llm.ToolDefinition {
	for i := range defs {
		if defs[i].Name == name {
			return &defs[i]
		}
	}
	return nil
}

// --- Completion criterion: PRD §24.2 Employee/salary -------------------------

func TestForIdentitySalaryGating(t *testing.T) {
	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	t.Run("hr_manager sees salary in create_employee", func(t *testing.T) {
		defs := tr.ForIdentity(context.Background(), auth.Identity{
			UserID: "u-hr", Roles: []string{"hr_manager"},
		})
		create := findTool(defs, "create_employee")
		if create == nil {
			t.Fatal("create_employee missing for hr_manager")
		}
		props := create.Parameters["properties"].(map[string]any)
		if _, ok := props["salary"]; !ok {
			t.Errorf("create_employee properties = %v, want salary present", keys(props))
		}
	})

	t.Run("recruiter with Create but not hr_manager omits salary", func(t *testing.T) {
		defs := tr.ForIdentity(context.Background(), auth.Identity{
			UserID: "u-rec", Roles: []string{"recruiter"},
		})
		create := findTool(defs, "create_employee")
		if create == nil {
			t.Fatal("create_employee missing for recruiter (has Create)")
		}
		props := create.Parameters["properties"].(map[string]any)
		if _, ok := props["salary"]; ok {
			t.Errorf("create_employee properties contain salary for non-hr_manager: %v", keys(props))
		}
	})

	t.Run("employee without Create gets no create_employee at all", func(t *testing.T) {
		defs := tr.ForIdentity(context.Background(), auth.Identity{
			UserID: "u-emp", Roles: []string{"employee"},
		})
		if create := findTool(defs, "create_employee"); create != nil {
			t.Errorf("create_employee present for read-only employee role")
		}
	})
}

func TestCreateEmployeeSchemaShape(t *testing.T) {
	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	defs := tr.ForIdentity(context.Background(), auth.Identity{
		UserID: "u-hr", Roles: []string{"hr_manager"},
	})
	create := findTool(defs, "create_employee")
	if create == nil {
		t.Fatal("create_employee missing")
	}
	props := create.Parameters["properties"].(map[string]any)
	required := toStringSlice(create.Parameters["required"])

	// PRD §24.2 required params: only fields where Required && !Hidden.
	wantRequired := []string{"first_name", "last_name", "email", "join_date"}
	if fmt.Sprint(required) != fmt.Sprint(wantRequired) {
		t.Errorf("required = %v, want %v", required, wantRequired)
	}

	// agent_hidden field excluded from the agent schema entirely (TAD §12.2).
	if _, ok := props["internal"]; ok {
		t.Error("agent_hidden field 'internal' present in create_employee")
	}

	// Link description per TAD §10.2.
	dept := props["department"].(map[string]any)
	if dept["description"] != "Reference to a Department document" {
		t.Errorf("department description = %v", dept["description"])
	}

	// options → enum; default preserved.
	status := props["status"].(map[string]any)
	if fmt.Sprint(status["enum"]) != "[Active Inactive On Leave]" {
		t.Errorf("status enum = %v", status["enum"])
	}
	if status["default"] != "Active" {
		t.Errorf("status default = %v", status["default"])
	}

	// Child table nests inside the parent payload (TAD §10.1 step 7).
	skills, ok := props["skills"].(map[string]any)
	if !ok {
		t.Fatal("skills child table property missing from create_employee")
	}
	if skills["type"] != "array" {
		t.Errorf("skills type = %v, want array", skills["type"])
	}

	// description follows PRD §24.2 ("Email address (required, must be unique)").
	email := props["email"].(map[string]any)
	if email["description"] != "Email address (required, must be unique)" {
		t.Errorf("email description = %v", email["description"])
	}
	if email["format"] != "email" {
		t.Errorf("email format = %v", email["format"])
	}
}

// --- Completion criterion: O(len(CompiledDocs)) tool count -------------------

func TestToolCountNotProportionalToChildTables(t *testing.T) {
	countFor := func(register func(reg schema.Registry), n int) int {
		reg := schema.NewRegistry()
		register(reg)
		if err := reg.Compile(); err != nil {
			t.Fatalf("compile: %v", err)
		}
		tr := tools.NewToolRegistry(perm.NewEngine(reg), nil)
		if err := tr.Compile(reg); err != nil {
			t.Fatalf("tool compile: %v", err)
		}
		return len(tr.ForIdentity(context.Background(), auth.Identity{Roles: []string{"System Administrator"}}))
	}

	withOneChild := func(reg schema.Registry) {
		for i := 0; i < 50; i++ {
			if err := reg.Register("app", &BulkDoc{MetaName: fmt.Sprintf("Doc%02d", i)}); err != nil {
				t.Fatalf("register: %v", err)
			}
		}
	}
	withThreeChildren := func(reg schema.Registry) {
		for i := 0; i < 50; i++ {
			if err := reg.Register("app", &BulkDocMany{MetaName: fmt.Sprintf("Doc%02d", i)}); err != nil {
				t.Fatalf("register: %v", err)
			}
		}
	}

	one := countFor(withOneChild, 50)
	three := countFor(withThreeChildren, 50)

	// 50 docs × (search, list, get, create, update, delete) + 3 discovery.
	const expected = 50*6 + 3
	if one != expected {
		t.Errorf("tool count (1 child/doc) = %d, want %d", one, expected)
	}
	if three != one {
		t.Errorf("tool count grew from %d to %d when child-table count per doc tripled — must stay O(len(CompiledDocs)) (TAD §10.1 step 7)", one, three)
	}
}

// --- Completion criterion: exactly one execute_action per workflowed DocType --

func TestExecuteActionOnePerWorkflowedDocType(t *testing.T) {
	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	defs := tr.ForIdentity(context.Background(), auth.Identity{
		UserID: "u-hr", Roles: []string{"hr_manager"},
	})

	var actionTools []string
	for _, d := range defs {
		if strings.HasPrefix(d.Name, "execute_action_leave_request") {
			actionTools = append(actionTools, d.Name)
		}
	}
	if len(actionTools) != 1 {
		t.Fatalf("execute_action_leave_request count = %d (%v), want exactly 1 despite 5 transitions (TAD §8.2)", len(actionTools), actionTools)
	}
	if got := actionTools[0]; got != "execute_action_leave_request" {
		t.Errorf("tool name = %q, want execute_action_leave_request", got)
	}

	// The action enum starts empty at compile time (TAD §8.2, populated per call).
	exec := findTool(defs, "execute_action_leave_request")
	action := exec.Parameters["properties"].(map[string]any)["action"].(map[string]any)
	if enum, ok := action["enum"]; ok && len(enum.([]any)) != 0 {
		t.Errorf("compile-time action enum = %v, want empty", enum)
	}
}

// --- agent_hidden exclusion (TAD §10.1, §12.2) -------------------------------

func TestAgentHiddenDocAndFieldExcluded(t *testing.T) {
	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	defs := tr.ForIdentity(context.Background(), auth.Identity{
		UserID: "u-sys", Roles: []string{"System Administrator"},
	})
	for _, name := range []string{
		"list_hidden_doc", "get_hidden_doc", "create_hidden_doc",
		"update_hidden_doc", "delete_hidden_doc", "search_hidden_doc",
	} {
		if findTool(defs, name) != nil {
			t.Errorf("tool %q present for agent_hidden Document", name)
		}
	}
}

// --- RPC method tools (TAD §10.1 step 8) --------------------------------------

func TestRPCMethodTools(t *testing.T) {
	rpc.ResetRegistry()
	t.Cleanup(rpc.ResetRegistry)

	rpc.RegisterMethod("hr.leave.get_balance", func(ctx context.Context, args map[string]any) (any, error) {
		return 12, nil
	}, rpc.MethodOpts{AllowedRoles: []string{"hr_manager"}})
	rpc.RegisterMethod("public.ping", func(ctx context.Context, args map[string]any) (any, error) {
		return "pong", nil
	}, rpc.MethodOpts{AllowedRoles: []string{}}) // not role-gated → no tool

	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	hr := tr.ForIdentity(context.Background(), auth.Identity{UserID: "u-hr", Roles: []string{"hr_manager"}})
	if findTool(hr, "hr_leave_get_balance") == nil {
		t.Error("hr_leave_get_balance missing for hr_manager")
	}
	if findTool(hr, "public_ping") != nil {
		t.Error("public_ping generated though it has no AllowedRoles gate")
	}

	emp := tr.ForIdentity(context.Background(), auth.Identity{UserID: "u-emp", Roles: []string{"employee"}})
	if findTool(emp, "hr_leave_get_balance") != nil {
		t.Error("hr_leave_get_balance present for employee without the gate role")
	}
}

// --- Custom tools (TAD §10.4, PRD §24.3) --------------------------------------

func TestCustomToolsMergedAndRoleFiltered(t *testing.T) {
	tools.ResetCustomTools()
	t.Cleanup(tools.ResetCustomTools)

	tools.RegisterCustomTool(tools.Tool{
		Name:         "calculate_leave_balance",
		Description:  "Calculate remaining leave balance",
		Parameters:   map[string]any{"type": "object"},
		AllowedRoles: []string{"hr_manager"},
	})
	tools.RegisterCustomTool(tools.Tool{
		Name:        "common_helper",
		Description: "Available to everyone",
		Parameters:  map[string]any{"type": "object"},
	})

	reg := newTestRegistry(t)
	tr := newTestToolRegistry(t, reg)

	hr := tr.ForIdentity(context.Background(), auth.Identity{UserID: "u-hr", Roles: []string{"hr_manager"}})
	if findTool(hr, "calculate_leave_balance") == nil {
		t.Error("custom tool missing for hr_manager")
	}
	if findTool(hr, "common_helper") == nil {
		t.Error("ungated custom tool missing")
	}

	emp := tr.ForIdentity(context.Background(), auth.Identity{UserID: "u-emp", Roles: []string{"employee"}})
	if findTool(emp, "calculate_leave_balance") != nil {
		t.Error("role-gated custom tool present for employee")
	}
	if findTool(emp, "common_helper") == nil {
		t.Error("ungated custom tool missing for employee")
	}
}

// --- helpers -----------------------------------------------------------------

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func toStringSlice(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
