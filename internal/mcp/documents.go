package mcp

import (
	"context"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/users"
	"nautilus/internal/documenttext"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/kms"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/pagination"
)

type documentTools struct {
	db    database.Database
	store objectstore.Store
	keys  kms.KeyManager
}

type listDocumentsInput struct {
	Limit  int    `json:"limit,omitempty" jsonschema:"Maximum documents to return, from 1 to 100; defaults to 50."`
	Cursor string `json:"cursor,omitempty" jsonschema:"Opaque next_cursor from the preceding list response."`
}

type documentInput struct {
	DocumentID string `json:"document_id" jsonschema:"Document ID returned by list_documents."`
}

type documentOutput struct {
	Document *documents.Document `json:"document"`
}

type readDocumentInput struct {
	DocumentID string `json:"document_id" jsonschema:"Document ID returned by list_documents."`
	Offset     int    `json:"offset,omitempty" jsonschema:"UTF-8 byte offset; defaults to 0. Use next_offset from the preceding response."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum UTF-8 bytes to return, from 4 to 65536; defaults to 16384."`
}

type readDocumentOutput struct {
	DocumentID string `json:"document_id"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	TotalBytes int    `json:"total_bytes"`
	NextOffset int    `json:"next_offset,omitempty"`
	HasMore    bool   `json:"has_more"`
}

func readOrganization(ctx context.Context) (*organizations.Organization, error) {
	org := organizations.FromContext(ctx)
	if org == nil || org.ID <= 0 || org.ExternalID == "" {
		return nil, errors.New("Document access denied")
	}
	if key := apikeys.FromContext(ctx); key != nil {
		if key.ID <= 0 || key.OrganizationID != org.ID || !slices.Contains(key.Scopes, enums.ScopeRead) {
			return nil, errors.New("Read scope is required to access documents")
		}
		return org, nil
	}
	grant := oauth.FromContext(ctx)
	user := users.FromContext(ctx)
	member := organizations.MemberFromContext(ctx)
	if grant == nil || user == nil || member == nil || user.ID <= 0 || member.ID <= 0 ||
		grant.UserID != user.ID || grant.MemberID != member.ID || grant.OrganizationID != org.ID ||
		member.UserID != user.ID || member.OrganizationID != org.ID || !member.Role.IsValid() {
		return nil, errors.New("Document access denied")
	}
	if !slices.Contains(strings.Fields(grant.Scope), string(enums.ScopeRead)) {
		return nil, errors.New("Read scope is required to access documents")
	}
	return org, nil
}

func (d *documentTools) list(ctx context.Context, _ *mcp.CallToolRequest, in listDocumentsInput) (*mcp.CallToolResult, pagination.Page[*documents.Document], error) {
	var empty pagination.Page[*documents.Document]
	org, err := readOrganization(ctx)
	if err != nil {
		return nil, empty, err
	}
	if in.Limit == 0 {
		in.Limit = pagination.DefaultLimit
	}
	if in.Limit < 1 || in.Limit > 100 {
		return nil, empty, errors.New("Limit must be between 1 and 100")
	}
	if len(in.Cursor) > 4096 {
		return nil, empty, errors.New("Invalid document cursor")
	}
	cursor, err := pagination.Decode(in.Cursor)
	if err != nil || in.Cursor != "" && cursor == nil {
		return nil, empty, errors.New("Invalid document cursor")
	}
	page, err := documents.List(ctx, d.db, org.ID, pagination.Params{Limit: in.Limit, Cursor: cursor})
	if errors.Is(err, documents.ErrInvalidCursor) {
		return nil, empty, errors.New("Invalid document cursor")
	}
	if err != nil {
		return nil, empty, documentError(ctx, err)
	}
	return nil, page, nil
}

func (d *documentTools) document(ctx context.Context, orgID int, id string) (*documents.Document, error) {
	doc, err := documents.GetByExternalID(ctx, d.db, orgID, id)
	if err != nil {
		return nil, documentError(ctx, err)
	}
	if doc == nil {
		return nil, errors.New("Document not found")
	}
	return doc, nil
}

func (d *documentTools) get(ctx context.Context, _ *mcp.CallToolRequest, in documentInput) (*mcp.CallToolResult, documentOutput, error) {
	org, err := readOrganization(ctx)
	if err != nil {
		return nil, documentOutput{}, err
	}
	doc, err := d.document(ctx, org.ID, in.DocumentID)
	return nil, documentOutput{Document: doc}, err
}

func (d *documentTools) read(ctx context.Context, _ *mcp.CallToolRequest, in readDocumentInput) (*mcp.CallToolResult, readDocumentOutput, error) {
	var empty readDocumentOutput
	org, err := readOrganization(ctx)
	if err != nil {
		return nil, empty, err
	}
	if in.Limit == 0 {
		in.Limit = 16384
	}
	if in.Offset < 0 || in.Limit < 4 || in.Limit > 65536 {
		return nil, empty, errors.New("Offset must be nonnegative and limit must be between 4 and 65536 bytes")
	}
	doc, err := d.document(ctx, org.ID, in.DocumentID)
	if err != nil {
		return nil, empty, err
	}
	if doc.Status != enums.DocumentStatusUploaded {
		return nil, empty, errors.New("Document text unavailable; extraction may still be pending")
	}
	if d.store == nil || d.keys == nil {
		return nil, empty, errors.New("Document reads are unavailable")
	}
	ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(d.keys, org.ExternalID))
	plaintext, err := documenttext.Read(ctx, d.store, doc)
	if errors.Is(err, objectstore.ErrNotFound) {
		return nil, empty, errors.New("Document text unavailable; extraction may still be pending")
	}
	if err != nil {
		return nil, empty, documentError(ctx, err)
	}
	defer clear(plaintext)
	if in.Offset > len(plaintext) || in.Offset < len(plaintext) && !utf8.RuneStart(plaintext[in.Offset]) {
		return nil, empty, errors.New("Offset must be a UTF-8 character boundary within the document")
	}
	end := in.Offset + min(in.Limit, len(plaintext)-in.Offset)
	for end < len(plaintext) && !utf8.RuneStart(plaintext[end]) {
		end--
	}
	out := readDocumentOutput{
		DocumentID: doc.ExternalID, Text: string(plaintext[in.Offset:end]),
		Offset: in.Offset, TotalBytes: len(plaintext), HasMore: end < len(plaintext),
	}
	if out.HasMore {
		out.NextOffset = end
	}
	return nil, out, nil
}

func documentError(ctx context.Context, err error) error {
	log.FromContext(ctx).Error("MCP document operation failed", "error", err)
	return errors.New("Unable to read documents")
}
