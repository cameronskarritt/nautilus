package organizations

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

var inviteTokenEncoding = base64.RawURLEncoding

// DefaultInviteExpiration is the default expiration time for invite tokens (7 days).
const DefaultInviteExpiration = 7 * 24 * time.Hour

// CreateInvite creates a new organization invite and returns the plaintext token.
// The token should be sent to the user; only the hash is stored in the database.
func CreateInvite(
	ctx context.Context,
	db database.Database,
	organizationID int,
	invitedBy int,
	email string,
	role Role,
	expiration time.Duration,
) (string, *Invite, error) {
	buf := make([]byte, 16)
	_, err := rand.Read(buf)
	if err != nil {
		return "", nil, errors.Wrap(err, "unable to read random source")
	}

	// Encode external token as hex (user-facing, 32 characters)
	token := hex.EncodeToString(buf)

	h := sha256.New()
	_, err = h.Write(buf)
	if err != nil {
		return "", nil, errors.Wrap(err, "unable to hash token")
	}
	hash := h.Sum(nil)

	encoded := inviteTokenEncoding.EncodeToString(hash)
	expiresAt := time.Now().Add(expiration)

	query := `
		INSERT INTO org_invites(organization_id, invited_by, email, role, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id;
	`

	var id int
	err = db.QueryRow(ctx, query, organizationID, invitedBy, email, role, encoded, expiresAt).Scan(&id)
	if err != nil {
		return "", nil, errors.Wrap(err, "unable to create org invite")
	}

	invite, err := GetInvite(ctx, db, id)
	if err != nil {
		return "", nil, err
	}

	return token, invite, nil
}

func GetInvite(ctx context.Context, db database.Database, id int) (*Invite, error) {
	invite := new(Invite)

	query := `
		SELECT id, external_id, organization_id, invited_by, email, role, expires_at, created_at, redeemed_at
		FROM org_invites
		WHERE id = $1 AND deleted_at IS NULL;
	`

	err := db.QueryRow(ctx, query, id).Scan(
		&invite.ID,
		&invite.ExternalID,
		&invite.OrganizationID,
		&invite.InvitedBy,
		&invite.Email,
		&invite.Role,
		&invite.ExpiresAt,
		&invite.CreatedAt,
		&invite.RedeemedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch org invite")
	}

	return invite, nil
}

func GetInviteByExternalID(ctx context.Context, db database.Database, externalID string) (*Invite, error) {
	query := `SELECT id FROM org_invites WHERE external_id = $1 AND deleted_at IS NULL;`

	var id int
	err := db.QueryRow(ctx, query, externalID).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch org invite by external ID")
	}

	return GetInvite(ctx, db, id)
}

// VerifyInvite verifies a token and returns the invite if it's valid, not expired, and not redeemed.
// Returns nil, nil if the token is invalid, expired, or already redeemed.
func VerifyInvite(ctx context.Context, db database.Database, token string) (*Invite, error) {
	b, err := hex.DecodeString(token)
	if err != nil {
		// Invalid hex encoding - treat as invalid token
		return nil, nil //nolint:nilerr
	}

	h := sha256.New()
	_, err = h.Write(b)
	if err != nil {
		return nil, errors.Wrap(err, "unable to hash token")
	}
	hash := h.Sum(nil)

	encoded := inviteTokenEncoding.EncodeToString(hash)

	query := `
		SELECT id FROM org_invites
		WHERE token_hash = $1 AND deleted_at IS NULL AND redeemed_at IS NULL;
	`

	var id int
	err = db.QueryRow(ctx, query, encoded).Scan(&id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to verify org invite token")
	}

	invite, err := GetInvite(ctx, db, id)
	if err != nil {
		return nil, err
	}

	// Check if expired
	if invite.IsExpired() {
		return nil, nil
	}

	return invite, nil
}

