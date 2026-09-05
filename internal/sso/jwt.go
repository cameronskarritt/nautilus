package sso

import (
	"crypto/ecdsa"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"nautilus/internal/errors"
)

// appleClientSecretClaims represents the claims for an Apple client secret JWT.
type appleClientSecretClaims struct {
	jwt.Claims
}

// signAppleClientSecret creates a signed JWT client secret for Apple Sign In.
// Apple requires client secrets to be ES256-signed JWTs.
func signAppleClientSecret(teamID, clientID, keyID string, privateKey *ecdsa.PrivateKey) (string, error) {
	// Create the signer with the key ID in the header
	signerOpts := new(jose.SignerOptions)
	signerOpts.WithHeader("kid", keyID)

	key := jose.SigningKey{
		Algorithm: jose.ES256,
		Key:       privateKey,
	}
	signer, err := jose.NewSigner(key, signerOpts)
	if err != nil {
		return "", errors.Wrap(err, "failed to create signer")
	}

	now := time.Now()
	claims := appleClientSecretClaims{
		Claims: jwt.Claims{
			Issuer:   teamID,
			Subject:  clientID,
			Audience: jwt.Audience{appleIssuer},
			IssuedAt: jwt.NewNumericDate(now),
			Expiry:   jwt.NewNumericDate(now.Add(5 * time.Minute)),
		},
	}

	token, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		return "", errors.Wrap(err, "failed to sign token")
	}

	return token, nil
}
