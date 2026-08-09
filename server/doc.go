// Package server is the HTTP assembly root: it wires the Registry, Database,
// permission engine, event bus, cache, and Admin UI into the orjanda.Site
// composition root and starts the Chi HTTP server.
//
// See TAD §12.4 and PRD §12.1 for the full specification.
// Implemented in Phase 6 (API wiring) and Phase 9 (UI embedding).
package server
