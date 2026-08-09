// Package safety implements SafetyPolicy and SafetyLayer: the five-step
// approval-evaluation order (Always → Bulk → RoleOverride → RequireApproval →
// AutoApprove), rate limiting, token budgets, and tool allowlists.
//
// Delete operations are AlwaysRequireApproval and cannot be overridden by
// any configuration (TAD §12.1 step 1, PRD §28.1).
//
// See TAD §12 and PRD §28 for the full specification.
// Implemented in Phase 8.
package safety
