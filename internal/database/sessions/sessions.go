package sessions

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

const sessionTTL = 30 * 24 * 60 * 60

func RequestMetadata(r *http.Request) *SessionMetadata {
	meta := new(SessionMetadata)

	if addr := r.RemoteAddr; addr != "" {
		meta.Addr = optional.Set(addr)
	}

	if ua := r.UserAgent(); ua != "" {
		meta.UserAgent = optional.Set(ua)
	}

	return meta
}

func Create(
	ctx context.Context,
	db database.Database,
	userID int,
	orgMemberID optional.Optional[int],
	meta *SessionMetadata,
) (*Session, error) {
	expiresAt := time.Now().Add(sessionTTL * time.Second)
	token, hash, err := generateToken(64)
	if err != nil {
		return nil, err
	}
	var addr, ua optional.Optional[string]
	if meta != nil {
		addr = meta.Addr
		ua = meta.UserAgent
	}

	query := `INSERT INTO sessions(user_id, org_member_id, token_hash, expires_at, ip_addr, user_agent) VALUES ($1, $2, $3, $4, $5, $6);`
	_, err = db.Exec(ctx, query, userID, orgMemberID, hash, expiresAt, addr, ua)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create session")
	}

	session := &Session{
		Token:       token,
		OrgMemberID: orgMemberID,
		ExpiresAt:   expiresAt,
	}
	return session, nil
}

func Get(ctx context.Context, db database.Database, token string) (*Session, error) {
	hash, err := hashToken(token)
	if err != nil {
		return nil, err
	}

	query := `SELECT id, user_id, org_member_id, assumed_by, assumed_org_id, expires_at FROM sessions WHERE token_hash = $1 AND deleted_at IS NULL AND expires_at > CURRENT_TIMESTAMP;`
	row := db.QueryRow(ctx, query, hash)

	session := new(Session)
	err = row.Scan(&session.ID, &session.UserID, &session.OrgMemberID, &session.AssumedBy, &session.AssumedOrgID, &session.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to retrieve session")
	}

	return session, nil
}

func GetByID(ctx context.Context, db database.Database, id int) (*Session, error) {
	query := `SELECT id, user_id, org_member_id, assumed_by, assumed_org_id, expires_at FROM sessions WHERE id = $1 AND deleted_at IS NULL AND expires_at > CURRENT_TIMESTAMP;`
	row := db.QueryRow(ctx, query, id)

	session := new(Session)
	err := row.Scan(&session.ID, &session.UserID, &session.OrgMemberID, &session.AssumedBy, &session.AssumedOrgID, &session.ExpiresAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to retrieve session by id")
	}

	return session, nil
}

func Touch(ctx context.Context, db database.Database, token string) (*Session, error) {
	hash, err := hashToken(token)
	if err != nil {
		return nil, err
	}

	expiresAt := time.Now().Add(sessionTTL * time.Second)
	query := `UPDATE sessions SET expires_at = $2 WHERE token_hash = $1 AND deleted_at IS NULL;`
	_, err = db.Exec(ctx, query, hash, expiresAt)
	if err != nil {
		return nil, errors.Wrap(err, "unable to touch session")
	}

	session := &Session{
		Token:     token,
		ExpiresAt: expiresAt,
	}

	return session, nil
}

func End(ctx context.Context, db database.Database, id int) error {
	query := `UPDATE sessions SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL;`
	_, err := db.Exec(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "unable to end session")
	}

	return nil
}

func EndAllExcept(ctx context.Context, db database.Database, userID int, sessionID int) error {
	query := `UPDATE sessions SET deleted_at = CURRENT_TIMESTAMP WHERE user_id = $1 AND deleted_at IS NULL AND id != $2;`
	_, err := db.Exec(ctx, query, userID, sessionID)
	if err != nil {
		return errors.Wrap(err, "unable to end all sessions")
	}

	return nil
}

func EndAll(ctx context.Context, db database.Database, userID int) error {
	// Bit of a hack around EndAllExcept, but I don't want a duplicate query
	return EndAllExcept(ctx, db, userID, -1)
}

// SwitchOrg updates the session's current organization context
func SwitchOrg(ctx context.Context, db database.Database, sessionID int, orgMemberID int) error {
	query := `UPDATE sessions SET org_member_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`
	_, err := db.Exec(ctx, query, orgMemberID, sessionID)
	if err != nil {
		return errors.Wrap(err, "unable to switch organization")
	}

	return nil
}

// AssumeOrg sets the assumed org for an admin session
func AssumeOrg(ctx context.Context, db database.Database, sessionID int, orgID int) error {
	query := `UPDATE sessions SET assumed_org_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;`
	_, err := db.Exec(ctx, query, orgID, sessionID)
	if err != nil {
		return errors.Wrap(err, "unable to assume organization")
	}

	return nil
}

// UnassumeOrg clears the assumed org from a session
func UnassumeOrg(ctx context.Context, db database.Database, sessionID int) error {
	query := `UPDATE sessions SET assumed_org_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL;`
	_, err := db.Exec(ctx, query, sessionID)
	if err != nil {
		return errors.Wrap(err, "unable to unassume organization")
	}

	return nil
}
