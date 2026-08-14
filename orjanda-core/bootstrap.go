package core

import (
	"context"
	"crypto/rand"
	"errors"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/oklog/ulid/v2"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
	modsqlite "modernc.org/sqlite"
)

const (
	AdminEmail = "admin@localhost"
	AdminRole  = "System Administrator"
)

// errAlreadyBootstrapped is returned by the bootstrap transaction when the
// User table is already populated; Bootstrap maps it back to a no-op success.
var errAlreadyBootstrapped = errors.New("system already bootstrapped")

// Bootstrap executes the first-run system administrator setup if no users exist.
// Returns (password, nil) if bootstrapped, or ("", nil) if already bootstrapped.
// Idempotent: does nothing on subsequent calls. The whole sequence runs inside
// a single transaction (REVIEW-2026-08-12 finding 12): the User-empty check and
// every insert share one write transaction, and the database enforces
// uniqueness on User.email / Role.role_name, so concurrent serve instances can
// never create duplicate admins. See TAD §4.2.
func Bootstrap(ctx context.Context, db dal.Database, reg schema.Registry) (string, error) {
	// Generate and hash the password before taking the write lock so the
	// bootstrap transaction stays as short as possible.
	password, err := generateRandomPassword(16)
	if err != nil {
		return "", err
	}
	hashedPassword, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}

	err = db.Transaction(ctx, func(tx dal.Tx) error {
		// 1. Already bootstrapped? Every check and write below shares this
		// transaction, so no interleaving instance can slip an admin in between
		// our check and our inserts.
		users, qErr := tx.Query(ctx, dal.Select{DocType: "User", Limit: 1})
		if qErr != nil {
			return qErr
		}
		if len(users) > 0 {
			return errAlreadyBootstrapped
		}

		now := time.Now()

		// 2. Create "System Administrator" Role if missing.
		var roleID string
		roles, qErr := tx.Query(ctx, dal.Select{
			DocType: "Role",
			Filters: map[string]any{"role_name": AdminRole},
			Limit:   1,
		})
		if qErr != nil {
			return qErr
		}
		if len(roles) > 0 {
			roleID, _ = roles[0]["id"].(string)
		} else {
			roleID = ulid.Make().String()
			if _, insErr := tx.Insert(ctx, "Role", map[string]any{
				"id":         roleID,
				"role_name":  AdminRole,
				"name":       AdminRole,
				"created_at": now,
				"updated_at": now,
				"deleted":    false,
			}); insErr != nil {
				return insErr
			}
		}

		// 3. Grant "System Administrator" permissions on all registered DocTypes.
		for _, doc := range reg.List() {
			existing, qErr := tx.Query(ctx, dal.Select{
				DocType: "RolePermission",
				Filters: map[string]any{
					"role":     AdminRole,
					"doc_type": doc.Name,
				},
				Limit: 1,
			})
			if qErr != nil {
				return qErr
			}
			if len(existing) > 0 {
				continue
			}
			if _, insErr := tx.Insert(ctx, "RolePermission", map[string]any{
				"id":         ulid.Make().String(),
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
			}); insErr != nil {
				return insErr
			}
		}

		// 4. Create admin@localhost user & UserRole link.
		adminUserID := ulid.Make().String()
		if _, uErr := tx.Insert(ctx, "User", map[string]any{
			"id":         adminUserID,
			"email":      AdminEmail,
			"full_name":  "System Administrator",
			"password":   hashedPassword,
			"active":     true,
			"created_at": now,
			"updated_at": now,
			"deleted":    false,
		}); uErr != nil {
			return uErr
		}
		if _, urErr := tx.Insert(ctx, "UserRole", map[string]any{
			"id":        ulid.Make().String(),
			"parent_id": adminUserID,
			"idx":       0,
			"role":      AdminRole,
		}); urErr != nil {
			return urErr
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, errAlreadyBootstrapped) || isUniqueViolation(err) {
			// Either already bootstrapped, or a concurrent instance won the
			// race and our duplicate insert hit the unique constraint.
			return "", nil
		}
		return "", orjerrors.Internal("bootstrap failed", err)
	}

	// The generated password must reach the operator exactly once, but never
	// the structured log stream (REVIEW-2026-08-12 finding 12). Callers print
	// it to stdout; slog only records that the account was created.
	slog.Info("bootstrapped system administrator account", "email", AdminEmail)
	return password, nil
}

// isUniqueViolation reports whether err is a unique-constraint violation from
// either supported driver (modernc SQLite or pgx/PostgreSQL). Bootstrap treats
// it as "a concurrent instance already bootstrapped" and returns a no-op.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		// SQLSTATE 23505 = unique_violation.
		return pgErr.Code == "23505"
	}
	var sqliteErr *modsqlite.Error
	if errors.As(err, &sqliteErr) {
		// SQLITE_CONSTRAINT_UNIQUE = 2067 (extended code of SQLITE_CONSTRAINT).
		return sqliteErr.Code() == 2067 ||
			(sqliteErr.Code() == 19 && strings.Contains(sqliteErr.Error(), "UNIQUE"))
	}
	return false
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
