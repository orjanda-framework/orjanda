// Package rpc dispatches POST /api/v1/method/{app}.{module}.{method} requests
// to registered api.MethodHandler implementations, enforcing AllowedRoles
// through the shared perm.Engine path (TAD §9.2).
//
// See PRD §14.3 and TAD §9.2 for the full specification.
// Implemented in Phase 6.
package rpc
