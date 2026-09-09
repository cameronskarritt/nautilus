package documenttext

import (
	"context"
	"io"
	"unicode/utf8"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
)

// Read decrypts document OCR text. The caller must clear the returned plaintext.
func Read(ctx context.Context, store objectstore.Store, doc *documents.Document) ([]byte, error) {
	object, err := store.Get(ctx, doc.ObjectKey+"/ocr", nil)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	const maxEnvelope = encrypt.MaxPlaintextSize + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxEnvelope+1))
	if err != nil {
		return nil, errors.Wrap(err, "unable to read encrypted document text")
	}
	if len(ciphertext) > maxEnvelope {
		return nil, errors.New("encrypted document text exceeds size limit")
	}
	plaintext, err := encrypt.FromContext(ctx).Open(ctx, ciphertext, encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(plaintext) {
		clear(plaintext)
		return nil, errors.New("document text is not valid UTF-8")
	}
	return plaintext, nil
}
