// Package document implements the Document Engine: Create, Read, Update,
// Delete, and List operations driven by the compiled Registry schema, with
// field validation, lifecycle hooks, permission enforcement, and audit logging.
//
// See TAD §3.2 and PRD §12.2 for the full request lifecycle.
// Implemented in Phases 3 (bare CRUD) and 4 (full integration).
package document
