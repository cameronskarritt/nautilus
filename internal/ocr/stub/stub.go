package stub

import (
	"context"
	"io"

	"nautilus/internal/errors"
	"nautilus/internal/ocr"
)

// OCR intentionally performs no extraction until a real provider is configured.
type OCR struct{}

var _ ocr.OCR = OCR{}

func (OCR) Extract(ctx context.Context, _ io.Reader, _ string) (string, error) {
	return "", errors.Wrap(ctx.Err(), "OCR canceled")
}
