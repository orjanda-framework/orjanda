// Package background defines the background.Job and background.Queue
// interfaces plus an in-memory, non-durable stub Queue for the MVP.
//
// Background job durability is explicitly post-MVP (PRD §44.3). The interface
// is defined now so Application authors can code against a stable contract.
//
// See TAD §9.1 for the full specification.
// Implemented in Phase 2.
package background
