package auth

import (
	"net/http"
	"strings"
	"time"

	"nautilus/internal/crypto/argon2"
	"nautilus/internal/crypto/totp"
	"nautilus/internal/database"
	"nautilus/internal/database/auditlogs"
	"nautilus/internal/database/authcodes"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/recoverycodes"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/mail"
	"nautilus/internal/optional"
)

const (
	recoveryCodeTTL     = 2 * time.Hour
	verificationCodeTTL = 2 * time.Hour
	emailChangeCodeTTL  = 6 * time.Hour
	maxLoginAttempts    = 5
	loginCounterTimeout = 10 * time.Minute
)

func (a *Mux) Register(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	var form RegisterForm

	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	exists, err := users.EmailExists(ctx, a.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if exists {
		httputil.Error(ctx, w, RegistrationError(ErrEmailExists))
		return
	}

	username := form.Username.Data
	if !form.Username.Set {
		username = randomUsername()
	}

	// Register user with personal organization
	result, err := users.RegisterWithPersonalOrg(
		ctx,
		a.db,
		optional.Set(username),
		optional.Set(form.Email),
		form.Password,
	)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	token, err := authcodes.Create(ctx, a.db, enums.AuthCodeVerification, result.User.ID, verificationCodeTTL, nil)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if result.User.Email.Set {
		mailErr := mail.SendTemplate(ctx, a.sender, result.User.Email.Data, enums.MailTemplateEmailVerification, token)
		if mailErr != nil {
			logger.Error("error sending email verification code", "error", mailErr)
		}
	}

	meta := sessions.RequestMetadata(r)
	session, err := sessions.Create(ctx, a.db, result.User.ID, optional.Set(result.Member.ID), meta)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	cookie := sessions.CreateCookie(session.Token)
	http.SetCookie(w, cookie)

	res := httputil.Map{
		"message":      "Account registered",
		"user":         result.User,
		"organization": result.Organization,
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) Login(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	token, err := sessions.FromCookie(r)
	if err == nil {
		session, err := sessions.Get(ctx, a.db, token)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
		if session != nil {
			user, err := users.Get(ctx, a.db, session.UserID)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if user != nil {
				res := httputil.Map{
					"message": "Already logged in",
					"user":    user,
				}
				httputil.JSON(ctx, w, res)
				return
			}
		}
	}

	logger := log.FromContext(ctx)

	var form LoginForm
	err = httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	userID, hash, mfaEnabled, err := users.GetPassword(ctx, a.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if userID == -1 {
		err := LoginError(ErrNoUser)
		httputil.Error(ctx, w, err)
		return
	}

	key := loginCounterKey(userID)
	attempts, _, err := a.counter.Count(ctx, key)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if attempts > maxLoginAttempts {
		httputil.Error(ctx, w, ErrTooManyAttempts)
		return
	}

	// Move the counter expiration forward every attempt, to prevent bursting
	// at the edge of expiration
	err = a.counter.Expire(ctx, key, loginCounterTimeout)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Check MFA requirement before expensive password comparison to prevent timing attacks.
	// If MFA is enabled but no code provided, prompt for code before verifying password.
	if mfaEnabled && form.Code == "" {
		httputil.Error(ctx, w, LoginError(ErrMFARequired))
		return
	}

	match, err := argon2.Compare(form.Password, hash)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !match {
		err := LoginError(ErrPasswordMismatch)
		httputil.Error(ctx, w, err)
		return
	}

	// Validate MFA code if enabled
	if mfaEnabled {
		secret, err := users.GetTOTPSecret(ctx, a.db, userID)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
		if secret == "" {
			httputil.Error(ctx, w, MFAError(ErrTOTPNotEnabled))
			return
		}

		valid, err := totp.Validate(secret, form.Code)
		if err != nil {
			httputil.Error(ctx, w, errors.Wrap(err, "unable to validate TOTP code"))
			return
		}

		// If TOTP code is invalid, try recovery code
		if !valid {
			recoveryValid, err := recoverycodes.Verify(ctx, a.db, userID, form.Code)
			if err != nil {
				httputil.Error(ctx, w, err)
				return
			}
			if recoveryValid {
				valid = true
				logger.Info("MFA verified with recovery code", "user_id", userID)
			}
		}

		if !valid {
			httputil.Error(ctx, w, LoginError(ErrInvalidTOTPCode))
			return
		}
	}

	// Clear rate limit counter on successful login
	err = a.counter.Expire(ctx, key, -1)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	user, err := users.Get(ctx, a.db, userID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Get the user's default org membership (personal org first)
	defaultMember, err := organizations.GetDefaultMemberForUser(ctx, a.db, user.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	var orgMemberID optional.Optional[int]
	if defaultMember != nil {
		orgMemberID = optional.Set(defaultMember.ID)
	}

	meta := sessions.RequestMetadata(r)
	session, err := sessions.Create(ctx, a.db, user.ID, orgMemberID, meta)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	cookie := sessions.CreateCookie(session.Token)
	http.SetCookie(w, cookie)

	res := httputil.Map{
		"message": "Login successful",
		"user":    user,
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// This endpoint isn't behind RequireSession to handle the client desyncing
	// so we'll have to manually fetch the session
	token, err := sessions.FromCookie(r)
	if err != nil {
		// Note(CLS): As of writing, this is the only possible error FromCookie can return
		if errors.Is(err, http.ErrNoCookie) || errors.Is(err, sessions.ErrNoAuthorizationHeader) {
			res := httputil.Map{
				"message": "Logout successful",
			}
			httputil.JSON(ctx, w, res)
			return
		}

		httputil.Error(ctx, w, err)
		return
	}

	session, err := sessions.Get(ctx, a.db, token)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Pass through even if the session does not exist, though the client
	// shouldn't ever hit this endpoint without a session, it's still possible
	if session != nil {
		err := sessions.End(ctx, a.db, session.ID)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
	}

	cookie := sessions.DeleteCookie()
	http.SetCookie(w, cookie)

	res := httputil.Map{
		"message": "Logout successful",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) RequestRecovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)

	var form RequestRecoveryForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	exists, err := users.EmailExists(ctx, a.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// To prevent user enumeration, every exit here should be a happy path
	res := httputil.Map{
		"message": "account recovery initiated",
	}

	if !exists {
		logger.Warn("attempted to intiate recovery for nonexistent user", "email", form.Email)
		httputil.JSON(ctx, w, res)
		return
	}

	userID, provider, err := users.GetAuthProvider(ctx, a.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if provider != enums.AuthProviderLocal {
		logger.Info("attempted to recover non-local account", "email", form.Email)
		mailErr := mail.SendTemplate(ctx, a.sender, form.Email, enums.MailTemplateWrongAuthProvider, provider)
		if mailErr != nil {
			logger.Error("error sending wrong auth provider email", "error", mailErr)
		}

		httputil.JSON(ctx, w, res)
		return
	}

	logger.Info("initiating account recovery", "email", form.Email)

	token, err := authcodes.Create(ctx, a.db, enums.AuthCodeRecovery, userID, recoveryCodeTTL, nil)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	mailErr := mail.SendTemplate(ctx, a.sender, form.Email, enums.MailTemplateInitiateRecovery, token)
	if mailErr != nil {
		logger.Error("error sending initiate recovery email", "error", mailErr)
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) CompleteRecovery(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	var form CompleteRecoveryForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	var userID int

	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		code, err := authcodes.Verify(ctx, txn, enums.AuthCodeRecovery, form.Token)
		if err != nil {
			return err
		}
		if code == nil {
			return AccountRecoveryError(ErrInvalidAuthCode)
		}
		userID = code.UserID

		err = users.UpdatePassword(ctx, txn, userID, form.Password)
		if err != nil {
			return err
		}

		err = sessions.EndAll(ctx, txn, userID)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	logger.Info("account recovered", "user_id", userID)

	user, err := users.Get(ctx, a.db, userID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if user.Email.Set {
		mailErr := mail.SendTemplate(ctx, a.sender, user.Email.Data, enums.MailTemplateCompleteRecovery, nil)
		if mailErr != nil {
			logger.Error("error sending recovery complete email", "error", mailErr)
		}
	}

	res := httputil.Map{
		"message": "account recovered",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) ChangePassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)

	var form ChangePasswordForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	user := users.FromContext(ctx)
	sessionID := sessions.FromContext(ctx)

	if user.AuthProvider != enums.AuthProviderLocal {
		err = AccountUpdateError(ErrWrongAuthProvider)
		httputil.Error(ctx, w, err)
		return
	}

	if !user.Email.Set {
		err = AccountUpdateError(ErrEmailNotConfigured)
		httputil.Error(ctx, w, err)
		return
	}
	email := user.Email.Data

	_, hash, _, err := users.GetPassword(ctx, a.db, email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	match, err := argon2.Compare(form.OldPassword, hash)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if !match {
		err := AccountUpdateError(ErrOldPasswordMismatch)
		httputil.Error(ctx, w, err)
		return
	}

	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		err := users.UpdatePassword(ctx, txn, user.ID, form.NewPassword)
		if err != nil {
			return err
		}

		err = sessions.EndAllExcept(ctx, txn, user.ID, sessionID)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if user.Email.Set {
		mailErr := mail.SendTemplate(ctx, a.sender, user.Email.Data, enums.MailTemplatePasswordUpdated, nil)
		if mailErr != nil {
			logger.Error("error sending password updated email", "error", mailErr)
		}
	}

	res := httputil.Map{
		"message": "password updated",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) RequestEmailChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	if user.AuthProvider != enums.AuthProviderLocal {
		err := AccountUpdateError(ErrWrongAuthProvider)
		httputil.Error(ctx, w, err)
		return
	}
	if !user.Email.Set {
		err := AccountUpdateError(ErrEmailNotConfigured)
		httputil.Error(ctx, w, err)
		return
	}
	email := user.Email.Data

	var form ChangeEmailForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if strings.EqualFold(email, form.Email) {
		err = AccountUpdateError(ErrEmailUnchanged)
		httputil.Error(ctx, w, err)
		return
	}

	exists, err := users.EmailExists(ctx, a.db, form.Email)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if exists {
		err = AccountUpdateError(ErrEmailExists)
		httputil.Error(ctx, w, err)
		return
	}

	data := authcodes.ChangeEmailData{
		OldEmail: email,
		NewEmail: form.Email,
	}
	token, err := authcodes.Create(ctx, a.db, enums.AuthCodeEmailChange, user.ID, emailChangeCodeTTL, data)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	err = mail.SendTemplate(ctx, a.sender, email, enums.MailTemplateChangeEmailRequest, token)
	if err != nil {
		logger.Error("error sending email change request email", "error", err)
	}

	res := httputil.Map{
		"message": "email confirmation sent",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) CompleteEmailChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	var form CompleteEmailChangeForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Note(CLS): Intentionally do this outside of a transaction - we always want to burn the token
	// This call is non-interactive, and inherently race-conditiony since an ErrEmailExists
	// is non-retryable.
	// Technically we could still fail the UpdateEmail call, but I'm willing to take that risk.
	code, err := authcodes.Verify(ctx, a.db, enums.AuthCodeEmailChange, form.Token)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if code == nil {
		err = AccountUpdateError(ErrInvalidAuthCode)
		httputil.Error(ctx, w, err)
		return
	}

	var data authcodes.ChangeEmailData
	err = code.UnmarshalData(&data)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	exists, err := users.EmailExists(ctx, a.db, data.NewEmail)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if exists {
		err = AccountUpdateError(ErrEmailExists)
		httputil.Error(ctx, w, err)
		return
	}

	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		err := users.UpdateEmail(ctx, a.db, user.ID, data.NewEmail)
		if err != nil {
			return err
		}

		err = users.SetVerification(ctx, txn, user.ID, false)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Notify the old email of the address change
	err = mail.SendTemplate(ctx, a.sender, data.OldEmail, enums.MailTemplateEmailUpdated, nil)
	if err != nil {
		logger.Error("error sending email updated email", "error", err)
	}

	// Send a verification token to the new address since the account has been unverified
	token, err := authcodes.Create(ctx, a.db, enums.AuthCodeVerification, user.ID, verificationCodeTTL, nil)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	err = mail.SendTemplate(ctx, a.sender, data.NewEmail, enums.MailTemplateEmailVerification, token)
	if err != nil {
		logger.Error("error sending email verification code", "error", err)
	}

	user, err = users.Get(ctx, a.db, user.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"message": "email updated",
		"user":    user,
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) RequestVerifcation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	if user.AuthProvider != enums.AuthProviderLocal {
		err := AccountUpdateError(ErrWrongAuthProvider)
		httputil.Error(ctx, w, err)
		return
	}

	if user.Verified {
		err := VerificationError(ErrAlreadyVerified)
		httputil.Error(ctx, w, err)
		return
	}

	if !user.Email.Set {
		err := AccountUpdateError(ErrEmailNotConfigured)
		httputil.Error(ctx, w, err)
		return
	}
	email := user.Email.Data

	token, err := authcodes.Create(ctx, a.db, enums.AuthCodeVerification, user.ID, verificationCodeTTL, nil)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	err = mail.SendTemplate(ctx, a.sender, email, enums.MailTemplateEmailVerification, token)
	if err != nil {
		logger.Error("error sending verification code", "error", err)
	}

	res := httputil.Map{
		"message": "verification code sent",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) CompleteVerification(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	logger := log.FromContext(ctx)
	user := users.FromContext(ctx)

	if user.AuthProvider != enums.AuthProviderLocal {
		err := VerificationError(ErrWrongAuthProvider)
		httputil.Error(ctx, w, err)
		return
	}

	if user.Verified {
		err := VerificationError(ErrAlreadyVerified)
		httputil.Error(ctx, w, err)
		return
	}

	var form CompleteVerificationForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	err = database.Transact(ctx, a.db, func(txn database.Database) error {
		code, err := authcodes.Verify(ctx, txn, enums.AuthCodeVerification, form.Token)
		if err != nil {
			return err
		}
		if code == nil {
			return VerificationError(ErrInvalidAuthCode)
		}

		err = users.SetVerification(ctx, txn, user.ID, true)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	if user.Email.Set {
		mailErr := mail.SendTemplate(ctx, a.sender, user.Email.Data, enums.MailTemplateWelcome, nil)
		if mailErr != nil {
			logger.Error("error sending welcome email from verification", "error", mailErr)
		}
	}

	res := httputil.Map{
		"message": "account verified",
	}
	httputil.JSON(ctx, w, res)
}

func (a *Mux) SwitchOrganization(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	user := users.FromContext(ctx)
	sessionID := sessions.FromContext(ctx)

	var form SwitchOrganizationForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Verify the organization exists
	org, err := organizations.GetByExternalID(ctx, a.db, form.OrganizationID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, OrgSwitchError(ErrOrgNotFound))
		return
	}

	// Verify the user is a member of the organization
	member, err := organizations.GetMemberByUserAndOrg(ctx, a.db, user.ID, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if member == nil {
		httputil.Error(ctx, w, OrgSwitchError(ErrNotOrgMember))
		return
	}

	// Update the session's org context
	err = sessions.SwitchOrg(ctx, a.db, sessionID, member.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	res := httputil.Map{
		"message":      "organization switched",
		"organization": org,
	}
	httputil.JSON(ctx, w, res)
}

// Assume allows an admin to assume access to another organization.
// The admin's session is updated with the assumed org ID.
func (a *Mux) Assume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	adminUser := users.FromContext(ctx)
	sessionID := sessions.FromContext(ctx)

	var form AssumeForm
	err := httputil.ProcessForm(r, &form)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Look up the organization by slug
	org, err := organizations.GetBySlug(ctx, a.db, form.OrgSlug)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, AssumeError(ErrOrgNotFound))
		return
	}

	// Update the session with the assumed org
	err = sessions.AssumeOrg(ctx, a.db, sessionID, org.ID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Log the impersonation event
	_, err = auditlogs.Create(ctx, a.db, adminUser.ID, enums.AuditTypeOrgAssume, optional.Set(org.ID), optional.Empty[any]())
	if err != nil {
		// Log error but don't fail the request
		logger.Error("unable to create audit log", "error", err)
	}

	logger.Info("admin assuming organization",
		"admin_id", adminUser.ID,
		"org_id", org.ID,
		"org_slug", org.Slug,
	)

	res := httputil.Map{
		"message":      "organization assumed",
		"organization": org,
	}
	httputil.JSON(ctx, w, res)
}

// Unassume clears the assumed organization from the admin's session.
func (a *Mux) Unassume(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := log.FromContext(ctx)

	adminUser := users.FromContext(ctx)
	sessionID := sessions.FromContext(ctx)

	// Clear the assumed org from the session
	err := sessions.UnassumeOrg(ctx, a.db, sessionID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}

	// Log the unassume event
	_, err = auditlogs.Create(ctx, a.db, adminUser.ID, enums.AuditTypeOrgUnassume, optional.Empty[int](), optional.Empty[any]())
	if err != nil {
		// Log error but don't fail the request
		logger.Error("unable to create audit log", "error", err)
	}

	logger.Info("admin unassuming organization",
		"admin_id", adminUser.ID,
	)

	res := httputil.Map{
		"message": "organization unassumed",
	}
	httputil.JSON(ctx, w, res)
}
