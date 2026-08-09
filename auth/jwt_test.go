package auth_test

import (
	"context"
	"testing"
	"time"

	"github.com/orjanda-framework/orjanda/auth"
	orjerrors "github.com/orjanda-framework/orjanda/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret-key-for-phase-5")

func makeProvider() *auth.JWTProvider {
	return auth.NewJWTProvider(testSecret, 15*time.Minute, 7*24*time.Hour)
}

func testIdentity() auth.Identity {
	return auth.Identity{
		UserID:   "01JZAAABBBCCCDDDEEEFFFGGG",
		Email:    "test@example.com",
		FullName: "Test User",
		Roles:    []string{"HR Manager"},
		Tenant:   "",
		Source:   "local",
	}
}

// ---------------------------------------------------------------------------
// Token generation & validation
// ---------------------------------------------------------------------------

func TestJWTProvider_GenerateAndValidate_Access(t *testing.T) {
	p := makeProvider()
	id := testIdentity()

	accessToken, _, err := p.GenerateTokenPair(id)
	require.NoError(t, err)
	require.NotEmpty(t, accessToken)

	ctx := context.Background()
	got, err := p.ValidateToken(ctx, accessToken)
	require.NoError(t, err)

	assert.Equal(t, id.UserID, got.UserID)
	assert.Equal(t, id.Email, got.Email)
	assert.Equal(t, id.FullName, got.FullName)
	assert.Equal(t, id.Roles, got.Roles)
	assert.Equal(t, id.Source, got.Source)
}

func TestJWTProvider_ValidateToken_RefreshIsRejected(t *testing.T) {
	p := makeProvider()
	_, refreshToken, err := p.GenerateTokenPair(testIdentity())
	require.NoError(t, err)

	ctx := context.Background()
	_, err = p.ValidateToken(ctx, refreshToken)
	require.Error(t, err)

	var orjErr orjerrors.Error
	require.True(t, orjerrors.As(err, &orjErr))
	assert.Equal(t, orjerrors.CodeAuth, orjErr.Code())
}

func TestJWTProvider_ValidateToken_InvalidToken(t *testing.T) {
	p := makeProvider()
	ctx := context.Background()
	_, err := p.ValidateToken(ctx, "not-a-valid-token")
	require.Error(t, err)

	var orjErr orjerrors.Error
	require.True(t, orjerrors.As(err, &orjErr))
	assert.Equal(t, orjerrors.CodeAuth, orjErr.Code())
}

func TestJWTProvider_ValidateToken_WrongSecret(t *testing.T) {
	p1 := auth.NewJWTProvider([]byte("secret-a"), 0, 0)
	p2 := auth.NewJWTProvider([]byte("secret-b"), 0, 0)

	accessToken, _, err := p1.GenerateTokenPair(testIdentity())
	require.NoError(t, err)

	ctx := context.Background()
	_, err = p2.ValidateToken(ctx, accessToken)
	require.Error(t, err)

	var orjErr orjerrors.Error
	require.True(t, orjerrors.As(err, &orjErr))
	assert.Equal(t, orjerrors.CodeAuth, orjErr.Code())
}

// ---------------------------------------------------------------------------
// GetUserInfo
// ---------------------------------------------------------------------------

func TestJWTProvider_GetUserInfo(t *testing.T) {
	p := makeProvider()
	id := testIdentity()

	accessToken, _, err := p.GenerateTokenPair(id)
	require.NoError(t, err)

	ctx := context.Background()
	info, err := p.GetUserInfo(ctx, accessToken)
	require.NoError(t, err)

	assert.Equal(t, id.UserID, info.UserID)
	assert.Equal(t, id.Email, info.Email)
	assert.Equal(t, id.FullName, info.FullName)
	assert.Equal(t, id.Roles, info.Roles)
}

// ---------------------------------------------------------------------------
// Token rotation
// ---------------------------------------------------------------------------

