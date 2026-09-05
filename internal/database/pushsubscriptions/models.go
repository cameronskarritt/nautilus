package pushsubscriptions

import "time"

type PushSubscription struct {
	ID        int       `json:"-"`
	UserID    int       `json:"-"`
	Endpoint  string    `json:"endpoint"`
	KeyAuth   string    `json:"key_auth"`
	KeyP256dh string    `json:"key_p256dh"`
	CreatedAt time.Time `json:"created_at"`
}
