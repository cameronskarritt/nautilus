package documents

import (
	"context"
	"strings"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

func ListPages(ctx context.Context, db database.Database, orgID int, externalID string) ([]Page, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	pages := make([]Page, 0)
	id, err := uuid.Parse(externalID)
	if err != nil {
		return pages, nil
	}
	rows, err := db.Query(ctx, `SELECT p.number, p.content_type, p.size, p.object_key
 FROM document_pages p JOIN documents d ON d.organization_id = p.organization_id AND d.id = p.document_id
 JOIN organizations o ON o.id = d.organization_id
 WHERE d.organization_id = $1 AND d.external_id = $2 AND o.deleted_at IS NULL ORDER BY p.number`, orgID, id.String())
	if err != nil {
		return nil, errors.Wrap(err, "unable to list document pages")
	}
	err = database.ScanRows(rows, func(row database.Row) error {
		var page Page
		if err := row.Scan(&page.Number, &page.ContentType, &page.Size, &page.ObjectKey); err != nil {
			return errors.Wrap(err, "unable to read document page")
		}
		pages = append(pages, page)
		return nil
	})
	return pages, err
}

// PublishPDF lets only the first eligible attempt publish its immutable artifact.
func PublishPDF(ctx context.Context, db database.Database, orgID int, externalID, key string, size int64) (*Document, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if size <= 0 || size > 100<<20 {
		return nil, ErrInvalidSize
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	prefix := "documents/" + id.String() + "/pdf/"
	hash, ok := strings.CutPrefix(key, prefix)
	if !ok || len(hash) != 64 || strings.Trim(hash, "0123456789abcdef") != "" {
		return nil, ErrInvalidPDFKey
	}
	query := `UPDATE documents SET pdf_key = $3, size = $4, sha256 = $7, status = $5, updated_at = CURRENT_TIMESTAMP
 WHERE organization_id = $1 AND external_id = $2 AND status = $6 AND page_count > 0 AND content_type = 'application/pdf'
 AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)
 RETURNING ` + columns
	return scan(db.QueryRow(ctx, query, orgID, id.String(), key, size, enums.DocumentStatusUploaded, enums.DocumentStatusUploading, hash))
}
