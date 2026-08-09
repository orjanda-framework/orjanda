// Package planner defines the Plan/PlanStep structured-output contract and
// the whole-plan pre-execution validation logic used in Plan-and-Execute mode.
// An invalid plan is rejected wholesale before any step executes (TAD §11.3).
//
// See TAD §11.2–§11.3 and PRD §27 for the full specification.
// Implemented in Phase 8.
package planner
