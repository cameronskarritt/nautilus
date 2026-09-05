package auth

import (
	"strings"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/optional"
	"nautilus/internal/validators"
)

type LoginForm struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Code     string `json:"code"`
}

func (form *LoginForm) Validate() error {
	var errs []error

	if form.Email == "" {
		errs = append(errs, ErrEmptyEmail)
	}

	if form.Password == "" {
		errs = append(errs, ErrEmptyPassword)
	}

	if len(errs) > 0 {
		return LoginError(errs...)
	}
	return nil
}

func (form *LoginForm) Normalize() {
	form.Email = strings.TrimSpace(form.Email)
	form.Code = strings.TrimSpace(form.Code)
}

type RegisterForm struct {
	Email    string                    `json:"email"`
	Password string                    `json:"password"`
	Username optional.Optional[string] `json:"username"`
}

func (form *RegisterForm) Validate() error {
	var errs []error

	if form.Email == "" {
		errs = append(errs, ErrEmptyEmail)
	} else if !validators.ValidateEmail(form.Email) {
		errs = append(errs, ErrInvalidEmail)
	}

	if err := validators.ValidatePassword(form.Password); err != nil {
		errs = append(errs, err)
	}

	if form.Username.Set {
		if err := validators.ValidateUsername(form.Username.Data); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return RegistrationError(errs...)
	}
	return nil
}

func (form *RegisterForm) Normalize() {
	form.Email = strings.TrimSpace(form.Email)

	if form.Username.Set {
		form.Username.Data = strings.TrimSpace(form.Username.Data)
	}
}

type RequestRecoveryForm struct {
	Email string `json:"email"`
}

func (form *RequestRecoveryForm) Validate() error {
	if form.Email == "" {
		return AccountRecoveryError(ErrEmptyEmail)
	}

	if !validators.ValidateEmail(form.Email) {
		return AccountRecoveryError(ErrInvalidEmail)
	}

	return nil
}

func (form *RequestRecoveryForm) Normalize() {
	form.Email = strings.TrimSpace(form.Email)
}

type CompleteRecoveryForm struct {
	httputil.NoopNormalizer
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (form *CompleteRecoveryForm) Validate() error {
	err := validators.ValidatePassword(form.Password)
	if err != nil {
		return AccountRecoveryError(err)
	}

	return nil
}

type ChangePasswordForm struct {
	httputil.NoopNormalizer
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (form *ChangePasswordForm) Validate() error {
	err := validators.ValidatePassword(form.NewPassword)
	if err != nil {
		if detail, ok := err.(errors.ErrorDetail); ok {
			detail.Field = "new_password"
			err = detail
		}
		return AccountUpdateError(err)
	}

	return nil
}

type ChangeEmailForm struct {
	Email string `json:"email"`
}

func (form *ChangeEmailForm) Validate() error {
	if form.Email == "" {
		return AccountUpdateError(ErrEmptyEmail)
	}

	if !validators.ValidateEmail(form.Email) {
		return AccountUpdateError(ErrInvalidEmail)
	}

	return nil
}

func (form *ChangeEmailForm) Normalize() {
	form.Email = strings.TrimSpace(form.Email)
}

type CompleteEmailChangeForm struct {
	httputil.NoopNormalizer
	Token string `json:"token"`
}

func (form *CompleteEmailChangeForm) Validate() error {
	if form.Token == "" {
		return AccountUpdateError(ErrEmptyToken)
	}

	return nil
}

type CompleteVerificationForm struct {
	httputil.NoopNormalizer
	Token string `json:"token"`
}

func (form *CompleteVerificationForm) Validate() error {
	if form.Token == "" {
		return VerificationError(ErrEmptyToken)
	}

	return nil
}

type SwitchOrganizationForm struct {
	OrganizationID string `json:"organization_id"`
}

func (form *SwitchOrganizationForm) Validate() error {
	if form.OrganizationID == "" {
		return OrgSwitchError(ErrOrgNotFound)
	}

	return nil
}

func (form *SwitchOrganizationForm) Normalize() {
	form.OrganizationID = strings.TrimSpace(form.OrganizationID)
}

type AssumeForm struct {
	OrgSlug string `json:"org_slug"`
}

func (form *AssumeForm) Validate() error {
	if form.OrgSlug == "" {
		return AssumeError(ErrOrgNotFound)
	}

	return nil
}

func (form *AssumeForm) Normalize() {
	form.OrgSlug = strings.TrimSpace(form.OrgSlug)
}

// TOTP Forms

type RequestTOTPForm struct {
	httputil.NoopNormalizer
	Password string `json:"password"`
}

func (form *RequestTOTPForm) Validate() error {
	if form.Password == "" {
		return MFAError(ErrIncorrectPassword)
	}
	return nil
}

type CompleteTOTPForm struct {
	Code string `json:"code"`
}

func (form *CompleteTOTPForm) Validate() error {
	if form.Code == "" {
		return MFAError(ErrEmptyTOTPCode)
	}
	return nil
}

func (form *CompleteTOTPForm) Normalize() {
	form.Code = strings.TrimSpace(form.Code)
}

type DisableTOTPForm struct {
	httputil.NoopNormalizer
	Password string `json:"password"`
}

func (form *DisableTOTPForm) Validate() error {
	if form.Password == "" {
		return MFAError(ErrIncorrectPassword)
	}
	return nil
}
