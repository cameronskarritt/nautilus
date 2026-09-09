package webhooks

import (
	"strings"

	"nautilus/internal/enums"
	"nautilus/internal/httputil"
	"nautilus/internal/optional"
	"nautilus/internal/webhook"
)

type CreateForm struct {
	Name       string                   `json:"name"`
	URL        string                   `json:"url"`
	EventTypes []enums.WebhookEventType `json:"event_types"`
}

func (f *CreateForm) Normalize() {
	f.Name = strings.TrimSpace(f.Name)
	f.URL = strings.TrimSpace(f.URL)
}
func (f *CreateForm) Validate() error {
	return validate(optional.Set(f.Name), optional.Set(f.URL), optional.Set(f.EventTypes))
}

type UpdateForm struct {
	Name       optional.Optional[string]                   `json:"name"`
	URL        optional.Optional[string]                   `json:"url"`
	EventTypes optional.Optional[[]enums.WebhookEventType] `json:"event_types"`
	Enabled    optional.Optional[*bool]                    `json:"enabled"`
}

func (f *UpdateForm) Normalize() {
	if f.Name.Set {
		f.Name.Data = strings.TrimSpace(f.Name.Data)
	}
	if f.URL.Set {
		f.URL.Data = strings.TrimSpace(f.URL.Data)
	}
}
func (f *UpdateForm) Validate() error {
	if !f.Name.Set && !f.URL.Set && !f.EventTypes.Set && !f.Enabled.Set {
		return FormError(ErrEmptyUpdate)
	}
	if f.Enabled.Set && f.Enabled.Data == nil {
		return FormError(ErrEnabled)
	}
	return validate(f.Name, f.URL, f.EventTypes)
}
func validate(name, url optional.Optional[string], types optional.Optional[[]enums.WebhookEventType]) error {
	var errs []error
	if name.Set && !webhook.ValidName(name.Data) {
		errs = append(errs, ErrName)
	}
	if url.Set && webhook.ValidateURL(url.Data) != nil {
		errs = append(errs, ErrURL)
	}
	if types.Set {
		valid := len(types.Data) > 0
		for _, kind := range types.Data {
			valid = valid && kind.IsValid()
		}
		if !valid {
			errs = append(errs, ErrEventTypes)
		}
	}
	if len(errs) > 0 {
		return FormError(errs...)
	}
	return nil
}

var _ httputil.Form = (*CreateForm)(nil)
var _ httputil.Form = (*UpdateForm)(nil)
