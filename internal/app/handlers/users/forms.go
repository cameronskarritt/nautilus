package users

import (
	"strings"

	"nautilus/internal/optional"
	"nautilus/internal/validators"
)

type UpdateUserForm struct {
	Username optional.Optional[string] `json:"username"`
}

func (form *UpdateUserForm) Normalize() {
	if form.Username.Set {
		form.Username.Data = strings.TrimSpace(form.Username.Data)
	}
}

func (form *UpdateUserForm) Validate() error {
	var errs []error

	if form.Username.Set {
		if err := validators.ValidateUsername(form.Username.Data); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return UserUpdateError(errs...)
	}
	return nil
}
