package sso

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"nautilus/internal/testutil/require"
)

func TestSignAppleClientSecret(t *testing.T) {
	t.Parallel()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	token, err := signAppleClientSecret("TEAM123", "com.example.app", "KEY456", privateKey)
	require.NoError(t, err)

	parsedToken, err := jwt.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	require.NoError(t, err)

	var claims jwt.Claims
	err = parsedToken.Claims(&privateKey.PublicKey, &claims)
	require.NoError(t, err)

	require.Equal(t, "TEAM123", claims.Issuer)
	require.Equal(t, "com.example.app", claims.Subject)
	require.Contains(t, claims.Audience, appleIssuer)
	require.NotNil(t, claims.IssuedAt)
	require.NotNil(t, claims.Expiry)
}
