package oauth

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
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

func secret(prefix string) string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return prefix + base64.RawURLEncoding.EncodeToString(b[:])
}

func hash(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}

func validScope(scope string) bool {
	fields := strings.Fields(scope)
	if len(fields) == 0 || strings.Join(fields, " ") != scope {
		return false
	}
	for i, field := range fields {
		if !enums.Scope(field).IsValid() || slices.Contains(fields[:i], field) {
			return false
		}
	}
	return true
}

func allows(role enums.Role, scope string) bool {
	return role.IsValid() && validScope(scope) &&
		(role != enums.RoleViewer || !slices.Contains(strings.Fields(scope), string(enums.ScopeWrite)))
}

func RegisterClient(ctx context.Context, db database.Database, name string, redirectURIs []string) (*Client, error) {
	if len(redirectURIs) == 0 {
		return nil, ErrInvalidClient
	}
	c := &Client{ID: secret("mcp_client_"), Name: name, RedirectURIs: redirectURIs}
	_, err := db.Exec(ctx, `INSERT INTO mcp_oauth_clients(id, name, redirect_uris) VALUES ($1, $2, $3)`, c.ID, c.Name, c.RedirectURIs)
	if err != nil {
		return nil, errors.Wrap(err, "unable to register OAuth client")
	}
	return c, nil
}

func GetClient(ctx context.Context, db database.Database, id string) (*Client, error) {
	c := new(Client)
	err := db.QueryRow(ctx, `SELECT id, name, redirect_uris FROM mcp_oauth_clients WHERE id = $1`, id).Scan(&c.ID, &c.Name, &c.RedirectURIs)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to get OAuth client")
	}
	return c, nil
}

func membership(ctx context.Context, db database.Database, userID, memberID int) (enums.Role, error) {
	var role enums.Role
	err := db.QueryRow(ctx, `SELECT m.role FROM org_members m
		JOIN users u ON u.id = m.user_id JOIN organizations o ON o.id = m.organization_id
		WHERE m.id = $1 AND m.user_id = $2 AND m.deleted_at IS NULL AND u.deleted_at IS NULL AND o.deleted_at IS NULL`, memberID, userID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidGrant
	}
	if err != nil {
		return "", errors.Wrap(err, "unable to validate OAuth membership")
	}
	return role, nil
}

func CreateGrant(ctx context.Context, db database.Database, userID, memberID int, opts *GrantOptions) (string, error) {
	if opts == nil || !validScope(opts.Scope) {
		return "", ErrInvalidScope
	}
	challenge, err := base64.RawURLEncoding.DecodeString(opts.Challenge)
	if err != nil || len(challenge) != 32 || opts.Resource == "" {
		return "", ErrInvalidGrant
	}
	c, err := GetClient(ctx, db, opts.ClientID)
	if err != nil {
		return "", err
	}
	if c == nil || !slices.Contains(c.RedirectURIs, opts.RedirectURI) {
		return "", ErrInvalidClient
	}
	code := secret("mcp_code_")
	err = database.Transact(ctx, db, func(tx database.Database) error {
		role, err := membership(ctx, tx, userID, memberID)
		if err != nil {
			return err
		}
		if !allows(role, opts.Scope) {
			return ErrInvalidScope
		}
		var id string
		now := time.Now()
		err = tx.QueryRow(ctx, `INSERT INTO mcp_oauth_grants(client_id, user_id, member_id, scope, resource, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`, opts.ClientID, userID, memberID, opts.Scope, opts.Resource, now.Add(30*24*time.Hour)).Scan(&id)
		if err != nil {
			return errors.Wrap(err, "unable to create OAuth grant")
		}
		_, err = tx.Exec(ctx, `INSERT INTO mcp_oauth_codes(hash, grant_id, redirect_uri, challenge, expires_at) VALUES ($1, $2, $3, $4, $5)`, hash(code), id, opts.RedirectURI, opts.Challenge, now.Add(5*time.Minute))
		return errors.Wrap(err, "unable to create OAuth code")
	})
	if err != nil {
		return "", err
	}
	return code, nil
}

