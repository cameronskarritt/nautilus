package apikeys

import (
	"strings"
	"unicode/utf8"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/httputil"
)

type CreateForm struct {
	Name   string          `json:"name"`
	Scopes []apikeys.Scope `json:"scopes"`
}

func (form *CreateForm) Normalize() {
	form.Name = strings.TrimSpace(form.Name)
	scopes := make([]apikeys.Scope, 0, len(form.Scopes))
	seen := make(map[apikeys.Scope]bool, len(form.Scopes))
	for _, scope := range form.Scopes {
		scope = apikeys.Scope(strings.ToLower(strings.TrimSpace(string(scope))))
		if scope != "" && !seen[scope] {
			seen[scope] = true
			scopes = append(scopes, scope)
		}
	}
	form.Scopes = scopes
}

func (form *CreateForm) Validate() error {
	var errs []error
	if form.Name == "" {
		errs = append(errs, ErrEmptyName)
	} else if utf8.RuneCountInString(form.Name) > apikeys.MaxNameLength {
		errs = append(errs, ErrNameTooLong)
	}
	if len(form.Scopes) == 0 {
		errs = append(errs, ErrEmptyScopes)
	} else {
		for _, scope := range form.Scopes {
			if !scope.IsValid() {
				errs = append(errs, ErrInvalidScope)
				break
			}
		}
	}
	if len(errs) > 0 {
		return APIKeyError(errs...)
	}
	return nil
}

func (form CreateForm) Options() *apikeys.CreateOptions {
	return &apikeys.CreateOptions{Name: form.Name, Scopes: form.Scopes}
}

var _ httputil.Form = (*CreateForm)(nil)
