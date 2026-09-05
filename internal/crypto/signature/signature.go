package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
)

// Sign signs data with HMAC-SHA256 and returns a base64url-encoded signature.
func Sign(secret, data []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify checks if the base64url-encoded signature matches the data.
func Verify(secret, data []byte, sig string) bool {
	signature, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, secret)
	mac.Write(data)
	expected := mac.Sum(nil)

	return hmac.Equal(signature, expected)
}

// SignString is a convenience wrapper for signing string data.
func SignString(secret, data string) string {
	return Sign([]byte(secret), []byte(data))
}

// VerifyString is a convenience wrapper for verifying string data.
func VerifyString(secret, data, sig string) bool {
	return Verify([]byte(secret), []byte(data), sig)
}
