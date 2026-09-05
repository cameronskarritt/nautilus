package webpush

import (
	"encoding/base64"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nautilus/internal/testutil/require"
)

func TestGenerateVAPIDKeys(t *testing.T) {
	t.Parallel()

	privateKey, publicKey, err := GenerateVAPIDKeys()
	require.NoError(t, err)
	require.Len(t, privateKey, 43)
	require.Len(t, publicKey, 87)
}

func TestGetVAPIDAuthorizationHeader(t *testing.T) {
	t.Parallel()

	endpoint := "https://updates.push.services.mozilla.com/wpush/v2/gAAAAA"
	sub := "test@test.com"

	vapidPrivateKey, vapidPublicKey, err := GenerateVAPIDKeys()
	require.NoError(t, err)

	expiration := time.Now().Add(time.Hour*11 + 23*time.Minute)

	vapidAuthHeader, err := getVAPIDAuthorizationHeader(
		endpoint,
		sub,
		vapidPublicKey,
		vapidPrivateKey,
		expiration,
	)
	require.NoError(t, err)

	tokenString := extractTokenFromHeader(t, vapidAuthHeader)

	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodECDSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		decodedVapidPrivateKey, err := base64.RawURLEncoding.DecodeString(vapidPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("unable to decode VAPID private key: %w", err)
		}

		privKey, err := generateVAPIDHeaderKeys(decodedVapidPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("unable to derive VAPID header keys: %w", err)
		}
		return privKey.Public(), nil
	})
	require.NoError(t, err)
	require.True(t, token.Valid)

	claims, ok := token.Claims.(jwt.MapClaims)
	require.True(t, ok)

	require.Equal(t, fmt.Sprintf("mailto:%s", sub), claims["sub"])
	require.NotEqual(t, "", claims["aud"])
	require.Equal(t, float64(expiration.Unix()), claims["exp"])
}

func TestDecodeVapidKey(t *testing.T) {
	t.Parallel()

	_, publicKey, err := GenerateVAPIDKeys()
	require.NoError(t, err)

	tests := []struct {
		Name string
		Key  string
	}{
		{
			Name: "raw url encoding (no padding)",
			Key:  publicKey,
		},
		{
			Name: "url encoding with padding",
			Key:  publicKey + "=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			decoded, err := decodeVapidKey(tt.Key)
			require.NoError(t, err)
			require.True(t, len(decoded) > 0)
		})
	}
}

func extractTokenFromHeader(t *testing.T, header string) string {
	t.Helper()

	// header format: "vapid t=<jwt>, k=<key>"
	hsplit := strings.Split(header, " ")
	require.True(t, len(hsplit) >= 3)

	tsplit := strings.Split(hsplit[1], "=")
	require.True(t, len(tsplit) >= 2)

	// Remove trailing comma
	return tsplit[1][:len(tsplit[1])-1]
}
