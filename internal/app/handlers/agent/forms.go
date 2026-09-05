package agent

import (
	"strings"

	"nautilus/internal/httputil"
	"nautilus/internal/optional"
)

type CreateStreamForm struct {
	Message string `json:"message"`
}

func (form *CreateStreamForm) Normalize() {
	form.Message = strings.TrimSpace(form.Message)
}

func (form *CreateStreamForm) Validate() error {
	if form.Message == "" {
		return StreamError(ErrEmptyMessage)
	}
	return nil
}

type SendMessageForm struct {
	Message string `json:"message"`
}

func (form *SendMessageForm) Normalize() {
	form.Message = strings.TrimSpace(form.Message)
}

func (form *SendMessageForm) Validate() error {
	if form.Message == "" {
		return StreamError(ErrEmptyMessage)
	}
	return nil
}

type ResolveApprovalForm struct {
	httputil.NoopNormalizer
	Approved bool                      `json:"approved"`
	Reason   string                    `json:"reason"`
	Message  optional.Optional[string] `json:"message,omitzero"`
}

func (form *ResolveApprovalForm) Validate() error {
	if !form.Approved && strings.TrimSpace(form.Reason) == "" {
		return ApprovalError(ErrReasonRequiredForRejection)
	}
	return nil
}
