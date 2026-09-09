package documentdownloads

import "nautilus/internal/errors"

var ErrInvalidDownload = errors.New("invalid document download")

type CreateOptions struct {
	APIKeyID        int    `json:"-"`
	OAuthAccessHash []byte `json:"-"`
	Resource        string `json:"-"`
}

type Download struct {
	OrganizationID int    `json:"-"`
	DocumentID     string `json:"-"`
}
