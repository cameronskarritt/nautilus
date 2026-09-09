package documents

import (
	"time"

	"nautilus/internal/enums"
)

type Document struct {
	UploadToken     string               `json:"-"`
	UploadExpiresAt time.Time            `json:"-"`
	UploadReady     bool                 `json:"-"`
	ID              int                  `json:"-"`
	ExternalID      string               `json:"id"`
	OrganizationID  int                  `json:"-"`
	ObjectKey       string               `json:"-"`
	PDFKey          string               `json:"-"`
	Status          enums.DocumentStatus `json:"status"`
	Filename        string               `json:"filename"`
	ContentType     string               `json:"content_type"`
	Size            int64                `json:"size"`
	SHA256          string               `json:"sha256"` // Lowercase hex digest of downloadable plaintext, or empty when unknown.
	PageCount       int                  `json:"page_count"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
}

type CreateOptions struct {
	Filename    string
	ContentType string
	Size        int64
	Pages       []PageOptions
}

type PageOptions struct {
	ContentType string
	Size        int64
}

type Page struct {
	Number      int    `json:"number"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	ObjectKey   string `json:"-"`
}
