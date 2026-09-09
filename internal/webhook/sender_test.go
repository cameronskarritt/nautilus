package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestValidateURL(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"http://example.com", "https://user:secret@example.com", "https://example.com/#", "https://example.com:0", "https://example.com:65536",
		"https://127.0.0.1", "https://10.0.0.1", "https://100.100.100.200", "https://169.254.169.254", "https://168.63.129.16",
		"https://192.0.2.1", "https://198.18.0.1", "https://224.0.0.1", "https://255.255.255.255", "https://0.0.0.0",
		"https://[::]", "https://[::1]", "https://[::ffff:8.8.8.8]", "https://[fe80::1]", "https://[fc00::1]", "https://[ff02::1]",
		"https://[2001:db8::1]", "https://[2002:0808:0808::1]", "https://[64:ff9b::808:808]", "https://[3fff::1]", "https://[fe80::1%25en0]",
	} {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			require.ErrorIs(t, ValidateURL(raw), errDestination)
		})
	}
	for _, raw := range []string{"https://example.com/hook?token=secret", "https://8.8.8.8:8443", "https://[2606:4700:4700::1111]/hook"} {
		require.NoError(t, ValidateURL(raw))
	}
}

func TestDialPinsValidatedDNS(t *testing.T) {
	t.Parallel()
	lookups := 0
	var addresses []string
	d := dialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			lookups++
			return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("2606:4700:4700::1111")}, nil
		},
		connect: func(_ context.Context, _, address string) (net.Conn, error) {
			addresses = append(addresses, address)
			return nil, errors.New("unavailable")
		},
	}
	_, err := d.dial(t.Context(), "tcp", "example.com:443")
	require.Error(t, err)
	require.Equal(t, 1, lookups)
	require.Equal(t, []string{"8.8.8.8:443", "[2606:4700:4700::1111]:443"}, addresses)
}

func TestDialRejectsMixedDNS(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"127.0.0.1", "::1", "::ffff:8.8.8.8", "169.254.169.254"} {
		t.Run(address, func(t *testing.T) {
			t.Parallel()
			d := dialer{
				lookup: func(context.Context, string, string) ([]netip.Addr, error) {
					return []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr(address)}, nil
				},
				connect: func(context.Context, string, string) (net.Conn, error) {
					t.Error("unsafe DNS answer reached dial")
					return nil, errors.New("unexpected dial")
				},
			}
			_, err := d.dial(t.Context(), "tcp", "example.com:443")
			require.ErrorIs(t, err, errDestination)
		})
	}
}

func TestDialRechecksDNS(t *testing.T) {
	t.Parallel()
	lookups, connections := 0, 0
	d := dialer{
		lookup: func(context.Context, string, string) ([]netip.Addr, error) {
			lookups++
			if lookups == 1 {
				return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
			}
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		},
		connect: func(context.Context, string, string) (net.Conn, error) {
			connections++
			return nil, errors.New("unavailable")
		},
	}
	_, err := d.dial(t.Context(), "tcp", "example.com:443")
	require.Error(t, err)
	_, err = d.dial(t.Context(), "tcp", "example.com:443")
	require.ErrorIs(t, err, errDestination)
	require.Equal(t, 2, lookups)
	require.Equal(t, 1, connections)
}

// Preserve normal TLS verification, substituting only DNS and the TCP route.
func testSender(t *testing.T, handler http.Handler) *Sender {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	s := NewSender()
	transport := s.client.Transport.(*http.Transport)
	transport.TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	transport.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
		return conn, errors.Wrap(err, "test server connection failed")
	}
	t.Cleanup(transport.CloseIdleConnections)
	return s
}

func TestSendSignsExactBody(t *testing.T) {
	t.Parallel()
	payload := []byte("{ \"value\": 1 }\n")
	secrets := [][]byte{[]byte("current secret"), []byte("previous secret")}
	eventID := "bdc26b79-9778-4fb2-adca-4c30e9280180"
	var body []byte
	var headers http.Header
	s := testSender(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusAccepted)
	}))
	before := time.Now().Unix()
	result := s.Send(t.Context(), "https://example.com/hook", eventID, payload, secrets)
	require.Equal(t, Result{StatusCode: http.StatusAccepted}, result)
	require.Equal(t, payload, body)
	require.Equal(t, eventID, headers.Get("webhook-id"))
	timestamp := headers.Get("webhook-timestamp")
	n, err := strconv.ParseInt(timestamp, 10, 64)
	require.NoError(t, err)
	require.True(t, n >= before && n <= time.Now().Unix())
	signatures := strings.Split(headers.Get("webhook-signature"), " ")
	require.Len(t, signatures, 2)
	for i, secret := range secrets {
		mac := hmac.New(sha256.New, secret)
		_, _ = mac.Write(append([]byte(eventID+"."+timestamp+"."), payload...))
		require.Equal(t, "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)), signatures[i])
	}
}

func TestSendDoesNotRedirect(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := testSender(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "https://127.0.0.1/private", http.StatusTemporaryRedirect)
	}))
	result := s.Send(t.Context(), "https://example.com", "event", []byte("{}"), [][]byte{[]byte("secret")})
	require.Equal(t, Result{StatusCode: http.StatusTemporaryRedirect}, result)
	require.Equal(t, int32(1), calls.Load())
}

func TestSendTimeout(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	defer close(release)
	s := testSender(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	s.client.Timeout = 20 * time.Millisecond
	result := s.Send(t.Context(), "https://example.com", "event", []byte("{}"), [][]byte{[]byte("secret")})
	require.Equal(t, Result{ErrorCode: "timeout"}, result)
}

type responseBody struct {
	read   int
	closed bool
}

func (b *responseBody) Read(p []byte) (int, error) {
	b.read += len(p)
	return len(p), nil
}

func (b *responseBody) Close() error {
	b.closed = true
	return nil
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSendClosesBoundedBody(t *testing.T) {
	t.Parallel()
	body := new(responseBody)
	s := NewSender()
	s.client.Transport = transportFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Body: body, Header: make(http.Header)}, nil
	})
	result := s.Send(t.Context(), "https://example.com", "event", []byte("{}"), [][]byte{[]byte("secret")})
	require.Equal(t, Result{StatusCode: http.StatusInternalServerError}, result)
	require.Equal(t, 4096, body.read)
	require.True(t, body.closed)
}

func TestSenderTransportPolicy(t *testing.T) {
	t.Parallel()
	s := NewSender()
	transport := s.client.Transport.(*http.Transport)
	require.Nil(t, transport.Proxy)
	require.Nil(t, transport.TLSClientConfig)
	require.True(t, transport.DisableKeepAlives)
	require.Equal(t, 10*time.Second, s.client.Timeout)
	result := s.Send(t.Context(), "https://127.0.0.1?secret=hidden", "event", nil, [][]byte{[]byte("secret")})
	require.Equal(t, Result{ErrorCode: "invalid_destination"}, result)
}

func TestSendRejectsUntrustedTLS(t *testing.T) {
	t.Parallel()
	var calls atomic.Int32
	s := testSender(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	s.client.Transport.(*http.Transport).TLSClientConfig = nil
	result := s.Send(t.Context(), "https://example.com/private?token=hidden", "event", nil, [][]byte{[]byte("secret")})
	require.Equal(t, Result{ErrorCode: "network_error"}, result)
	require.Equal(t, int32(0), calls.Load())
}
