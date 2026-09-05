package webpush

import (
	"context"
	"encoding/json"

	"nautilus/internal/errors"
	"nautilus/internal/notifier"
)

type pushPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// SubscriptionStore looks up push subscriptions for a given user.
type SubscriptionStore interface {
	GetSubscriptions(ctx context.Context, userID string) ([]Subscription, error)
}

// WebPushNotifier implements notifier.Notifier by sending web push notifications
// to all subscriptions associated with a user.
type WebPushNotifier struct {
	store      SubscriptionStore
	subscriber string
	publicKey  string
	privateKey string
}

// NewNotifier creates a WebPushNotifier with the given subscription store and VAPID credentials.
// subscriber is the contact URI (email) for the VAPID JWT "sub" claim.
func NewNotifier(store SubscriptionStore, subscriber, publicKey, privateKey string) *WebPushNotifier {
	return &WebPushNotifier{
		store:      store,
		subscriber: subscriber,
		publicKey:  publicKey,
		privateKey: privateKey,
	}
}

func (n *WebPushNotifier) Notify(ctx context.Context, notification *notifier.Notification) error {
	if notification.Recipient == "" {
		return notifier.ErrRecipientRequired
	}

	subscriptions, err := n.store.GetSubscriptions(ctx, notification.Recipient)
	if err != nil {
		return errors.Wrap(err, "failed to get subscriptions")
	}

	opts := &Options{
		Subscriber:      n.subscriber,
		VAPIDPublicKey:  n.publicKey,
		VAPIDPrivateKey: n.privateKey,
		Urgency:         urgencyFromNotifier(notification.Urgency),
	}

	payload, err := json.Marshal(pushPayload{
		Title: notification.Subject,
		Body:  notification.Body,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal push payload")
	}

	for _, sub := range subscriptions {
		resp, err := SendNotification(ctx, payload, &sub, opts)
		if err != nil {
			return errors.Wrap(err, "failed to send web push notification")
		}
		resp.Body.Close()
	}

	return nil
}
