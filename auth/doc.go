// Package auth defines the Identity and UserInfo types, the auth.Provider
// interface, and the default JWT-based implementation (bcrypt passwords,
// 15-minute access tokens, 7-day rotating refresh tokens).
//
// Identity is propagated exclusively through context.Context via
// auth.FromContext — never through globals or extra parameters (TAD §1.2).
//
// See TAD §9.1 and PRD §15 for the full specification.
// Implemented in Phase 5.
package auth
