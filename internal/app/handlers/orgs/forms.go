package orgs

import (
	"strings"

	"nautilus/internal/database/organizations"
	"nautilus/internal/validators"
)

type CreateInviteForm struct {
	Email string             `json:"email"`
	Role  organizations.Role `json:"role"`
}

func (form *CreateInviteForm) Normalize() {
	form.Email = strings.TrimSpace(strings.ToLower(form.Email))
	form.Role = organizations.Role(strings.TrimSpace(string(form.Role)))
}

func (form *CreateInviteForm) Validate() error {
	var errs []error

	if form.Email == "" {
		errs = append(errs, ErrEmptyEmail)
	} else if !validators.ValidateEmail(form.Email) {
		errs = append(errs, ErrInvalidEmail)
	}

	if !form.Role.IsValid() {
		errs = append(errs, ErrInvalidRole)
	} else if form.Role == organizations.RoleOwner {
		errs = append(errs, ErrCannotInviteOwner)
	}

	if len(errs) > 0 {
		return InviteError(errs...)
	}
	return nil
}
