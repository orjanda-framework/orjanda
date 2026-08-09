// Package tools implements the ToolRegistry: Compile (run once after
// Registry.Compile) and ForIdentity (run per agent turn) following the
// deterministic O(len(CompiledDocs)) generation algorithm in TAD §10.
//
// See TAD §10 and PRD §24 for the full specification.
// Implemented in Phase 7.
package tools
