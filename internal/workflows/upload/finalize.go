package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/scan"
)

func (a Activities) Finalize(ctx context.Context, input Input) (err error) {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx, stop := heartbeat(ctx)
	defer stop()
	doc, err := documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
	if err != nil {
		return uploadFailure("unable to read document", false)
	}
	if doc == nil || (doc.Status != enums.DocumentStatusUploading && doc.Status != enums.DocumentStatusUploaded) {
		return uploadFailure("document upload unavailable", true)
	}
	// A retry after committing completion must not replace the canonical artifact.
	if doc.Status == enums.DocumentStatusUploaded {
		return nil
	}
	if doc.PageCount > 0 {
		defer func() {
			var failure *temporal.ApplicationError
			if ctx.Err() == nil && errors.As(err, &failure) && failure.NonRetryable() {
				if markErr := documents.MarkFailed(ctx, a.DB, input.OrganizationID, input.DocumentID); markErr != nil {
					err = uploadFailure("unable to mark document upload failed", false)
				}
			}
		}()

		org, err := organizations.Get(ctx, a.DB, input.OrganizationID)
		if err != nil {
			return uploadFailure("unable to read organization", false)
		}
		if org == nil {
			return uploadFailure("organization unavailable", true)
		}
		enc := encrypt.ForOrganization(a.Keys, org.ExternalID)
		images, _, err := a.sourcePages(ctx, doc, enc)
		if err != nil {
			return err
		}
		defer func() {
			for _, data := range images {
				clear(data)
			}
		}()
		pdf, err := scan.PDF(ctx, images)
		if err != nil {
			return uploadFailure("unable to create document PDF", errors.Is(err, scan.ErrTooLarge) || errors.Is(err, scan.ErrInvalidImage))
		}
		defer clear(pdf)
		encrypted, err := enc.Seal(ctx, pdf, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
		if err != nil {
			return uploadFailure("unable to encrypt document PDF", false)
		}
		current, err := documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
		if err != nil {
			return uploadFailure("unable to read document", false)
		}
		if current == nil || (current.Status != enums.DocumentStatusUploading && current.Status != enums.DocumentStatusUploaded) {
			return uploadFailure("document upload unavailable", true)
		}
		if current.Status == enums.DocumentStatusUploaded {
			return nil
		}
		digest := sha256.Sum256(pdf)
		key := doc.ObjectKey + "/pdf/" + hex.EncodeToString(digest[:])
		if err := a.Store.Put(ctx, key, bytes.NewReader(encrypted), &objectstore.PutOptions{ContentType: optional.Set("application/octet-stream")}); err != nil {
			return uploadFailure("unable to store document PDF", false)
		}
		published, err := documents.PublishPDF(ctx, a.DB, input.OrganizationID, input.DocumentID, key, int64(len(pdf)))
		if err != nil {
			return uploadFailure("unable to publish document PDF", false)
		}
		if published != nil {
			return nil
		}
		current, err = documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
		if err != nil {
			return uploadFailure("unable to read document", false)
		}
		if current == nil || current.Status != enums.DocumentStatusUploaded {
			return uploadFailure("document upload unavailable", true)
		}
		return nil
	}
	doc, err = documents.MarkUploaded(ctx, a.DB, input.OrganizationID, input.DocumentID)
	if err != nil {
		return uploadFailure("unable to finalize document upload", false)
	}
	if doc == nil {
		return uploadFailure("document upload unavailable", true)
	}
	return nil
}