// RedeemInvite verifies and redeems an invite token, creating an organization membership for the user.
// Returns the created Member on success.
// Returns nil, nil if the token is invalid, expired, or already redeemed.
func RedeemInvite(
	ctx context.Context,
	db database.Database,
	token string,
	userID int,
) (*Member, error) {
	var member *Member

	err := database.Transact(ctx, db, func(txn database.Database) error {
		invite, err := VerifyInvite(ctx, txn, token)
		if err != nil {
			return err
		}
		if invite == nil {
			return nil // Invalid token, will return nil, nil
		}

		// Check if user is already a member
		existing, err := GetMemberByUserAndOrg(ctx, txn, userID, invite.OrganizationID)
		if err != nil {
			return errors.Wrap(err, "unable to check existing membership")
		}
		if existing != nil {
			return errors.New("user is already a member of this organization")
		}

		// Create org membership
		member, err = CreateMember(ctx, txn, userID, invite.OrganizationID, invite.Role, optional.Empty[string]())
		if err != nil {
			return errors.Wrap(err, "unable to create org membership")
		}

		// Mark invite as redeemed
		markRedeemed := `UPDATE org_invites SET redeemed_at = CURRENT_TIMESTAMP WHERE id = $1;`
		_, err = txn.Exec(ctx, markRedeemed, invite.ID)
		if err != nil {
			return errors.Wrap(err, "unable to mark invite as redeemed")
		}

		return nil
	})
	if err != nil {
		return nil, err
	}

	return member, nil
}

// ListInvitesByOrg returns all pending (not redeemed, not expired, not deleted) invites for an organization.
func ListInvitesByOrg(ctx context.Context, db database.Database, organizationID int) ([]*Invite, error) {
	query := `
		SELECT id, external_id, organization_id, invited_by, email, role, expires_at, created_at, redeemed_at
		FROM org_invites
		WHERE organization_id = $1 AND deleted_at IS NULL AND redeemed_at IS NULL AND expires_at > CURRENT_TIMESTAMP
		ORDER BY created_at DESC;
	`

	rows, err := db.Query(ctx, query, organizationID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list org invites")
	}

	var invites []*Invite
	err = database.ScanRows(rows, func(row database.Row) error {
		i := new(Invite)
		if err := row.Scan(
			&i.ID,
			&i.ExternalID,
			&i.OrganizationID,
			&i.InvitedBy,
			&i.Email,
			&i.Role,
			&i.ExpiresAt,
			&i.CreatedAt,
			&i.RedeemedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan org invite")
		}
		invites = append(invites, i)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return invites, nil
}

// ListInvitesByEmail returns all pending invites for a specific email address.
func ListInvitesByEmail(ctx context.Context, db database.Database, email string) ([]*Invite, error) {
	query := `
		SELECT id, external_id, organization_id, invited_by, email, role, expires_at, created_at, redeemed_at
		FROM org_invites
		WHERE email = $1 AND deleted_at IS NULL AND redeemed_at IS NULL AND expires_at > CURRENT_TIMESTAMP
		ORDER BY created_at DESC;
	`

	rows, err := db.Query(ctx, query, email)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list org invites by email")
	}

	var invites []*Invite
	err = database.ScanRows(rows, func(row database.Row) error {
		i := new(Invite)
		if err := row.Scan(
			&i.ID,
			&i.ExternalID,
			&i.OrganizationID,
			&i.InvitedBy,
			&i.Email,
			&i.Role,
			&i.ExpiresAt,
			&i.CreatedAt,
			&i.RedeemedAt,
		); err != nil {
			return errors.Wrap(err, "unable to scan org invite")
		}
		invites = append(invites, i)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return invites, nil
}

// RevokeInvite soft-deletes an invite, making it no longer valid.
func RevokeInvite(ctx context.Context, db database.Database, id int) error {
	query := `UPDATE org_invites SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1 AND deleted_at IS NULL;`

	_, err := db.Exec(ctx, query, id)
	if err != nil {
		return errors.Wrap(err, "unable to revoke org invite")
	}

	return nil
}
