package documentfiles

import (
	"context"
	"unicode/utf8"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
)

// ReadText decrypts document OCR text. The caller must clear the returned plaintext.
func ReadText(ctx context.Context, store objectstore.Store, doc *documents.Document) ([]byte, error) {
	plaintext, err := read(ctx, store, doc.ObjectKey+"/ocr", encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(plaintext) {
		clear(plaintext)
		return nil, errors.New("document text is not valid UTF-8")
	}
	return plaintext, nil
}
