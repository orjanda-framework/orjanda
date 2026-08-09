// Package errors defines the framework-wide error model: ErrorCode enum,
// the Error interface, constructor helpers, and HTTP status mapping.
//
// Every exported function in the orjanda framework that can fail must return
// a value implementing errors.Error (or wrap one via Unwrap()).
//
// See TAD §1.1 for the full contract.
package errors
