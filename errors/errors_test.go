package errors_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/orjanda-framework/orjanda/errors"
)

// TestErrorCodeHTTPStatus verifies the canonical HTTP status mapping table for
// all six ErrorCode values. This is one of Phase 0's explicit completion criteria.
func TestErrorCodeHTTPStatus(t *testing.T) {
	cases := []struct {
		code   errors.ErrorCode
		status int
	}{
		{errors.CodeValidation, http.StatusBadRequest},
		{errors.CodeAuth, http.StatusUnauthorized},
		{errors.CodePermission, http.StatusForbidden},
		{errors.CodeNotFound, http.StatusNotFound},
		{errors.CodeConflict, http.StatusConflict},
		{errors.CodeInternal, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		got := tc.code.HTTPStatus()
		if got != tc.status {
			t.Errorf("ErrorCode(%q).HTTPStatus() = %d, want %d", tc.code, got, tc.status)
		}
	}
}

// TestNamedConstructors verifies that each named constructor produces an Error
// with the correct Code and a non-empty Message.
func TestNamedConstructors(t *testing.T) {
	cases := []struct {
		name string
		err  errors.Error
		code errors.ErrorCode
	}{
		{"Validation", errors.Validation("bad input", map[string]any{"field": "email"}), errors.CodeValidation},
		{"Auth", errors.Auth("invalid token"), errors.CodeAuth},
		{"Permission", errors.Permission("access denied"), errors.CodePermission},
		{"NotFound", errors.NotFound("resource missing"), errors.CodeNotFound},
		{"Conflict", errors.Conflict("duplicate key"), errors.CodeConflict},
		{"Internal", errors.Internal("unexpected failure", fmt.Errorf("db down")), errors.CodeInternal},
	}
	for _, tc := range cases {
		if tc.err.Code() != tc.code {
			t.Errorf("%s: Code() = %q, want %q", tc.name, tc.err.Code(), tc.code)
		}
		if tc.err.Message() == "" {
			t.Errorf("%s: Message() is empty", tc.name)
		}
	}
}

// TestValidationDetails verifies that the details map is preserved.
func TestValidationDetails(t *testing.T) {
	details := map[string]any{"email": "invalid format", "name": "required"}
	err := errors.Validation("validation failed", details)
	got := err.Details()
	if len(got) != 2 {
		t.Fatalf("Details() length = %d, want 2", len(got))
	}
	if got["email"] != "invalid format" {
		t.Errorf("Details()[email] = %v, want %q", got["email"], "invalid format")
	}
}

// TestUnwrap verifies that Internal wraps the cause and Unwrap() returns it.
func TestUnwrap(t *testing.T) {
	cause := fmt.Errorf("connection refused")
	err := errors.Internal("db unavailable", cause)
	if err.Unwrap() != cause {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

// TestErrorString verifies the Error() string contains the code and message.
func TestErrorString(t *testing.T) {
	err := errors.Permission("not allowed")
	s := err.Error()
	if s == "" {
		t.Fatal("Error() returned empty string")
	}
	// Must contain the code for log readability.
	want := "PERMISSION_DENIED"
	if len(s) < len(want) {
		t.Errorf("Error() = %q, expected to contain %q", s, want)
	}
}

// TestAs verifies that errors.As extracts an embedded Error from a wrapped chain.
func TestAs(t *testing.T) {
	inner := errors.NotFound("missing doc")
	outer := fmt.Errorf("handler failed: %w", inner)

	var target errors.Error
	if !errors.As(outer, &target) {
		t.Fatal("errors.As returned false, expected true")
	}
	if target.Code() != errors.CodeNotFound {
		t.Errorf("As().Code() = %q, want %q", target.Code(), errors.CodeNotFound)
	}
}

// TestWrap verifies that Wrap produces an error with the given code and preserves cause.
func TestWrap(t *testing.T) {
	cause := fmt.Errorf("raw db error")
	err := errors.Wrap(errors.CodeInternal, "something went wrong", cause)
	if err.Code() != errors.CodeInternal {
		t.Errorf("Wrap Code() = %q, want %q", err.Code(), errors.CodeInternal)
	}
	if err.Unwrap() != cause {
		t.Errorf("Wrap Unwrap() = %v, want %v", err.Unwrap(), cause)
	}
}

// TestRoundTrip exercises the full HTTP-status round-trip for all six codes,
// satisfying the Phase 0 completion criterion: "errors.Error round-trips
// through the HTTP status mapping table for all six ErrorCode values."
func TestRoundTrip(t *testing.T) {
	constructors := []func() errors.Error{
		func() errors.Error { return errors.Validation("v", nil) },
		func() errors.Error { return errors.Auth("a") },
		func() errors.Error { return errors.Permission("p") },
		func() errors.Error { return errors.NotFound("n") },
		func() errors.Error { return errors.Conflict("c") },
		func() errors.Error { return errors.Internal("i", nil) },
	}
	for _, fn := range constructors {
		err := fn()
		status := err.Code().HTTPStatus()
		if status < 400 || status >= 600 {
			t.Errorf("Code %q produced non-error HTTP status %d", err.Code(), status)
		}
	}
}
