package errors

import (
	"fmt"
	"net/http"
)

// ErrorCode is a stable, machine-readable identifier for an error category.
// Every exported Orjanda function that can fail returns an errors.Error whose
// Code() is one of the six values below. No package may introduce additional
// codes for conditions these six already cover. See TAD §1.1.
type ErrorCode string

const (
	// CodeValidation maps to HTTP 400. Used for schema violations (required
	// fields, format checks, uniqueness conflicts that are caught before the
	// DB write), invalid query parameters, and malformed request payloads.
	CodeValidation ErrorCode = "VALIDATION_ERROR"

	// CodeAuth maps to HTTP 401. Used when the caller supplies a missing,
	// expired, or cryptographically invalid authentication credential.
	CodeAuth ErrorCode = "AUTH_ERROR"

	// CodePermission maps to HTTP 403. Used when the caller's identity is
	// recognised but lacks the required role or rule for the requested action.
	// Must be returned by perm.Engine before any DAL call is made (PRD §25.1).
	CodePermission ErrorCode = "PERMISSION_DENIED"

	// CodeNotFound maps to HTTP 404. Used when a requested Document ID or
	// DocType does not exist in the Registry or database.
	CodeNotFound ErrorCode = "NOT_FOUND"

	// CodeConflict maps to HTTP 409. Used for optimistic-locking failures,
	// duplicate unique-field violations caught at the DB layer, and invalid
	// workflow transitions from the current state (TAD §8.1 step 2).
	CodeConflict ErrorCode = "CONFLICT"

	// CodeInternal maps to HTTP 500. Used for unexpected infrastructure
	// failures: DB connectivity, internal invariant violations, etc.
	// The Message() must never expose raw system details to end-users or LLMs.
	CodeInternal ErrorCode = "INTERNAL_ERROR"
)

// HTTPStatus returns the HTTP status code that corresponds to c.
// This is the canonical mapping table used by the API layer (Phase 6) and
// documented in TAD §1.1.
func (c ErrorCode) HTTPStatus() int {
	switch c {
	case CodeValidation:
		return http.StatusBadRequest
	case CodeAuth:
		return http.StatusUnauthorized
	case CodePermission:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeConflict:
		return http.StatusConflict
	case CodeInternal:
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

// Error is the framework-wide error interface. Every exported Orjanda function
// that can fail must return a value implementing Error (or wrap one via
// Unwrap so that errors.As/Is traversal still reaches an Error). See TAD §1.1.
type Error interface {
	error // Error() string — human-readable, safe for logs.

	// Code returns the stable ErrorCode category.
	Code() ErrorCode

	// Message returns a human- and LLM-safe description of the failure.
	// It MUST NOT expose raw system internals (stack traces, SQL, DSNs).
	Message() string

	// Details returns structured supplementary information, e.g. per-field
	// validation failures: {"email": "invalid format", "name": "required"}.
	// May be nil when there is no additional context.
	Details() map[string]any

	// Unwrap returns the original underlying error for errors.As/Is chaining.
	// May be nil when this error was not wrapping another.
	Unwrap() error
}

// orjandaError is the concrete implementation of Error.
type orjandaError struct {
	code    ErrorCode
	message string
	details map[string]any
	cause   error
}

// Error satisfies the standard error interface. The string is safe to log but
// may include the cause's message for diagnostic purposes.
func (e *orjandaError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.code, e.message, e.cause)
	}
	return fmt.Sprintf("[%s] %s", e.code, e.message)
}

func (e *orjandaError) Code() ErrorCode        { return e.code }
func (e *orjandaError) Message() string         { return e.message }
func (e *orjandaError) Details() map[string]any { return e.details }
func (e *orjandaError) Unwrap() error            { return e.cause }

// New constructs a bare errors.Error with code, message, optional details map,
// and an optional underlying cause. Prefer the named constructors below.
func New(code ErrorCode, message string, details map[string]any, cause error) Error {
	return &orjandaError{
		code:    code,
		message: message,
		details: details,
		cause:   cause,
	}
}

// --- Named constructor helpers (one per ErrorCode) --------------------------
// Each constructor has a short, ergonomic signature for the most common use
// cases. Use New() directly when you need to supply both details and a cause.

// Validation returns a CodeValidation error. details may be nil.
func Validation(message string, details map[string]any) Error {
	return New(CodeValidation, message, details, nil)
}

// Auth returns a CodeAuth error.
func Auth(message string) Error {
	return New(CodeAuth, message, nil, nil)
}

// Permission returns a CodePermission error.
func Permission(message string) Error {
	return New(CodePermission, message, nil, nil)
}

// NotFound returns a CodeNotFound error.
func NotFound(message string) Error {
	return New(CodeNotFound, message, nil, nil)
}

// Conflict returns a CodeConflict error.
func Conflict(message string) Error {
	return New(CodeConflict, message, nil, nil)
}

// Internal returns a CodeInternal error, wrapping the underlying cause.
// The message MUST be a generic, safe string — do not include raw cause details.
func Internal(message string, cause error) Error {
	return New(CodeInternal, message, nil, cause)
}

// Wrap wraps an existing cause under a given code and message.
// If cause already implements Error, its Code is overridden by code.
func Wrap(code ErrorCode, message string, cause error) Error {
	return New(code, message, nil, cause)
}

// Is reports whether target matches e by ErrorCode. This enables
// errors.Is(err, errors.CodePermission) style usage via a sentinel.
func (e *orjandaError) Is(target error) bool {
	if t, ok := target.(*orjandaError); ok {
		return e.code == t.code
	}
	return false
}

// --- Sentinel values for errors.Is matching ---------------------------------
// Use these as the target in errors.Is rather than comparing Code() strings
// directly, which keeps call-sites clean:
//
//	if errors.Is(err, errors.ErrPermission) { ... }

var (
	ErrValidation = &orjandaError{code: CodeValidation}
	ErrAuth       = &orjandaError{code: CodeAuth}
	ErrPermission = &orjandaError{code: CodePermission}
	ErrNotFound   = &orjandaError{code: CodeNotFound}
	ErrConflict   = &orjandaError{code: CodeConflict}
	ErrInternal   = &orjandaError{code: CodeInternal}
)

// As reports whether err (or any wrapped error) satisfies the Error interface,
// and if so, sets target. This is a convenience wrapper so callers do not need
// to import the standard "errors" package just to call errors.As.
func As(err error, target *Error) bool {
	if err == nil {
		return false
	}
	if e, ok := err.(Error); ok {
		*target = e
		return true
	}
	// Walk the Unwrap chain.
	type unwrapper interface{ Unwrap() error }
	if u, ok := err.(unwrapper); ok {
		return As(u.Unwrap(), target)
	}
	return false
}
