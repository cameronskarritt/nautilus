package orgs

import (
	"net/http"

	"nautilus/internal/errors"
)

func InviteError(err ...error) error {
	return errors.NewHTTPError(http.StatusBadRequest, "Unable to process invite", err...)
}

var ErrInviteNotFound = errors.NewHTTPError(
	http.StatusNotFound,
	"Invite not found",
	errors.ErrorDetail{
		Message: "invite not found or has expired",
		Code:    errors.ErrorCodeINVITE01,
	},
)

var ErrInviteForbidden = errors.NewHTTPError(
	http.StatusForbidden,
	"Permission denied",
	errors.ErrorDetail{
		Message: "you do not have permission to manage invites for this organization",
		Code:    errors.ErrorCodeINVITE02,
	},
)

var (
	ErrEmptyEmail = errors.ErrorDetail{
		Message: "email is required",
		Code:    errors.ErrorCodeINVITE03,
		Field:   "email",
	}

	ErrInvalidEmail = errors.ErrorDetail{
		Message: "email is invalid",
		Code:    errors.ErrorCodeINVITE04,
		Field:   "email",
	}

	ErrInvalidRole = errors.ErrorDetail{
		Message: "role must be one of: member, admin, viewer",
		Code:    errors.ErrorCodeINVITE05,
		Field:   "role",
	}

	ErrCannotInviteOwner = errors.ErrorDetail{
		Message: "cannot invite users as owner",
		Code:    errors.ErrorCodeINVITE06,
		Field:   "role",
	}

	ErrAlreadyMember = errors.ErrorDetail{
		Message: "user is already a member of this organization",
		Code:    errors.ErrorCodeINVITE07,
	}

	ErrEmailMismatch = errors.ErrorDetail{
		Message: "your email does not match the invite",
		Code:    errors.ErrorCodeINVITE08,
	}

	ErrInvalidToken = errors.ErrorDetail{
		Message: "invalid or expired invite token",
		Code:    errors.ErrorCodeINVITE09,
	}

	ErrCannotInvitePersonalOrg = errors.ErrorDetail{
		Message: "cannot invite users to a personal organization",
		Code:    errors.ErrorCodeINVITE10,
	}
)
