package admin

import (
	"strings"

	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/optional"
)

type CreateFlagForm struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Enabled     bool   `json:"enabled"`
}

func (form *CreateFlagForm) Normalize() {
	form.Name = strings.TrimSpace(form.Name)
	form.Description = strings.TrimSpace(form.Description)
}

func (form *CreateFlagForm) Validate() error {
	var errs []error

	if form.Name == "" {
		errs = append(errs, ErrEmptyFlagName)
	}

	if len(errs) > 0 {
		return FeatureFlagError(errs...)
	}

	return nil
}

type UpdateFlagForm struct {
	Description optional.Optional[string] `json:"description"`
	Enabled     optional.Optional[bool]   `json:"enabled"`
}

func (form *UpdateFlagForm) Normalize() {
	if form.Description.IsSet() {
		form.Description = optional.Set(strings.TrimSpace(form.Description.Data))
	}
}

func (form *UpdateFlagForm) Validate() error {
	// All fields are optional for PATCH
	return nil
}

type UpdateOrgFlagForm struct {
	FlagID  int  `json:"flag_id"`
	Enabled bool `json:"enabled"`
}

func (form *UpdateOrgFlagForm) Normalize() {}

func (form *UpdateOrgFlagForm) Validate() error {
	if form.FlagID <= 0 {
		return FeatureFlagError(ErrInvalidFlagID)
	}
	return nil
}

var ErrInvalidFlagID = errors.ErrorDetail{
	Message: "flag_id must be a positive integer",
	Code:    errors.ErrorCodeADMIN06,
	Field:   "flag_id",
}

var (
	_ httputil.Form = (*CreateFlagForm)(nil)
	_ httputil.Form = (*UpdateFlagForm)(nil)
	_ httputil.Form = (*UpdateOrgFlagForm)(nil)
)
