// Package testing (imported as orjanda/testing) provides the first-class
// test harness: NewTestSite, WithApps, WithDialect, CreateUser, WithUser,
// SeedFixtures, MockLLM, ToolCall, TextResponse, and ApprovalPrompt.
//
// NewTestSite provisions a fully-wired site (Registry compiled, tables
// created, engines attached, no HTTP server) with a fresh in-memory SQLite
// database per test, or a testcontainers-go PostgreSQL instance under the
// "integration" build tag (WithDialect("postgres")). TestSite exposes the
// real Document Engine as Document and an Agent Runtime as Agent, so the
// PRD §32.2–§32.3 patterns read verbatim; per-turn LLM providers come from
// agent.WithProvider(…).
//
// See TAD §17 and PRD §32 for the full specification.
package testing
