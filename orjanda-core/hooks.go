package core

import (
	"context"
	"strings"

	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/event"
)

// RegisterUserHooks installs the core User lifecycle hooks. The stored
// Password value is a bcrypt hash (TAD §4.1, PRD §15.1): a plaintext password
// written through the Document Engine is hashed on before_save so the built-in
// login endpoint (api/auth.go) can verify it. Values that are already bcrypt
// hashes pass through untouched, so updates that re-send a stored hash are
// idempotent.
//
// Call once per site with the site's EventBus. The hook is keyed by the
// docType name, so registration order relative to document registration does
// not matter.
func RegisterUserHooks(bus event.Bus) {
	bus.On("User", event.EventBeforeSave, func(ctx context.Context, doc map[string]any) error {
		raw, ok := doc["password"].(string)
		if !ok || raw == "" || isBcryptHash(raw) {
			return nil
		}
		hashed, err := auth.HashPassword(raw)
		if err != nil {
			return err
		}
		doc["password"] = hashed
		return nil
	})
}

// isBcryptHash reports whether s already looks like a bcrypt hash. All bcrypt
// variants produced by golang.org/x/crypto/bcrypt use a "$2[a|b|y]$" prefix.
func isBcryptHash(s string) bool {
	return strings.HasPrefix(s, "$2a$") || strings.HasPrefix(s, "$2b$") || strings.HasPrefix(s, "$2y$")
}
