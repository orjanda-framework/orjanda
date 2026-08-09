package core

import (
	"context"
	"crypto/rand"
	"log/slog"
	"math/big"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
)

const (
	AdminEmail = "admin@localhost"
	AdminRole  = "System Administrator"
)

// Bootstrap executes the first-run system administrator setup if no users exist.
// Returns (password, nil) if bootstrapped, or ("", nil) if already bootstrapped.
// Idempotent: does nothing on subsequent calls. See TAD §4.2.
func Bootstrap(ctx context.Context, db dal.Database, reg schema.Registry) (string, error) {
	// 1. Check if any users exist
	users, err := db.Query(ctx, dal.Select{
		DocType: "User",
		Limit:   1,
	})
	if err == nil && len(users) > 0 {
		// System already bootstrapped
		return "", nil
	}

	// 2. Create "System Administrator" Role if missing
	roles, err := db.Query(ctx, dal.Select{
		DocType: "Role",
		Filters: map[string]any{"role_name": AdminRole},
		Limit:   1,
	})
	var roleID string
	if err == nil && len(roles) > 0 {
		roleID, _ = roles[0]["id"].(string)
	} else {
		roleID = ulid.Make().String()
		now := time.Now()
		err := db.Transaction(ctx, func(tx dal.Tx) error {
			_, err := tx.Insert(ctx, "Role", map[string]any{
				"id":         roleID,
				"role_name":  AdminRole,
				"name":       AdminRole,
				"created_at": now,
				"updated_at": now,
				"deleted":    false,
			})
			return err
		})
		if err != nil {
			return "", orjerrors.Internal("failed to create admin role", err)
		}
	}

	// 3. Grant "System Administrator" permissions on all registered DocTypes
	compiledDocs := reg.List()
	now := time.Now()
	err = db.Transaction(ctx, func(tx dal.Tx) error {
		for _, doc := range compiledDocs {
			existing, qErr := db.Query(ctx, dal.Select{
				DocType: "RolePermission",
				Filters: map[string]any{
					"role":     AdminRole,
					"doc_type": doc.Name,
				},
				Limit: 1,
			})
			if qErr == nil && len(existing) > 0 {
				continue
			}

			rpID := ulid.Make().String()
			_, insErr := tx.Insert(ctx, "RolePermission", map[string]any{
				"id":         rpID,
				"role":       AdminRole,
				"doc_type":   doc.Name,
				"read":       true,
				"write":      true,
				"create":     true,
				"delete":     true,
				"submit":     true,
				"created_at": now,
				"updated_at": now,
				"deleted":    false,
			})
			if insErr != nil {
				return insErr
			}
		}
		return nil
	})
	if err != nil {
		return "", orjerrors.Internal("failed to grant admin permissions", err)
	}

	// 4. Generate random password
	password, err := generateRandomPassword(16)
	if err != nil {
		return "", err
	}

	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}

	// 5. Create admin@localhost user & UserRole link
	adminUserID := ulid.Make().String()
	userRoleID := ulid.Make().String()

	err = db.Transaction(ctx, func(tx dal.Tx) error {
		_, uErr := tx.Insert(ctx, "User", map[string]any{
			"id":         adminUserID,
			"email":      AdminEmail,
			"full_name":  "System Administrator",
			"password":   hashedPassword,
			"active":     true,
			"created_at": now,
			"updated_at": now,
			"deleted":    false,
		})
		if uErr != nil {
			return uErr
		}

		_, urErr := tx.Insert(ctx, "UserRole", map[string]any{
			"id":        userRoleID,
			"parent_id": adminUserID,
			"idx":       0,
			"role":      AdminRole,
		})
		return urErr
	})
	if err != nil {
		return "", orjerrors.Internal("failed to create admin user", err)
	}

	slog.Info("bootstrapped system administrator account", "email", AdminEmail, "password", password)
	return password, nil
}

func generateRandomPassword(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	res := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", orjerrors.Internal("failed to generate random password", err)
		}
		res[i] = chars[num.Int64()]
	}
	return string(res), nil
}
