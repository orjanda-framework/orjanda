package auth

import "context"

// Identity carries the authenticated caller's information through context.
// It is the sole mechanism by which the permission engine, document engine,
// and agent runtime know who is making a request. See TAD §1.2.
type Identity struct {
	// UserID is the ULID of the authenticated User record.
	UserID string
	// Email is the user's email address.
	Email string
	// FullName is the display name of the user.
	FullName string
	// Roles is the list of role names this user holds (e.g. "HR Manager").
	Roles []string
	// Tenant is the tenant identifier for post-MVP multi-tenancy (§15).
	// Always empty string in MVP (site.Config.MultiTenant = false).
	Tenant string
	// Source indicates authentication origin ("local", "oauth:google", etc.).
	Source string
}

// UserInfo holds the public profile of an authenticated user.
type UserInfo struct {
	UserID   string
	Email    string
	FullName string
	Roles    []string
	Tenant   string
	Source   string
}

// Provider is the extension point for authentication backends.
// The built-in JWT provider (Phase 5) is itself just the default implementation.
// See TAD §9.1.
type Provider interface {
	ValidateToken(ctx context.Context, token string) (*Identity, error)
	GetUserInfo(ctx context.Context, token string) (*UserInfo, error)
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

type contextKey int

const identityKey contextKey = iota

// NewContext returns a copy of ctx carrying id.
func NewContext(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, identityKey, id)
}

// FromContext extracts the Identity from ctx. Returns a zero Identity if none
// was injected — callers that require authentication must check UserID != "".
func FromContext(ctx context.Context) Identity {
	if id, ok := ctx.Value(identityKey).(Identity); ok {
		return id
	}
	return Identity{}
}
