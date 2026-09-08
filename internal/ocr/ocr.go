package ocr

import (
	"context"
	"io"

	"nautilus/internal/errors"
)

// ErrInvalidDocument identifies unsupported, malformed, or oversized documents
// that cannot be fixed by retrying the OCR provider.
var ErrInvalidDocument = errors.New("document cannot be processed by OCR")

type OCR interface {
	// Extract reads plaintext document bytes and returns text in reading order.
	// The caller owns data; contentType is its MIME type.
	Extract(ctx context.Context, data io.Reader, contentType string) (string, error)
}
