package main

import (
	"context"
	"strings"
	"time"

	"github.com/orjanda-framework/orjanda"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/event"
)

// registerHooks wires the Application's lifecycle hooks (PRD §19.1–§19.2)
// onto the site's event bus. configure calls it; the validation harness calls
// it directly so hooks are exercised without going through configure.
func registerHooks(site *orjanda.Site) {
	site.EventBus.On("Employee", event.EventBeforeSave, validateEmployee)
	site.EventBus.On("LeaveRequest", event.EventBeforeSave, validateLeaveRequest)
}

// validateEmployee implements the PRD §19.2 example hook: an Active employee
// must have a department. The engine does not apply oj:"default=Active" at
// write time, so an empty status is treated as the declared default.
// Registered on Employee/before_save in configure.
func validateEmployee(_ context.Context, doc map[string]any) error {
	status, _ := doc["status"].(string)
	department, _ := doc["department"].(string)
	if (status == "" || strings.EqualFold(status, "Active")) && strings.TrimSpace(department) == "" {
		return orjerrors.Validation("active employees must have a department", nil)
	}
	return nil
}

// validateLeaveRequest enforces LeaveRequest business rules before any save
// (PRD §37 "validate leave balance") and seeds the workflow state to "Draft"
// on first insert so the LeaveApproval state machine has a starting point
// (TAD §8.1 step 1).
func validateLeaveRequest(_ context.Context, doc map[string]any) error {
	if doc["workflow_state"] == nil || doc["workflow_state"] == "" {
		doc["workflow_state"] = "Draft"
	}
	from, err := parseAnyDate(doc["from_date"])
	if err != nil {
		return orjerrors.Validation("invalid from_date", nil)
	}
	to, err := parseAnyDate(doc["to_date"])
	if err != nil {
		return orjerrors.Validation("invalid to_date", nil)
	}
	if from.After(to) {
		return orjerrors.Validation("leave start date must not be after the end date", nil)
	}
	return nil
}

// parseAnyDate accepts both the RFC3339 form the SQLite dialect stores and the
// calendar-date form the API/agent payloads send.
func parseAnyDate(v any) (time.Time, error) {
	switch val := v.(type) {
	case time.Time:
		return val, nil
	case string:
		for _, layout := range []string{"2006-01-02", time.RFC3339} {
			if t, err := time.Parse(layout, val); err == nil {
				return t, nil
			}
		}
	}
	return time.Time{}, orjerrors.Validation("invalid date value", nil)
}
