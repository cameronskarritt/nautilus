package ocr

import (
	"context"
	"io"
)

type OCR interface {
	// Extract reads plaintext document bytes and returns text in reading order.
	// The caller owns data; contentType is its MIME type.
	Extract(ctx context.Context, data io.Reader, contentType string) (string, error)
}