func TestJWTProvider_RefreshToken_IssuesnewPair(t *testing.T) {
	p := makeProvider()
	id := testIdentity()

	_, refreshToken, err := p.GenerateTokenPair(id)
	require.NoError(t, err)

	ctx := context.Background()
	newAccess, newRefresh, err := p.RefreshToken(ctx, refreshToken)
	require.NoError(t, err)
	require.NotEmpty(t, newAccess)
	require.NotEmpty(t, newRefresh)

	// New access token must be valid
	got, err := p.ValidateToken(ctx, newAccess)
	require.NoError(t, err)
	assert.Equal(t, id.UserID, got.UserID)
}

func TestJWTProvider_RefreshToken_OldTokenRevokedAfterRotation(t *testing.T) {
	p := makeProvider()
	_, refreshToken, err := p.GenerateTokenPair(testIdentity())
	require.NoError(t, err)

	ctx := context.Background()
	// First refresh – should succeed
	_, _, err = p.RefreshToken(ctx, refreshToken)
	require.NoError(t, err)

	// Reusing the same refresh token – must fail (token revoked)
	_, _, err = p.RefreshToken(ctx, refreshToken)
	require.Error(t, err)

	var orjErr orjerrors.Error
	require.True(t, orjerrors.As(err, &orjErr))
	assert.Equal(t, orjerrors.CodeAuth, orjErr.Code())
}

func TestJWTProvider_RefreshToken_AccessTokenRejected(t *testing.T) {
	p := makeProvider()
	accessToken, _, err := p.GenerateTokenPair(testIdentity())
	require.NoError(t, err)

	ctx := context.Background()
	_, _, err = p.RefreshToken(ctx, accessToken)
	require.Error(t, err)

	var orjErr orjerrors.Error
	require.True(t, orjerrors.As(err, &orjErr))
	assert.Equal(t, orjerrors.CodeAuth, orjErr.Code())
}

// ---------------------------------------------------------------------------
// Password hashing
// ---------------------------------------------------------------------------

func TestHashPassword_CheckPassword(t *testing.T) {
	plain := "super-secret-password-123!"

	hash, err := auth.HashPassword(plain)
	require.NoError(t, err)
	require.NotEmpty(t, hash)
	assert.NotEqual(t, plain, hash)

	assert.True(t, auth.CheckPassword(hash, plain))
	assert.False(t, auth.CheckPassword(hash, "wrong-password"))
}

// ---------------------------------------------------------------------------
// Context helpers
// ---------------------------------------------------------------------------

func TestIdentityContext_RoundTrip(t *testing.T) {
	id := testIdentity()
	ctx := auth.NewContext(context.Background(), id)

	got := auth.FromContext(ctx)
	assert.Equal(t, id.UserID, got.UserID)
	assert.Equal(t, id.Email, got.Email)
	assert.Equal(t, id.Roles, got.Roles)
}

func TestFromContext_EmptyContext_ReturnsZero(t *testing.T) {
	got := auth.FromContext(context.Background())
	assert.Empty(t, got.UserID)
	assert.Nil(t, got.Roles)
}

// ---------------------------------------------------------------------------
// auth.Provider interface substitution
// ---------------------------------------------------------------------------

// stubProvider is an alternative auth.Provider implementation, proving the
// interface is swappable without affecting downstream engine logic.
type stubProvider struct{}

func (s *stubProvider) ValidateToken(_ context.Context, token string) (*auth.Identity, error) {
	if token == "valid" {
		return &auth.Identity{UserID: "stub-user", Roles: []string{"Admin"}}, nil
	}
	return nil, orjerrors.Auth("stub: invalid token")
}

func (s *stubProvider) GetUserInfo(_ context.Context, token string) (*auth.UserInfo, error) {
	if token == "valid" {
		return &auth.UserInfo{UserID: "stub-user"}, nil
	}
	return nil, orjerrors.Auth("stub: invalid token")
}

func TestAuthProvider_InterfaceSubstitution(t *testing.T) {
	// Compile-time proof that stubProvider satisfies auth.Provider
	var _ auth.Provider = (*stubProvider)(nil)

	// Runtime verification
	ctx := context.Background()
	var p auth.Provider = &stubProvider{}

	id, err := p.ValidateToken(ctx, "valid")
	require.NoError(t, err)
	assert.Equal(t, "stub-user", id.UserID)

	_, err = p.ValidateToken(ctx, "bad-token")
	require.Error(t, err)
}
