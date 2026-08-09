// Package search exposes the search.Backend interface and a default adapter
// that delegates to the active dal.Dialect's FullTextSearch — no external
// search process is required for the MVP.
//
// See TAD §9.1 and PRD §13.3 for the full specification.
// Implemented in Phase 2.
package search
