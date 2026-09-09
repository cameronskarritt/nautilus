package documentfiles

import (
	"context"
	"io"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
)

// ReadContent decrypts the original file or canonical PDF. The caller must clear the returned plaintext.
func ReadContent(ctx context.Context, store objectstore.Store, doc *documents.Document) ([]byte, error) {
	key := doc.ObjectKey
	if doc.PageCount > 0 {
		if doc.PDFKey == "" {
			return nil, errors.New("document PDF is unavailable")
		}
		key = doc.PDFKey
	}
	plaintext, err := read(ctx, store, key, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	if err != nil {
		return nil, err
	}
	if int64(len(plaintext)) != doc.Size {
		clear(plaintext)
		return nil, errors.New("document size does not match content")
	}
	return plaintext, nil
}

func read(ctx context.Context, store objectstore.Store, key string, binding encrypt.Binding) ([]byte, error) {
	object, err := store.Get(ctx, key, nil)
	if err != nil {
		return nil, err
	}
	defer object.Body.Close()
	const maxEnvelope = encrypt.MaxPlaintextSize + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxEnvelope+1))
	if err != nil {
		return nil, errors.Wrap(err, "unable to read encrypted document")
	}
	if len(ciphertext) > maxEnvelope {
		return nil, errors.New("encrypted document exceeds size limit")
	}
	return encrypt.FromContext(ctx).Open(ctx, ciphertext, binding)
}
