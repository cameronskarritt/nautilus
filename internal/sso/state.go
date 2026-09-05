package sso

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"nautilus/internal/crypto/signature"
	"nautilus/internal/errors"
)

const (
	stateCookieName = "sso_state"
	stateTTL        = 10 * time.Minute
)

type statePayload struct {
	Nonce       string `json:"n"`
	Provider    string `json:"p"`
	RedirectURL string `json:"r,omitempty"`
	ExpiresAt   int64  `json:"e"`
}

type StateResult struct {
	Provider    string
	RedirectURL string
}

// GenerateState creates a new state token and sets it as a cookie.
// Returns the state string to be passed to the OAuth provider.
func GenerateState(w http.ResponseWriter, secret, provider, redirectURL string) (string, error) {
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", errors.Wrap(err, "failed to generate nonce")
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

	payload := statePayload{
		Nonce:       nonce,
		Provider:    provider,
		RedirectURL: redirectURL,
		ExpiresAt:   time.Now().Add(stateTTL).Unix(),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", errors.Wrap(err, "failed to marshal state payload")
	}
	payloadB64 := base64.RawURLEncoding.EncodeToString(payloadBytes)

	sig := signature.Sign([]byte(secret), payloadBytes)
	state := fmt.Sprintf("%s.%s", payloadB64, sig)

	cookie := &http.Cookie{
		Name:     stateCookieName,
		Value:    state,
		Path:     "/",
		MaxAge:   int(stateTTL.Seconds()),
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)

	return state, nil
}

// VerifyState verifies the state from the callback against the cookie.
// It returns the state result if valid, or an error if invalid.
func VerifyState(r *http.Request, secret, state string) (*StateResult, error) {
	cookie, err := r.Cookie(stateCookieName)
	if err != nil {
		return nil, errors.Wrap(err, "state cookie not found")
	}

	if cookie.Value != state {
		return nil, errors.New("state mismatch")
	}

	payload, err := parseAndVerifyState(secret, state)
	if err != nil {
		return nil, err
	}

	return &StateResult{
		Provider:    payload.Provider,
		RedirectURL: payload.RedirectURL,
	}, nil
}

func ClearState(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     stateCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	}
	http.SetCookie(w, cookie)
}

func parseAndVerifyState(secret, state string) (*statePayload, error) {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return nil, errors.New("invalid state format")
	}
	payloadB64, sig := parts[0], parts[1]
	if payloadB64 == "" || sig == "" {
		return nil, errors.New("invalid state format")
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode state payload")
	}

	if !signature.Verify([]byte(secret), payloadBytes, sig) {
		return nil, errors.New("invalid state signature")
	}

	var payload statePayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, errors.Wrap(err, "failed to unmarshal state payload")
	}

	if time.Now().Unix() > payload.ExpiresAt {
		return nil, errors.New("state expired")
	}

	return &payload, nil
}
