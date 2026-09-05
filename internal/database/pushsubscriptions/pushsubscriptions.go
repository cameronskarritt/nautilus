package pushsubscriptions

import (
	"context"
	"database/sql"

	"nautilus/internal/database"
	"nautilus/internal/errors"
	"nautilus/internal/notifier/webpush"
)

func Create(ctx context.Context, db database.Database, userID int, endpoint, keyAuth, keyP256dh string) (*PushSubscription, error) {
	query := `
		INSERT INTO push_subscriptions(user_id, endpoint, key_auth, key_p256dh)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id, endpoint) DO UPDATE SET key_auth = $3, key_p256dh = $4
		RETURNING id;
	`

	var id int
	err := db.QueryRow(ctx, query, userID, endpoint, keyAuth, keyP256dh).Scan(&id)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create push subscription")
	}

	return Get(ctx, db, id)
}

func Get(ctx context.Context, db database.Database, id int) (*PushSubscription, error) {
	sub := new(PushSubscription)

	query := `SELECT id, user_id, endpoint, key_auth, key_p256dh, created_at FROM push_subscriptions WHERE id = $1;`
	err := db.QueryRow(ctx, query, id).Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.KeyAuth, &sub.KeyP256dh, &sub.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch push subscription")
	}

	return sub, nil
}

func GetByUserAndEndpoint(ctx context.Context, db database.Database, userID int, endpoint string) (*PushSubscription, error) {
	sub := new(PushSubscription)

	query := `SELECT id, user_id, endpoint, key_auth, key_p256dh, created_at FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2;`
	err := db.QueryRow(ctx, query, userID, endpoint).Scan(&sub.ID, &sub.UserID, &sub.Endpoint, &sub.KeyAuth, &sub.KeyP256dh, &sub.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, errors.Wrap(err, "unable to fetch push subscription by endpoint")
	}

	return sub, nil
}

func ListByUser(ctx context.Context, db database.Database, userID int) ([]*PushSubscription, error) {
	query := `SELECT id, user_id, endpoint, key_auth, key_p256dh, created_at FROM push_subscriptions WHERE user_id = $1 ORDER BY created_at DESC;`
	rows, err := db.Query(ctx, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to list push subscriptions")
	}

	var subs []*PushSubscription
	err = database.ScanRows(rows, func(row database.Row) error {
		s := new(PushSubscription)
		if err := row.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.KeyAuth, &s.KeyP256dh, &s.CreatedAt); err != nil {
			return errors.Wrap(err, "unable to scan push subscription")
		}
		subs = append(subs, s)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return subs, nil
}

func Delete(ctx context.Context, db database.Database, userID int, endpoint string) error {
	query := `DELETE FROM push_subscriptions WHERE user_id = $1 AND endpoint = $2;`
	_, err := db.Exec(ctx, query, userID, endpoint)
	if err != nil {
		return errors.Wrap(err, "unable to delete push subscription")
	}

	return nil
}

// Store implements webpush.SubscriptionStore by querying push subscriptions
// for a user identified by their external ID.
type Store struct {
	db database.Database
}

func NewStore(db database.Database) *Store {
	return &Store{db: db}
}

func (s *Store) GetSubscriptions(ctx context.Context, userID string) ([]webpush.Subscription, error) {
	query := `
		SELECT ps.endpoint, ps.key_auth, ps.key_p256dh
		FROM push_subscriptions ps
		JOIN users u ON u.id = ps.user_id
		WHERE u.external_id = $1;
	`

	rows, err := s.db.Query(ctx, query, userID)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get push subscriptions for user")
	}

	var subs []webpush.Subscription
	err = database.ScanRows(rows, func(row database.Row) error {
		var sub webpush.Subscription
		if err := row.Scan(&sub.Endpoint, &sub.Keys.Auth, &sub.Keys.P256dh); err != nil {
			return errors.Wrap(err, "unable to scan push subscription")
		}
		subs = append(subs, sub)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return subs, nil
}
