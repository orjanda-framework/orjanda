// Package app defines the Application and Module system: app.Definition,
// app.Module, app.Dependency, dependency-DAG resolution, and the Installable/
// Upgradable/Uninstallable lifecycle interfaces resolved via Definition.Hooks
// (the TAD §7 "associated init type").
//
// See TAD §7 and PRD §11 for the full specification.
// Implemented in Phase 1.
package app
