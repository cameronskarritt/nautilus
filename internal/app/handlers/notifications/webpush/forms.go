package webpush

import (
	"strings"

	"nautilus/internal/httputil"
)

type SubscribeForm struct {
	Endpoint string        `json:"endpoint"`
	Keys     SubscribeKeys `json:"keys"`
}

type SubscribeKeys struct {
	Auth   string `json:"auth"`
	P256dh string `json:"p256dh"`
}

func (form *SubscribeForm) Normalize() {
	form.Endpoint = strings.TrimSpace(form.Endpoint)
	form.Keys.Auth = strings.TrimSpace(form.Keys.Auth)
	form.Keys.P256dh = strings.TrimSpace(form.Keys.P256dh)
}

func (form *SubscribeForm) Validate() error {
	var errs []error

	if form.Endpoint == "" {
		errs = append(errs, ErrEmptyEndpoint)
	}
	if form.Keys.Auth == "" {
		errs = append(errs, ErrEmptyAuthKey)
	}
	if form.Keys.P256dh == "" {
		errs = append(errs, ErrEmptyP256dhKey)
	}

	if len(errs) > 0 {
		return SubscriptionError(errs...)
	}
	return nil
}

type UnsubscribeForm struct {
	Endpoint string `json:"endpoint"`
}

func (form *UnsubscribeForm) Normalize() {
	form.Endpoint = strings.TrimSpace(form.Endpoint)
}

func (form *UnsubscribeForm) Validate() error {
	if form.Endpoint == "" {
		return SubscriptionError(ErrEmptyEndpoint)
	}
	return nil
}

var (
	_ httputil.Form = (*SubscribeForm)(nil)
	_ httputil.Form = (*UnsubscribeForm)(nil)
)
