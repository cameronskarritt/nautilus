package documentdownloads

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"slices"
	"strings"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/oauth"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

const tokenPrefix = "mcp_dl_"

func Create(ctx context.Context, db database.Database, organizationID, documentID int, opts *CreateOptions) (string, time.Time, error) {
	allowed, err := authorized(ctx, db, organizationID, opts)
	if err != nil {
		return "", time.Time{}, err
	}
	if !allowed || documentID <= 0 {
		return "", time.Time{}, ErrInvalidDownload
	}
	var data [32]byte
	_, _ = rand.Read(data[:])
	token := tokenPrefix + base64.RawURLEncoding.EncodeToString(data[:])
	hash := sha256.Sum256([]byte(token))
	now := time.Now()
	expires := now.Add(5 * time.Minute)
	_, err = db.Exec(ctx, `DELETE FROM document_downloads WHERE expires_at <= $1`, now)
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "unable to remove expired document downloads")
	}
	var keyID, accessHash any
	if opts.APIKeyID > 0 {
		keyID = opts.APIKeyID
	} else {
		accessHash = opts.OAuthAccessHash
	}
	err = db.QueryRow(ctx, `
		INSERT INTO document_downloads(token_hash, organization_id, document_id, api_key_id, oauth_access_hash, resource, expires_at)
		SELECT $1, d.organization_id, d.id, $4, $5, $6, $7
		FROM documents d JOIN organizations o ON o.id = d.organization_id
		WHERE d.organization_id = $2 AND d.id = $3 AND d.status = $8 AND o.deleted_at IS NULL
		RETURNING expires_at
	`, hash[:], organizationID, documentID, keyID, accessHash, opts.Resource, expires, enums.DocumentStatusUploaded).Scan(&expires)
	if errors.Is(err, sql.ErrNoRows) {
		return "", time.Time{}, ErrInvalidDownload
	}
	if err != nil {
		return "", time.Time{}, errors.Wrap(err, "unable to create document download")
	}
	return token, expires, nil
}

func Resolve(ctx context.Context, db database.Database, token, resource string) (*Download, error) {
	encoded, ok := strings.CutPrefix(token, tokenPrefix)
	if !ok || len(encoded) != 43 {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) != 32 {
		return nil, nil
	}
	hash := sha256.Sum256([]byte(token))
	download := new(Download)
	opts := &CreateOptions{Resource: resource}
	var keyID sql.NullInt64
	err = db.QueryRow(ctx, `
		SELECT d.organization_id, d.external_id, dl.api_key_id, dl.oauth_access_hash
		FROM document_downloads dl
		JOIN documents d ON d.organization_id = dl.organization_id AND d.id = dl.document_id
		JOIN organizations o ON o.id = d.organization_id
		WHERE dl.token_hash = $1 AND dl.resource = $2 AND dl.expires_at > $3
			AND d.status = $4 AND o.deleted_at IS NULL
	`, hash[:], resource, time.Now(), enums.DocumentStatusUploaded).Scan(&download.OrganizationID, &download.DocumentID, &keyID, &opts.OAuthAccessHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to resolve document download")
	}
	if keyID.Valid {
		opts.APIKeyID = int(keyID.Int64)
	}
	allowed, err := authorized(ctx, db, download.OrganizationID, opts)
	if err != nil {
		return nil, err
	}
	if !allowed {
		return nil, nil
	}
	return download, nil
}

func authorized(ctx context.Context, db database.Database, organizationID int, opts *CreateOptions) (bool, error) {
	if organizationID <= 0 || opts == nil || opts.Resource == "" || opts.APIKeyID < 0 ||
		(opts.APIKeyID > 0) == (len(opts.OAuthAccessHash) > 0) {
		return false, nil
	}
	if opts.APIKeyID > 0 {
		key, err := apikeys.Get(ctx, db, organizationID, opts.APIKeyID)
		if err != nil {
			return false, err
		}
		return key != nil && slices.Contains(key.Scopes, enums.ScopeRead), nil
	}
	grant, err := oauth.AuthenticateHash(ctx, db, opts.OAuthAccessHash, opts.Resource)
	if err != nil {
		return false, err
	}
	return grant != nil && grant.OrganizationID == organizationID &&
		slices.Contains(strings.Fields(grant.Scope), string(enums.ScopeRead)), nil
}
