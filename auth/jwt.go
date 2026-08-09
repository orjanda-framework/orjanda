package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/oklog/ulid/v2"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"golang.org/x/crypto/bcrypt"
)

// JWTClaims defines the claims stored inside Orjanda JWT tokens.
type JWTClaims struct {
	jwt.RegisteredClaims
	Email    string   `json:"email,omitempty"`
	FullName string   `json:"full_name,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Tenant   string   `json:"tenant,omitempty"`
	Source   string   `json:"source,omitempty"`
	Type     string   `json:"type"` // "access" or "refresh"
}

// JWTProvider implements Provider using JWT tokens and bcrypt password hashing.
// See PRD §15.1 and TAD §9.1.
type JWTProvider struct {
	secretKey     []byte
	accessTTL     time.Duration
	refreshTTL    time.Duration
	mu            sync.RWMutex
	revokedTokens map[string]bool // map of revoked refresh token JTIs
}

// NewJWTProvider initializes a JWTProvider with secret key and TTLs.
// Defaults: accessTTL = 15m, refreshTTL = 7 days.
func NewJWTProvider(secretKey []byte, accessTTL, refreshTTL time.Duration) *JWTProvider {
	if accessTTL <= 0 {
		accessTTL = 15 * time.Minute
	}
	if refreshTTL <= 0 {
		refreshTTL = 7 * 24 * time.Hour
	}
	return &JWTProvider{
		secretKey:     secretKey,
		accessTTL:     accessTTL,
		refreshTTL:    refreshTTL,
		revokedTokens: make(map[string]bool),
	}
}

// HashPassword generates a bcrypt hash of the given plain-text password.
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", orjerrors.Internal("failed to hash password", err)
	}
	return string(bytes), nil
}

// CheckPassword compares a bcrypt hashed password with a plain-text candidate.
func CheckPassword(hashedPassword, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	return err == nil
}

// GenerateTokenPair issues a new access token (15m) and refresh token (7 days) for id.
func (p *JWTProvider) GenerateTokenPair(id Identity) (accessToken string, refreshToken string, err error) {
	now := time.Now()

	// 1. Access Token
	accessClaims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(p.accessTTL)),
			ID:        ulid.Make().String(),
		},
		Email:    id.Email,
		FullName: id.FullName,
		Roles:    id.Roles,
		Tenant:   id.Tenant,
		Source:   id.Source,
		Type:     "access",
	}

	accTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessToken, err = accTokenObj.SignedString(p.secretKey)
	if err != nil {
		return "", "", orjerrors.Internal("failed to sign access token", err)
	}

	// 2. Refresh Token
	refreshJTI := ulid.Make().String()
	refreshClaims := JWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   id.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(p.refreshTTL)),
			ID:        refreshJTI,
		},
		Email:    id.Email,
		FullName: id.FullName,
		Roles:    id.Roles,
		Tenant:   id.Tenant,
		Source:   id.Source,
		Type:     "refresh",
	}

	refTokenObj := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshToken, err = refTokenObj.SignedString(p.secretKey)
	if err != nil {
		return "", "", orjerrors.Internal("failed to sign refresh token", err)
	}

	return accessToken, refreshToken, nil
}

// ValidateToken parses and validates a JWT token (access token).
func (p *JWTProvider) ValidateToken(ctx context.Context, tokenString string) (*Identity, error) {
	claims, err := p.parseClaims(tokenString)
	if err != nil {
		return nil, err
	}

	if claims.Type != "access" {
		return nil, orjerrors.Auth("token is not an access token")
	}

	return &Identity{
		UserID:   claims.Subject,
		Email:    claims.Email,
		FullName: claims.FullName,
		Roles:    claims.Roles,
		Tenant:   claims.Tenant,
		Source:   claims.Source,
	}, nil
}

// GetUserInfo retrieves UserInfo from a valid access token.
func (p *JWTProvider) GetUserInfo(ctx context.Context, tokenString string) (*UserInfo, error) {
	id, err := p.ValidateToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	return &UserInfo{
		UserID:   id.UserID,
		Email:    id.Email,
		FullName: id.FullName,
		Roles:    id.Roles,
		Tenant:   id.Tenant,
		Source:   id.Source,
	}, nil
}

// RefreshToken validates the refresh token, revokes it, and issues a rotated pair.
// Reused or revoked refresh tokens are rejected.
func (p *JWTProvider) RefreshToken(ctx context.Context, refreshTokenString string) (newAccess, newRefresh string, err error) {
	claims, err := p.parseClaims(refreshTokenString)
	if err != nil {
		return "", "", err
	}

	if claims.Type != "refresh" {
		return "", "", orjerrors.Auth("token is not a refresh token")
	}

	jti := claims.ID
	if jti == "" {
		return "", "", orjerrors.Auth("refresh token missing JTI")
	}

	p.mu.Lock()
	if p.revokedTokens[jti] {
		p.mu.Unlock()
		return "", "", orjerrors.Auth("refresh token has been revoked or reused")
	}
	// Revoke this refresh token (rotation)
	p.revokedTokens[jti] = true
	p.mu.Unlock()

	id := Identity{
		UserID:   claims.Subject,
		Email:    claims.Email,
		FullName: claims.FullName,
		Roles:    claims.Roles,
		Tenant:   claims.Tenant,
		Source:   claims.Source,
	}

	return p.GenerateTokenPair(id)
}

func (p *JWTProvider) parseClaims(tokenString string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return p.secretKey, nil
	})

	if err != nil || !token.Valid {
		return nil, orjerrors.Auth("invalid or expired token")
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return nil, orjerrors.Auth("invalid token claims")
	}

	return claims, nil
}
