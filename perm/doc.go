// Package perm implements the permission engine: RBAC document-level checks
// from DocPermission metadata, ABAC via registered perm.Rules, and field-level
// filtering via FilterRead/FilterWrite.
//
// Every read/write path — REST, RPC, agent tool, workflow transition — must
// call through perm.Engine. A hand-rolled check outside it is a defect.
//
// See TAD §2.4, §2.7, §9.1 and PRD §16 for the full specification.
// Implemented in Phase 4.
package perm