// Lock the family before reading token state so concurrent rotations observe the
// committed consumption and revoke the family on replay.
func lockGrant(ctx context.Context, db database.Database, clientID string, tokenHash []byte, code bool) (*Grant, error) {
	var id string
	query := `SELECT grant_id FROM mcp_oauth_tokens WHERE refresh_hash = $1 OR access_hash = $1`
	if code {
		query = `SELECT grant_id FROM mcp_oauth_codes WHERE hash = $1`
	}
	err := db.QueryRow(ctx, query, tokenHash).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to find OAuth grant")
	}
	g := new(Grant)
	var expires time.Time
	var revoked sql.NullTime
	err = db.QueryRow(ctx, `SELECT id, client_id, user_id, member_id, scope, resource, expires_at, revoked_at
		FROM mcp_oauth_grants WHERE id = $1 AND client_id = $2 FOR UPDATE`, id, clientID).Scan(&g.ID, &g.ClientID, &g.UserID, &g.MemberID, &g.Scope, &g.Resource, &expires, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInvalidGrant
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to lock OAuth grant")
	}
	if revoked.Valid || !expires.After(time.Now()) {
		return nil, ErrInvalidGrant
	}
	return g, nil
}

func revokeGrant(ctx context.Context, db database.Database, id string) error {
	_, err := db.Exec(ctx, `UPDATE mcp_oauth_grants SET revoked_at = CURRENT_TIMESTAMP WHERE id = $1`, id)
	return errors.Wrap(err, "unable to revoke OAuth grant")
}

func issue(ctx context.Context, db database.Database, g *Grant, scope string) (*Tokens, error) {
	role, err := membership(ctx, db, g.UserID, g.MemberID)
	if err != nil {
		return nil, err
	}
	if !allows(role, scope) {
		return nil, ErrInvalidGrant
	}
	t := &Tokens{AccessToken: secret("mcp_at_"), RefreshToken: secret("mcp_rt_"), TokenType: "Bearer", ExpiresIn: 3600, Scope: scope}
	_, err = db.Exec(ctx, `INSERT INTO mcp_oauth_tokens(access_hash, refresh_hash, grant_id, scope, access_expires_at)
		VALUES ($1, $2, $3, $4, $5)`, hash(t.AccessToken), hash(t.RefreshToken), g.ID, scope, time.Now().Add(time.Hour))
	if err != nil {
		return nil, errors.Wrap(err, "unable to issue OAuth tokens")
	}
	return t, nil
}

