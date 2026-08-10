package main

import (
	"context"
	"fmt"

	"github.com/orjanda-framework/orjanda"
	"github.com/orjanda-framework/orjanda/agent/tools"
	"github.com/orjanda-framework/orjanda/document"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
)

// appSite is the live site composition root captured by configure. The custom
// tool handler needs it to query the Document Engine; it is only ever read
// during agent turns, by which time configure has run (PRD §24.3).
var appSite *orjanda.Site

// init registers the one hand-written agent tool the HR example needs
// (PRD §24.3 / §37): every other tool on the four Documents is generated
// automatically from the Registry (TAD §10).
func init() {
	tools.RegisterCustomTool(tools.Tool{
		Name:        "calculate_leave_balance",
		Description: "Calculate the remaining leave balance for an employee",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"employee_id": map[string]any{"type": "string", "description": "The Employee ID"},
				"leave_type":  map[string]any{"type": "string", "description": "Type of leave (Annual, Sick, etc.)"},
			},
			"required": []any{"employee_id", "leave_type"},
		},
		AllowedRoles: []string{"hr_manager", "employee"}, // see PRD §24.3
		Handler:      calculateLeaveBalance,
	})
}

// calculateLeaveBalance returns max_days_per_year (from the matching LeaveType)
// minus the days consumed by Approved LeaveRequests for the employee and type.
func calculateLeaveBalance(ctx context.Context, args map[string]any) (any, error) {
	employeeID, _ := args["employee_id"].(string)
	leaveTypeName, _ := args["leave_type"].(string)
	if employeeID == "" || leaveTypeName == "" {
		return nil, orjerrors.Validation("employee_id and leave_type are required", nil)
	}
	if appSite == nil {
		return nil, orjerrors.Internal("leave balance requires a configured site", nil)
	}

	types, err := appSite.DocEngine.List(ctx, "LeaveType", document.ListOpts{
		Filters: map[string]any{"name": leaveTypeName},
	})
	if err != nil || len(types) == 0 {
		return nil, orjerrors.NotFound(fmt.Sprintf("leave type %q not found", leaveTypeName))
	}
	maxDays := asInt(types[0]["max_days_per_year"])

	requests, err := appSite.DocEngine.List(ctx, "LeaveRequest", document.ListOpts{
		Filters: map[string]any{
			"employee":       employeeID,
			"leave_type":     types[0]["id"],
			"workflow_state": "Approved",
		},
	})
	if err != nil {
		return nil, err
	}

	used := 0
	for _, req := range requests {
		from, ferr := parseAnyDate(req["from_date"])
		to, terr := parseAnyDate(req["to_date"])
		if ferr != nil || terr != nil {
			continue
		}
		used += int(to.Sub(from).Hours()/24) + 1
	}

	return map[string]any{
		"employee_id": employeeID,
		"leave_type":  leaveTypeName,
		"max_days":    maxDays,
		"used_days":   used,
		"remaining":   maxDays - used,
	}, nil
}

// asInt normalizes int-like scan values (the SQLite driver returns int64).
func asInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
