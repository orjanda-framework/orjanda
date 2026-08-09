// Package audit provides the immutable audit log: Entry, FieldChange, and
// Log.Write/Query. Every Document Engine and Workflow Engine write shares a
// single dal.Tx with its audit write — a failed audit write rolls back the
// data write (TAD §13.1).
//
// See TAD §13 and PRD §29.1 for the full specification.
// Implemented in Phase 4.
package audit
