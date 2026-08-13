package gateway_test

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stock-anomaly-detection/go-api/internal/domain/auth"
	"github.com/stock-anomaly-detection/go-api/internal/interface/gateway"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJWTTokenService_IssueAndVerify(t *testing.T) {
	svc := gateway.NewJWTTokenService("test-secret")

	token, err := svc.IssueToken("user-123")
	require.NoError(t, err)
	assert.NotEmpty(t, token)

	userID, err := svc.VerifyToken(token)
	require.NoError(t, err)
	assert.Equal(t, "user-123", userID)
}

func TestJWTTokenService_VerifyToken_WrongSecret(t *testing.T) {
	issuer := gateway.NewJWTTokenService("secret-a")
	verifier := gateway.NewJWTTokenService("secret-b")

	token, err := issuer.IssueToken("user-123")
	require.NoError(t, err)

	_, err = verifier.VerifyToken(token)
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}

func TestJWTTokenService_VerifyToken_Expired(t *testing.T) {
	svc := gateway.NewJWTTokenService("test-secret")
	claims := jwt.RegisteredClaims{
		Subject:   "user-123",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
	}
	expiredToken := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := expiredToken.SignedString([]byte("test-secret"))
	require.NoError(t, err)

	_, err = svc.VerifyToken(signed)
	assert.ErrorIs(t, err, auth.ErrInvalidToken)
}
