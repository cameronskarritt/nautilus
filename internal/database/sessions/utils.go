package sessions

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"

	"nautilus/internal/errors"
)

const sessionCookieName = "nautilus-session"

var ErrNoAuthorizationHeader = errors.New("authorization header is required")
var b64 = base64.RawURLEncoding

func hashToken(token string) (string, error) {
	b, err := b64.DecodeString(token)
	if err != nil {
		return "", errors.Wrap(err, "unable to decode ")
	}

	h := sha256.New()
	_, err = h.Write(b)
	if err != nil {
		return "", errors.Wrap(err, "error writing token hash")
	}
	hash := h.Sum(nil)
	encoded := b64.EncodeToString(hash)

	return encoded, nil
}

func generateToken(n int) (string, string, error) {
	buf := make([]byte, n)
	_, err := rand.Read(buf)
	if err != nil {
		return "", "", errors.Wrap(err, "error reading rand source")
	}

	token := b64.EncodeToString(buf)

	hash, err := hashToken(token)
	if err != nil {
		return "", "", err
	}

	return token, hash, nil
}

func CreateCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		MaxAge:   sessionTTL,
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
		Domain:   "localhost",
	}
}

func DeleteCookie() *http.Cookie {
	cookie := CreateCookie("")
	cookie.MaxAge = -1
	return cookie
}

func FromCookie(r *http.Request) (string, error) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return "", errors.Wrap(err, "unable to read cookie")
	}

	return cookie.Value, nil
}

func FromHeader(r *http.Request) (string, error) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", errors.New("authorization header is required")
	}
	return header, nil
}
