// Package workflow implements the state-machine workflow engine:
// Definition, State, Transition, GuardFunc, and Engine.Execute — all
// enforced through the shared perm.Engine (no bespoke permission path).
//
// See TAD §8 and PRD §19.3 for the full specification.
// Implemented in Phase 4.
package workflow