func ExchangeCode(ctx context.Context, db database.Database, clientID, code, redirectURI, resource, verifier string) (*Tokens, error) {
	if len(verifier) < 43 || len(verifier) > 128 || strings.ContainsAny(verifier, " \t\r\n") {
		return nil, ErrInvalidGrant
	}
	for _, c := range verifier {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && !strings.ContainsRune("-._~", c) {
			return nil, ErrInvalidGrant
		}
	}
	var tokens *Tokens
	var replay bool
	err := database.Transact(ctx, db, func(tx database.Database) error {
		g, err := lockGrant(ctx, tx, clientID, hash(code), true)
		if err != nil {
			return err
		}
		var redirect, challenge string
		var expires time.Time
		var used sql.NullTime
		err = tx.QueryRow(ctx, `SELECT redirect_uri, challenge, expires_at, used_at FROM mcp_oauth_codes WHERE hash = $1`, hash(code)).Scan(&redirect, &challenge, &expires, &used)
		if err != nil {
			return errors.Wrap(err, "unable to read OAuth code")
		}
		if redirect != redirectURI || g.Resource != resource || challenge != base64.RawURLEncoding.EncodeToString(hash(verifier)) {
			return ErrInvalidGrant
		}
		if used.Valid {
			replay = true
			return revokeGrant(ctx, tx, g.ID)
		}
		if !expires.After(time.Now()) {
			return ErrInvalidGrant
		}
		_, err = tx.Exec(ctx, `UPDATE mcp_oauth_codes SET used_at = CURRENT_TIMESTAMP WHERE hash = $1`, hash(code))
		if err != nil {
			return errors.Wrap(err, "unable to consume OAuth code")
		}
		tokens, err = issue(ctx, tx, g, g.Scope)
		return err
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return nil, ErrInvalidGrant
	}
	return tokens, nil
}

func Refresh(ctx context.Context, db database.Database, clientID, refreshToken, resource, scope string) (*Tokens, error) {
	var tokens *Tokens
	var replay bool
	err := database.Transact(ctx, db, func(tx database.Database) error {
		g, err := lockGrant(ctx, tx, clientID, hash(refreshToken), false)
		if err != nil {
			return err
		}
		if g.Resource != resource {
			return ErrInvalidGrant
		}
		var previous string
		var used sql.NullTime
		err = tx.QueryRow(ctx, `SELECT scope, refresh_used_at FROM mcp_oauth_tokens WHERE refresh_hash = $1`, hash(refreshToken)).Scan(&previous, &used)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrInvalidGrant
		}
		if err != nil {
			return errors.Wrap(err, "unable to read OAuth refresh token")
		}
		if used.Valid {
			replay = true
			return revokeGrant(ctx, tx, g.ID)
		}
		if scope == "" {
			scope = previous
		}
		if !validScope(scope) {
			return ErrInvalidScope
		}
		for _, field := range strings.Fields(scope) {
			if !slices.Contains(strings.Fields(previous), field) {
				return ErrInvalidScope
			}
		}
		_, err = tx.Exec(ctx, `UPDATE mcp_oauth_tokens SET refresh_used_at = CURRENT_TIMESTAMP WHERE refresh_hash = $1`, hash(refreshToken))
		if err != nil {
			return errors.Wrap(err, "unable to consume OAuth refresh token")
		}
		tokens, err = issue(ctx, tx, g, scope)
		return err
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return nil, ErrInvalidGrant
	}
	return tokens, nil
}

func Authenticate(ctx context.Context, db database.Database, accessToken, resource string) (*Grant, error) {
	encoded, ok := strings.CutPrefix(accessToken, "mcp_at_")
	if !ok || len(encoded) != 43 {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(encoded)
	if err != nil || len(decoded) != 32 {
		return nil, nil
	}
	return AuthenticateHash(ctx, db, hash(accessToken), resource)
}

// AuthenticateHash validates a stored reference to an exact OAuth access token.
func AuthenticateHash(ctx context.Context, db database.Database, accessHash []byte, resource string) (*Grant, error) {
	if len(accessHash) != sha256.Size {
		return nil, nil
	}
	g := new(Grant)
	var role enums.Role
	err := db.QueryRow(ctx, `SELECT g.id, g.client_id, g.user_id, g.member_id, m.organization_id, t.scope, g.resource, m.role
		FROM mcp_oauth_tokens t JOIN mcp_oauth_grants g ON g.id = t.grant_id
		JOIN org_members m ON m.id = g.member_id AND m.user_id = g.user_id
		JOIN users u ON u.id = g.user_id JOIN organizations o ON o.id = m.organization_id
		WHERE t.access_hash = $1 AND g.resource = $2 AND t.access_expires_at > $3 AND g.expires_at > $3
		AND g.revoked_at IS NULL AND m.deleted_at IS NULL AND u.deleted_at IS NULL AND o.deleted_at IS NULL`, accessHash, resource, time.Now()).Scan(&g.ID, &g.ClientID, &g.UserID, &g.MemberID, &g.OrganizationID, &g.Scope, &g.Resource, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrap(err, "unable to authenticate OAuth token")
	}
	if !allows(role, g.Scope) {
		return nil, nil
	}
	g.AccessHash = slices.Clone(accessHash)
	return g, nil
}

func Revoke(ctx context.Context, db database.Database, clientID, token string) error {
	return database.Transact(ctx, db, func(tx database.Database) error {
		g, err := lockGrant(ctx, tx, clientID, hash(token), false)
		if errors.Is(err, ErrInvalidGrant) {
			return nil
		}
		if err != nil {
			return err
		}
		return revokeGrant(ctx, tx, g.ID)
	})
}
