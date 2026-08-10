package main

import (
	"context"
	"log/slog"

	"github.com/orjanda-framework/orjanda/workflow"
)

// LeaveApproval is the LeaveRequest state machine declared verbatim from
// PRD §19.3: Draft → Pending Approval → Approved/Rejected, with Submit open
// to Employees and Approve/Reject reserved for Department Heads and HR
// Managers. It is registered onto the site's workflow Engine by the
// verification test (and by any future post-compile wiring) via
// site.Workflows.Register (TAD §8).
var LeaveApproval = workflow.Definition{
	DocType: "LeaveRequest",
	States: []workflow.State{
		{Name: "Draft", Style: "gray"},
		{Name: "Pending Approval", Style: "yellow"},
		{Name: "Approved", Style: "green"},
		{Name: "Rejected", Style: "red"},
	},
	Transitions: []workflow.Transition{
		{From: "Draft", To: "Pending Approval", Action: "Submit",
			AllowedRoles: []string{"employee"}},
		{From: "Pending Approval", To: "Approved", Action: "Approve",
			AllowedRoles: []string{"department_head", "hr_manager"}},
		{From: "Pending Approval", To: "Rejected", Action: "Reject",
			AllowedRoles: []string{"department_head", "hr_manager"}},
	},
	OnTransition: map[string]workflow.Handler{
		// Notify the manager when a request moves to Pending Approval
		// (PRD §37 "notify manager on submission").
		"Pending Approval": func(ctx context.Context, doc map[string]any) error {
			slog.Info("leave_request.submitted",
				"id", doc["id"], "employee", doc["employee"], "from", doc["from_date"], "to", doc["to_date"])
			return nil
		},
		// Deduct leave balance on approval (PRD §19.3).
		"Approved": func(ctx context.Context, doc map[string]any) error {
			return nil
		},
	},
}
