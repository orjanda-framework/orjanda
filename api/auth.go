package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/orjanda-framework/orjanda/api/render"
	"github.com/orjanda-framework/orjanda/auth"
	"github.com/orjanda-framework/orjanda/dal"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/orjanda-framework/orjanda/schema"
	"github.com/orjanda-framework/orjanda/ui"
)

// tokenIssuer is the capability the built-in JWT provider satisfies. It is a
// narrower slice of auth.Provider's functionality needed to issue/rotate
// tokens; external identity providers (TAD §9.1) may instead leave the login
// routes unmounted and authenticate elsewhere.
type tokenIssuer interface {
	GenerateTokenPair(id auth.Identity) (accessToken, refreshToken string, err error)
	RefreshToken(ctx context.Context, refreshToken string) (newAccess, newRefresh string, err error)
}

// AuthHandler serves the built-in email/password login flow (PRD §15.1).
// Credential verification reads the User/Role tables directly through the DAL
// — deliberately bypassing the Document Engine, whose permission checks would
// deny an as-yet-unauthenticated caller (PRD §25.1 applies to already-known
// identities; login is the one path that must mint an identity from a secret).
type AuthHandler struct {
	db      dal.Database
	reg     schema.Registry
	issuer  tokenIssuer
	enabled bool
}

// NewAuthHandler builds the login/refresh handler. enabled is false when the
// configured auth.Provider cannot issue tokens, in which case the routes are
// not mounted.
func NewAuthHandler(db dal.Database, reg schema.Registry, p auth.Provider) *AuthHandler {
	issuer, ok := p.(tokenIssuer)
	if !ok {
		return &AuthHandler{}
	}
	return &AuthHandler{db: db, reg: reg, issuer: issuer, enabled: ok && db != nil && reg != nil}
}

// Login handles POST /api/v1/auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if !h.enabled {
		render.RespondError(w, orjerrors.Auth("authentication is not enabled on this provider"))
		return
	}

	var body struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.RespondError(w, orjerrors.Validation("invalid JSON request body", nil))
		return
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || body.Password == "" {
		render.RespondError(w, orjerrors.Auth("email and password are required"))
		return
	}

	rows, err := h.db.Query(r.Context(), dal.Select{
		DocType: "User",
		Filters: map[string]any{"email": body.Email},
		Limit:   1,
	})
	if err != nil || len(rows) == 0 {
		slog.Warn("auth.login.denied", "email", body.Email, "reason", "no_such_user")
		render.RespondError(w, orjerrors.Auth("invalid credentials"))
		return
	}
	row := rows[0]

	if !asBool(row["active"]) {
		slog.Warn("auth.login.denied", "email", body.Email, "reason", "inactive")
		render.RespondError(w, orjerrors.Auth("account is disabled"))
		return
	}
	hashed, _ := row["password"].(string)
	if hashed == "" || !auth.CheckPassword(hashed, body.Password) {
		slog.Warn("auth.login.denied", "email", body.Email, "reason", "bad_password")
		render.RespondError(w, orjerrors.Auth("invalid credentials"))
		return
	}

	userID, _ := row["id"].(string)
	fullName, _ := row["full_name"].(string)
	id := auth.Identity{
		UserID:   userID,
		Email:    body.Email,
		FullName: fullName,
		Roles:    h.rolesForUser(r.Context(), userID),
		Source:   "local",
	}

	access, refresh, err := h.issuer.GenerateTokenPair(id)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	render.RespondJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "bearer",
		"refresh_token": refresh,
		"expires_in":    900, // seconds; the built-in provider default (PRD §15.1)
	}, nil)
}

// Refresh handles POST /api/v1/auth/refresh, rotating a refresh token.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	if !h.enabled {
		render.RespondError(w, orjerrors.Auth("authentication is not enabled on this provider"))
		return
	}

	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		render.RespondError(w, orjerrors.Validation("invalid JSON request body", nil))
		return
	}
	if body.RefreshToken == "" {
		render.RespondError(w, orjerrors.Auth("refresh_token is required"))
		return
	}

	access, refresh, err := h.issuer.RefreshToken(r.Context(), body.RefreshToken)
	if err != nil {
		render.RespondError(w, err)
		return
	}

	render.RespondJSON(w, http.StatusOK, map[string]any{
		"access_token":  access,
		"token_type":    "bearer",
		"refresh_token": refresh,
		"expires_in":    900,
	}, nil)
}

// asBool coerces the scalar a sqlite/postgres driver returns for a boolean
// column (bool, int64 0/1, or "true"/"1") into a Go bool.
func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	case float64:
		return t != 0
	case string:
		return t == "true" || t == "1" || t == "1.0"
	}
	return false
}

// rolesForUser resolves the role NAMEs a user holds by reading the UserRole
// child rows (bootstrap stores role names in the link column, TAD §4.1).
func (h *AuthHandler) rolesForUser(ctx context.Context, userID string) []string {
	rows, err := h.db.Query(ctx, dal.Select{
		DocType: "UserRole",
		Filters: map[string]any{"parent_id": userID},
	})
	if err != nil {
		slog.Warn("auth.roles", "error", err)
		return nil
	}
	roles := make([]string, 0, len(rows))
	for _, row := range rows {
		if r, _ := row["role"].(string); r != "" {
			roles = append(roles, r)
		}
	}
	return roles
}

// ---------------------------------------------------------------------------
// /api/v1/meta/pages — custom ui.Page registrations for the sidebar (PRD §18.3)
// ---------------------------------------------------------------------------

// PagesHandler serves the registered ui.Pages as JSON for the Admin UI shell.
type PagesHandler struct {
	reg ui.Registry
}

// NewPagesHandler keeps a reference to the ui.Registry so late registrations
// (e.g. an Application registering pages after route assembly) are visible.
func NewPagesHandler(reg ui.Registry) *PagesHandler {
	return &PagesHandler{reg: reg}
}

// List handles GET /api/v1/meta/pages.
func (h *PagesHandler) List(w http.ResponseWriter, r *http.Request) {
	var pages []ui.Page
	if h.reg != nil {
		pages = h.reg.Pages()
	}
	render.RespondJSON(w, http.StatusOK, pages, nil)
}
