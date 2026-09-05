package auth

import (
	"crypto/rand"
	"encoding/base32"
	"net/http"
	"time"

	"nautilus/internal/config"
	"nautilus/internal/crypto/argon2"
	"nautilus/internal/crypto/totp"
	"nautilus/internal/database"
	"nautilus/internal/database/recoverycodes"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
)

const (
	pendingTOTPTTL   = 10 * time.Minute
	totpSecretLength = 20 // 160 bits, RFC 4226 recommendation
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// RequestTOTP initiates the TOTP setup flow.
// Requires password verification and returns an otpauth URI for QR code display.
func (a *Mux) RequestTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	user := users.FromContext(ctx)

	// Only allow for local auth users
	if user.AuthProvider != enums.AuthProviderLocal {
		httputil.Error(ctx, w, MFAError(ErrWrongAuthProvider))
		return
	}

	// Check if MFA is already enabled
	if user.MFAEnabled {
		httputil.Error(ctx, w, MFAError(ErrTOTPAlreadyEnabled))
		return
	}

	var form RequestTOTPForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Verify password
	if !user.Email.Set {
		httputil.Error(ctx, w, MFAError(ErrEmailNotConfigured))
		return
	}

	_, hash, _, err := users.GetPassword(ctx, a.db, user.Email.Data)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	match, err := argon2.Compare(form.Password, hash)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !match {
		httputil.Error(ctx, w, MFAError(ErrIncorrectPassword))
		return
	}

	// Generate TOTP secret
	secretBytes := make([]byte, totpSecretLength)
	_, err = rand.Read(secretBytes)
	if err != nil {
		httputil.Error(ctx, w, errors.Wrap(err, "unable to generate TOTP secret"))
		return
	}
	secret := b32.EncodeToString(secretBytes)

	// Store as pending (encryption handled via context)
	expiresAt := time.Now().Add(pendingTOTPTTL)
	err = users.SetPendingTOTP(ctx, a.db, user.ID, secret, expiresAt)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Generate otpauth URI
	issuer := config.Get("APP_NAME", "Nautilus")
	account := user.Email.Data
	otp := totp.TOTP(issuer, account, secret)
	uri := otp.URI()

	res := httputil.Map{
		"uri": uri,
	}
	httputil.JSON(ctx, w, res)
}

// CompleteTOTP completes the TOTP setup by validating the code.
// On success, enables MFA and returns recovery codes.
func (a *Mux) CompleteTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	// Only allow for local auth users
	if user.AuthProvider != enums.AuthProviderLocal {
		httputil.Error(ctx, w, MFAError(ErrWrongAuthProvider))
		return
	}

	// Check if MFA is already enabled
	if user.MFAEnabled {
		httputil.Error(ctx, w, MFAError(ErrTOTPAlreadyEnabled))
		return
	}

	var form CompleteTOTPForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Get pending TOTP (decryption handled via context)
	pending, err := users.GetPendingTOTP(ctx, a.db, user.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if pending == nil {
		httputil.Error(ctx, w, MFAError(ErrInvalidMFAToken))
		return
	}

	// Validate the code
	valid, err := totp.Validate(pending.Secret, form.Code)
	if err != nil {
		httputil.Error(ctx, w, errors.Wrap(err, "unable to validate TOTP code"))
		return
	}
	if !valid {
		httputil.Error(ctx, w, MFAError(ErrInvalidTOTPCode))
		return
	}

	var codes []string

	// Enable MFA in a transaction
	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		// Enable MFA (secret is already in totp_secret from pending)
		err := users.EnableMFA(ctx, txn, user.ID)
		if err != nil {
			return err
		}

		// Generate recovery codes
		codes, err = recoverycodes.Generate(ctx, txn, user.ID)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	logger.Info("MFA enabled for user", "user_id", user.ID)

	res := httputil.Map{
		"message":        "Two-factor authentication enabled",
		"recovery_codes": codes,
	}
	httputil.JSON(ctx, w, res)
}

// DisableTOTP disables MFA for a user.
// Requires password verification.
func (a *Mux) DisableTOTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	// Only allow for local auth users
	if user.AuthProvider != enums.AuthProviderLocal {
		httputil.Error(ctx, w, MFAError(ErrWrongAuthProvider))
		return
	}

	// Check if MFA is enabled
	if !user.MFAEnabled {
		httputil.Error(ctx, w, MFAError(ErrTOTPNotEnabled))
		return
	}

	var form DisableTOTPForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Verify password
	if !user.Email.Set {
		httputil.Error(ctx, w, MFAError(ErrEmailNotConfigured))
		return
	}

	_, hash, _, err := users.GetPassword(ctx, a.db, user.Email.Data)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	match, err := argon2.Compare(form.Password, hash)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !match {
		httputil.Error(ctx, w, MFAError(ErrIncorrectPassword))
		return
	}

	// Disable MFA in a transaction
	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		err := users.DisableMFA(ctx, txn, user.ID)
		if err != nil {
			return err
		}

		// Delete recovery codes
		err = recoverycodes.DeleteAll(ctx, txn, user.ID)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	logger.Info("MFA disabled for user", "user_id", user.ID)

	res := httputil.Map{
		"message": "Two-factor authentication disabled",
	}
	httputil.JSON(ctx, w, res)
}
