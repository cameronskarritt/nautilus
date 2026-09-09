package documents

import (
	"context"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

// ClaimUpload is a system-wide maintenance scan. The token fences expired writers
// and earlier recovery attempts; the immutable ID cursor bounds each sweep.
func ClaimUpload(ctx context.Context, db database.Database, after int) (*Document, error) {
	return scan(db.QueryRow(ctx, `UPDATE documents SET upload_token = $2,
 upload_expires_at = CURRENT_TIMESTAMP + INTERVAL '15 minutes'
 WHERE id = (SELECT d.id FROM documents d JOIN organizations o ON o.id = d.organization_id
 WHERE d.id > $1 AND d.status = 'uploading' AND d.upload_expires_at < CURRENT_TIMESTAMP
 AND o.deleted_at IS NULL ORDER BY d.id LIMIT 1 FOR UPDATE OF d SKIP LOCKED)
 RETURNING `+columns, after, uuid.NewV4().String()))
}

// ReadyUpload hands immutable source objects to the workflow. An expired writer
// cannot start a workflow after recovery has taken ownership of its upload.
func ReadyUpload(ctx context.Context, db database.Database, doc *Document) (bool, error) {
	result, err := db.Exec(ctx, `UPDATE documents SET upload_ready = TRUE
 WHERE organization_id = $1 AND external_id = $2 AND upload_token = $3
 AND status = 'uploading' AND upload_expires_at > CURRENT_TIMESTAMP
 AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, doc.OrganizationID, doc.ExternalID, doc.UploadToken)
	if err != nil {
		return false, errors.Wrap(err, "unable to ready document upload")
	}
	return result.RowsAffected() > 0, nil
}

// ReleaseUpload leaves ambiguous PUT results for inspection, including a final
// PUT accepted by storage whose response never reached the handler.
func ReleaseUpload(ctx context.Context, db database.Database, doc *Document) error {
	_, err := db.Exec(ctx, `UPDATE documents SET upload_expires_at = CURRENT_TIMESTAMP
 WHERE organization_id = $1 AND external_id = $2 AND upload_token = $3
 AND status = 'uploading' AND upload_ready = FALSE`, doc.OrganizationID, doc.ExternalID, doc.UploadToken)
	return errors.Wrap(err, "unable to release document upload")
}

func FailUpload(ctx context.Context, db database.Database, doc *Document) error {
	_, err := db.Exec(ctx, `UPDATE documents SET status = 'failed', updated_at = CURRENT_TIMESTAMP
 WHERE organization_id = $1 AND external_id = $2 AND upload_token = $3
 AND status = 'uploading' AND upload_ready = FALSE AND upload_expires_at > CURRENT_TIMESTAMP
 AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`, doc.OrganizationID, doc.ExternalID, doc.UploadToken)
	return errors.Wrap(err, "unable to fail abandoned document upload")
}
