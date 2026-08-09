// Package middleware contains the Chi middleware stack applied to every
// incoming HTTP request: CORS, Auth (JWT extraction), Rate Limit, and
// Permission (perm.Engine.CheckAction).
//
// See PRD §12.2 and TAD §3.2 for the ordering specification.
// Implemented in Phase 6.
package middleware
