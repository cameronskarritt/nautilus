package documents

import (
	"time"

	"nautilus/internal/enums"
)

type Document struct {
	ID             int                  `json:"-"`
	ExternalID     string               `json:"id"`
	OrganizationID int                  `json:"-"`
	ObjectKey      string               `json:"-"`
	Status         enums.DocumentStatus `json:"status"`
	Filename       string               `json:"filename"`
	ContentType    string               `json:"content_type"`
	Size           int64                `json:"size"`
	CreatedAt      time.Time            `json:"created_at"`
	UpdatedAt      time.Time            `json:"updated_at"`
}

type CreateOptions struct {
	Filename    string
	ContentType string
	Size        int64
}
