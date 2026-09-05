package webpush_test

import (
	"testing"

	"nautilus/internal/app/handlers/notifications/webpush"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func requireDetails(t *testing.T, err error, details ...errors.ErrorDetail) {
	t.Helper()

	var httpErr *errors.HTTPError
	require.ErrorAs(t, err, &httpErr)
	for _, detail := range details {
		require.Contains(t, httpErr.Errors, detail)
	}
}

func TestSubscribeForm(t *testing.T) {
	t.Parallel()

	t.Run("normalizes and validates", func(t *testing.T) {
		t.Parallel()

		form := webpush.SubscribeForm{
			Endpoint: " https://push.example/sub ",
			Keys: webpush.SubscribeKeys{
				Auth:   " auth ",
				P256dh: " p256dh ",
			},
		}
		form.Normalize()

		require.NoError(t, form.Validate())
		require.Equal(t, "https://push.example/sub", form.Endpoint)
		require.Equal(t, "auth", form.Keys.Auth)
		require.Equal(t, "p256dh", form.Keys.P256dh)
	})

	t.Run("returns all missing fields", func(t *testing.T) {
		t.Parallel()

		form := webpush.SubscribeForm{
			Endpoint: " ",
			Keys: webpush.SubscribeKeys{
				Auth:   "\t",
				P256dh: "\n",
			},
		}
		form.Normalize()

		err := form.Validate()
		require.ErrorContains(t, err, "Unable to process push subscription")
		requireDetails(t, err, webpush.ErrEmptyEndpoint, webpush.ErrEmptyAuthKey, webpush.ErrEmptyP256dhKey)
	})
}

func TestUnsubscribeForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Endpoint string
		WantErr  bool
	}{
		{
			Name:     "valid endpoint",
			Endpoint: " https://push.example/sub ",
		},
		{
			Name:     "empty endpoint",
			Endpoint: " ",
			WantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			form := webpush.UnsubscribeForm{Endpoint: tt.Endpoint}
			form.Normalize()
			err := form.Validate()

			if !tt.WantErr {
				require.NoError(t, err)
				require.Equal(t, "https://push.example/sub", form.Endpoint)
				return
			}

			require.ErrorContains(t, err, "Unable to process push subscription")
			requireDetails(t, err, webpush.ErrEmptyEndpoint)
		})
	}
}
