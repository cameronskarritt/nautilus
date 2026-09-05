package webpush

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"nautilus/internal/errors"
)

// GenerateVAPIDKeys creates a VAPID key pair and returns the base64url-encoded
// private and public keys.
func GenerateVAPIDKeys() (privateKey, publicKey string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		err = errors.Wrap(err, "unable to generate VAPID private key")
		return
	}

	privBytes, err := priv.Bytes()
	if err != nil {
		err = errors.Wrap(err, "unable to encode VAPID private key")
		return
	}
	pubBytes, err := priv.PublicKey.Bytes()
	if err != nil {
		err = errors.Wrap(err, "unable to encode VAPID public key")
		return
	}

	privateKey = base64.RawURLEncoding.EncodeToString(privBytes)
	publicKey = base64.RawURLEncoding.EncodeToString(pubBytes)

	return
}

// generateVAPIDHeaderKeys derives the ECDSA private key from raw bytes for JWT signing.
func generateVAPIDHeaderKeys(privateKey []byte) (*ecdsa.PrivateKey, error) {
	priv, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "unable to parse VAPID private key")
	}
	return priv, nil
}

// getVAPIDAuthorizationHeader builds the VAPID Authorization header value
// in the format: vapid t=<jwt>, k=<publicKey>
func getVAPIDAuthorizationHeader(
	endpoint,
	subscriber,
	vapidPublicKey,
	vapidPrivateKey string,
	expiration time.Time,
) (string, error) {
	subURL, err := url.Parse(endpoint)
	if err != nil {
		return "", errors.Wrap(err, "unable to parse endpoint URL")
	}

	if !strings.HasPrefix(subscriber, "https:") {
		subscriber = "mailto:" + subscriber
	}

	token := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"aud": subURL.Scheme + "://" + subURL.Host,
		"exp": expiration.Unix(),
		"sub": subscriber,
	})

	decodedVapidPrivateKey, err := decodeVapidKey(vapidPrivateKey)
	if err != nil {
		return "", err
	}

	privKey, err := generateVAPIDHeaderKeys(decodedVapidPrivateKey)
	if err != nil {
		return "", err
	}

	jwtString, err := token.SignedString(privKey)
	if err != nil {
		return "", errors.Wrap(err, "unable to sign VAPID token")
	}

	pubKey, err := decodeVapidKey(vapidPublicKey)
	if err != nil {
		return "", err
	}

	return "vapid t=" + jwtString + ", k=" + base64.RawURLEncoding.EncodeToString(pubKey), nil
}

// decodeVapidKey decodes a VAPID key from either padded or unpadded base64url encoding.
// See: https://github.com/SherClockHolmes/webpush-go/issues/29
func decodeVapidKey(key string) ([]byte, error) {
	bytes, err := base64.URLEncoding.DecodeString(key)
	if err == nil {
		return bytes, nil
	}

	bytes, err = base64.RawURLEncoding.DecodeString(key)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decode VAPID key")
	}
	return bytes, nil
}
