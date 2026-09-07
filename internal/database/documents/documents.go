package documents

import (
	"context"
	"database/sql"
	"mime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/pagination"
)

var (
	ErrInvalidOrganization = errors.New("invalid document organization")
	ErrInvalidFilename     = errors.New("invalid document filename")
	ErrInvalidContentType  = errors.New("invalid document content type")
	ErrInvalidSize         = errors.New("invalid document size")
	ErrInvalidCursor       = errors.New("invalid document cursor")
)

const columns = `id, external_id, organization_id, object_key, status, filename, content_type, size, created_at, updated_at`

func Create(ctx context.Context, db database.Database, orgID int, opts *CreateOptions) (*Document, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	if opts == nil {
		return nil, ErrInvalidFilename
	}
	filename := strings.TrimSpace(opts.Filename)
	if filename == "" || !utf8.ValidString(filename) || utf8.RuneCountInString(filename) > 255 || strings.ContainsFunc(filename, unicode.IsControl) {
		return nil, ErrInvalidFilename
	}
	contentType := strings.TrimSpace(opts.ContentType)
	if len(contentType) == 0 || len(contentType) > 255 {
		return nil, ErrInvalidContentType
	}
	contentType, _, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.Contains(contentType, "/") || strings.Contains(contentType, "*") {
		return nil, ErrInvalidContentType
	}
	if opts.Size < 0 {
		return nil, ErrInvalidSize
	}
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "unable to generate document ID")
	}
	externalID := id.String()
	query := `
		INSERT INTO documents(external_id, organization_id, object_key, status, filename, content_type, size)
		SELECT $1, id, $3, 'pending', $4, $5, $6 FROM organizations WHERE id = $2 AND deleted_at IS NULL
		RETURNING ` + columns
	return scan(db.QueryRow(ctx, query, externalID, orgID, "documents/"+externalID, filename, contentType, opts.Size))
}

// MarkReady publishes a completed upload exactly once within its active organization.
func MarkReady(ctx context.Context, db database.Database, orgID int, externalID string) (*Document, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	query := `
		UPDATE documents SET status = 'ready', updated_at = CURRENT_TIMESTAMP
		WHERE organization_id = $1 AND external_id = $2 AND status = 'pending'
		  AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)
		RETURNING ` + columns
	return scan(db.QueryRow(ctx, query, orgID, id.String()))
}

func GetByExternalID(ctx context.Context, db database.Database, orgID int, externalID string) (*Document, error) {
	if orgID <= 0 {
		return nil, ErrInvalidOrganization
	}
	id, err := uuid.Parse(externalID)
	if err != nil {
		return nil, nil
	}
	query := `SELECT ` + columns + ` FROM documents
		WHERE organization_id = $1 AND external_id = $2 AND status = 'ready'
		  AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`
	return scan(db.QueryRow(ctx, query, orgID, id.String()))
}

func List(ctx context.Context, db database.Database, orgID int, params pagination.Params) (pagination.Page[*Document], error) {
	if orgID <= 0 {
		return pagination.Page[*Document]{}, ErrInvalidOrganization
	}
	limit := params.Limit
	if limit <= 0 {
		limit = pagination.DefaultLimit
	}
	limit = min(limit, 100)
	query := `SELECT ` + columns + ` FROM documents
		WHERE organization_id = $1 AND status = 'ready'
		  AND EXISTS (SELECT 1 FROM organizations WHERE id = $1 AND deleted_at IS NULL)`
	args := []any{orgID, limit + 1}
	if params.Cursor != nil {
		id, ok := params.Cursor["id"].(string)
		organization, orgOK := params.Cursor["organization_id"].(string)
		n, err := strconv.Atoi(id)
		if len(params.Cursor) != 2 || !ok || !orgOK || organization != strconv.Itoa(orgID) || err != nil || n <= 0 || strconv.Itoa(n) != id {
			return pagination.Page[*Document]{}, ErrInvalidCursor
		}
		query += ` AND id < $3`
		args = append(args, n)
	}
	query += ` ORDER BY id DESC LIMIT $2`
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return pagination.Page[*Document]{}, errors.Wrap(err, "unable to list documents")
	}
	docs := make([]*Document, 0, limit+1)
	err = database.ScanRows(rows, func(row database.Row) error {
		doc, err := scan(row)
		if err != nil {
			return err
		}
		docs = append(docs, doc)
		return nil
	})
	if err != nil {
		return pagination.Page[*Document]{}, err
	}
	return pagination.Build(docs, limit, func(doc *Document) pagination.Cursor {
		return pagination.Cursor{"id": strconv.Itoa(doc.ID), "organization_id": strconv.Itoa(orgID)}
	}), nil
}

func scan(row database.Row) (*Document, error) {
	doc := new(Document)
	if err := row.Scan(&doc.ID, &doc.ExternalID, &doc.OrganizationID, &doc.ObjectKey, &doc.Status,
		&doc.Filename, &doc.ContentType, &doc.Size, &doc.CreatedAt, &doc.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to read document")
	}
	return doc, nil
}
