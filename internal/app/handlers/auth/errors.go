package auth

import (
	"net/http"

	"nautilus/internal/errors"
)

func RegistrationError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to complete registration", errs...)
}

func LoginError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusUnauthorized, "Unable to log in", errs...)
}

func AccountRecoveryError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to recover account", errs...)
}

func AccountUpdateError(err ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to update account", err...)
}

func VerificationError(err ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to verify account", err...)
}

var ErrEmailExists = errors.ErrorDetail{
	Message: "email address is already in use",
	Code:    errors.ErrorCodeAUTH01,
	Field:   "email",
}

var ErrEmptyEmail = errors.ErrorDetail{
	Message: "email cannot be empty",
	Code:    errors.ErrorCodeAUTH02,
	Field:   "email",
}

var ErrEmptyPassword = errors.ErrorDetail{
	Message: "password cannot be empty",
	Code:    errors.ErrorCodeAUTH30,
	Field:   "password",
}

var ErrInvalidEmail = errors.ErrorDetail{
	Message: "invalid email address",
	Code:    errors.ErrorCodeAUTH03,
	Field:   "email",
}

// Couple these two together to prevent user enumeration
var ErrNoUser = errors.ErrorDetail{
	Message: "invalid email or password",
	Code:    errors.ErrorCodeAUTH04,
}
var ErrPasswordMismatch = errors.ErrorDetail{
	Message: "invalid email or password",
	Code:    errors.ErrorCodeAUTH04,
}

var ErrInvalidAuthCode = errors.ErrorDetail{
	Message: "invalid or expired code",
	Code:    errors.ErrorCodeAUTH05,
}

var ErrUsernameExists = errors.ErrorDetail{
	Message: "username is unavailable",
	Code:    errors.ErrorCodeAUTH08,
	Field:   "username",
}

var ErrOldPasswordMismatch = errors.ErrorDetail{
	Message: "passwords do not match",
	Code:    errors.ErrorCodeAUTH09,
	Field:   "old_password",
}

var ErrIncorrectPassword = errors.ErrorDetail{
	Message: "password is incorrect",
	Code:    errors.ErrorCodeAUTH09,
	Field:   "password",
}

var ErrTooManyAttempts = errors.NewHTTPError(
	http.StatusTooManyRequests,
	"Unable to log in",
	errors.ErrorDetail{
		Message: "too many failed login attempts, please try again later",
		Code:    errors.ErrorCodeAUTH10,
	},
)

var ErrEmailUnchanged = errors.ErrorDetail{
	Message: "new email cannot be the same as the current email",
	Code:    errors.ErrorCodeAUTH11,
	Field:   "email",
}

var ErrAlreadyVerified = errors.ErrorDetail{
	Message: "user already verified",
	Code:    errors.ErrorCodeAUTH12,
}

var ErrWrongAuthProvider = errors.ErrorDetail{
	Message: "wrong authentication provider",
	Code:    errors.ErrorCodeAUTH13,
}

var ErrEmptyToken = errors.ErrorDetail{
	Message: "token cannot be empty",
	Code:    errors.ErrorCodeAUTH14,
}

var ErrEmailNotConfigured = errors.ErrorDetail{
	Message: "email not configured",
	Code:    errors.ErrorCodeAUTH15,
}

func OrgSwitchError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to switch organization", errs...)
}

var ErrOrgNotFound = errors.ErrorDetail{
	Message: "organization not found",
	Code:    errors.ErrorCodeORG01,
	Field:   "organization_id",
}

var ErrNotOrgMember = errors.ErrorDetail{
	Message: "not a member of this organization",
	Code:    errors.ErrorCodeORG02,
	Field:   "organization_id",
}

func SSOError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to complete SSO authentication", errs...)
}

var ErrInvalidProvider = errors.ErrorDetail{
	Message: "This sign-in method is not available",
	Code:    errors.ErrorCodeAUTH16,
	Field:   "provider",
}

var ErrSSOProviderError = errors.ErrorDetail{
	Message: "The sign-in provider returned an error. Please try again.",
	Code:    errors.ErrorCodeAUTH17,
}

var ErrSSOMissingCode = errors.ErrorDetail{
	Message: "Sign-in was cancelled or incomplete. Please try again.",
	Code:    errors.ErrorCodeAUTH18,
}

var ErrSSOInvalidState = errors.ErrorDetail{
	Message: "Your session expired. Please try again.",
	Code:    errors.ErrorCodeAUTH19,
}

var ErrSSOProviderMismatch = errors.ErrorDetail{
	Message: "Sign-in provider mismatch. Please try again.",
	Code:    errors.ErrorCodeAUTH20,
}

var ErrSSOExchangeFailed = errors.ErrorDetail{
	Message: "Failed to complete sign-in. Please try again.",
	Code:    errors.ErrorCodeAUTH21,
}

var ErrSSOEmailExists = errors.ErrorDetail{
	Message: "An account with this email already exists. Try signing in with a different method.",
	Code:    errors.ErrorCodeAUTH22,
}

var ErrSSOServerError = errors.ErrorDetail{
	Message: "Something went wrong. Please try again later.",
	Code:    errors.ErrorCodeAUTH23,
}

var ErrSSOOrganizationMembership = errors.ErrorDetail{
	Message: "Your GitHub account must be an active member of the configured organization.",
	Code:    errors.ErrorCodeAUTH31,
}

func AssumeError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to assume session", errs...)
}

// MFA Errors

func MFAError(errs ...error) *errors.HTTPError {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to complete MFA operation", errs...)
}

var ErrInvalidTOTPCode = errors.ErrorDetail{
	Message: "invalid verification code",
	Code:    errors.ErrorCodeAUTH24,
	Field:   "code",
}

var ErrTOTPNotEnabled = errors.ErrorDetail{
	Message: "two-factor authentication is not enabled",
	Code:    errors.ErrorCodeAUTH25,
}

var ErrTOTPAlreadyEnabled = errors.ErrorDetail{
	Message: "two-factor authentication is already enabled",
	Code:    errors.ErrorCodeAUTH26,
}

var ErrMFARequired = errors.ErrorDetail{
	Message: "two-factor authentication required",
	Code:    errors.ErrorCodeAUTH27,
}

var ErrInvalidMFAToken = errors.ErrorDetail{
	Message: "invalid or expired MFA session",
	Code:    errors.ErrorCodeAUTH28,
}

var ErrEmptyTOTPCode = errors.ErrorDetail{
	Message: "verification code cannot be empty",
	Code:    errors.ErrorCodeAUTH29,
	Field:   "code",
}
