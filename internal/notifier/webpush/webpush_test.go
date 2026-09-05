package webpush

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"nautilus/internal/testutil/require"
)

// testVAPIDPrivateKey and testVAPIDPublicKey are a fixed P-256 keypair used
// only for tests. The private key must be a valid 32-byte raw scalar so that
// ecdsa.ParseRawPrivateKey accepts it.
const (
	testVAPIDPrivateKey = "SjFSkbKKTvKktL5TLEchfnnfcuNe09TfgSKEmXtcrdo"
	testVAPIDPublicKey  = "BDJPJBHNyMZqBSEBiezGWgJ6C_s0hJqgLxJx79raygIn2brwEVrpFcHTuC9j8cWBfYun9DPyMaySdUAnICvrpgc"
)

type testHTTPClient func(*http.Request)

func (c testHTTPClient) Do(req *http.Request) (*http.Response, error) {
	if c != nil {
		c(req)
	}
	return &http.Response{StatusCode: http.StatusCreated, Body: http.NoBody}, nil
}

func assertPushRequest(t *testing.T, req *http.Request) {
	t.Helper()

	require.Equal(t, http.MethodPost, req.Method)
	require.Equal(t, "aes128gcm", req.Header.Get("Content-Encoding"))
	require.Equal(t, "application/octet-stream", req.Header.Get("Content-Type"))
	require.Equal(t, "0", req.Header.Get("TTL"))
	require.Equal(t, "test_topic", req.Header.Get("Topic"))
	require.Equal(t, string(UrgencyLow), req.Header.Get("Urgency"))
	require.True(t, strings.HasPrefix(req.Header.Get("Authorization"), "vapid t="))
	require.Contains(t, req.Header.Get("Authorization"), ", k="+testVAPIDPublicKey)

	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)
	require.NotEmpty(t, body)
}

func urlEncodedTestSubscription() *Subscription {
	return &Subscription{
		Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/gAAAAA",
		Keys: Keys{
			P256dh: "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk",
			Auth:   "zqbxT6JKstKSY9JKibZLSQ",
		},
	}
}

func standardEncodedTestSubscription() *Subscription {
	return &Subscription{
		Endpoint: "https://updates.push.services.mozilla.com/wpush/v2/gAAAAA",
		Keys: Keys{
			P256dh: "BNNL5ZaTfK81qhXOx23+wewhigUeFb632jN6LvRWCFH1ubQr77FE/9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk=",
			Auth:   "zqbxT6JKstKSY9JKibZLSQ==",
		},
	}
}

func TestSendNotification(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name         string
		Message      []byte
		Subscription *Subscription
		Options      *Options
		ExpectError  bool
	}{
		{
			Name:         "url encoded subscription",
			Message:      []byte("Test"),
			Subscription: urlEncodedTestSubscription(),
			Options: &Options{
				RecordSize:      3070,
				Subscriber:      "test@example.com",
				Topic:           "test_topic",
				TTL:             0,
				Urgency:         UrgencyLow,
				VAPIDPublicKey:  testVAPIDPublicKey,
				VAPIDPrivateKey: testVAPIDPrivateKey,
			},
		},
		{
			Name:         "standard encoded subscription",
			Message:      []byte("Test"),
			Subscription: standardEncodedTestSubscription(),
			Options: &Options{
				Subscriber:      "test@example.com",
				Topic:           "test_topic",
				TTL:             0,
				Urgency:         UrgencyLow,
				VAPIDPublicKey:  testVAPIDPublicKey,
				VAPIDPrivateKey: testVAPIDPrivateKey,
			},
		},
		{
			Name:         "payload too large",
			Message:      []byte(strings.Repeat("Test", int(MaxRecordSize))),
			Subscription: standardEncodedTestSubscription(),
			Options: &Options{
				Subscriber:      "test@example.com",
				Topic:           "test_topic",
				TTL:             0,
				Urgency:         UrgencyLow,
				VAPIDPublicKey:  testVAPIDPublicKey,
				VAPIDPrivateKey: testVAPIDPrivateKey,
			},
			ExpectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			if tt.ExpectError {
				tt.Options.HTTPClient = testHTTPClient(func(*http.Request) { t.Fatal("client should not be called") })
			} else {
				tt.Options.HTTPClient = testHTTPClient(func(req *http.Request) { assertPushRequest(t, req) })
			}

			resp, err := SendNotification(context.Background(), tt.Message, tt.Subscription, tt.Options)
			if tt.ExpectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, resp.StatusCode)
			require.NoError(t, resp.Body.Close())
		})
	}
}
