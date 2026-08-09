// Package api implements the HTTP API surface on Chi: REST CRUD handlers,
// RPC method dispatch, the Metadata API, and the middleware chain
// (CORS → Auth → RateLimit → Permission → Handler).
//
// See TAD §3.2, §9.2 and PRD §14 for the full specification.
// Implemented in Phase 6.
package api
