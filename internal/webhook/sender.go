package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var errDestination = errors.New("invalid webhook destination")

// Restrict IPv6 to global unicast and exclude special-purpose ranges within it.
var blocked = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("168.63.129.16/32"), // Azure platform virtual address.
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("3fff::/20"),
}

func publicIP(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Is4In6() || ip.Zone() != "" || !ip.IsGlobalUnicast() {
		return false
	}
	if ip.Is6() && !netip.MustParsePrefix("2000::/3").Contains(ip) {
		return false
	}
	for _, prefix := range blocked {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

// ValidateURL checks syntax and literal IPs. DNS is checked again at connection time.
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || strings.Contains(raw, "#") || u.Opaque != "" {
		return errDestination
	}
	if strings.Contains(u.Hostname(), "%") || strings.HasSuffix(u.Host, ":") {
		return errDestination
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return errDestination
		}
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicIP(ip) {
		return errDestination
	}
	return nil
}

type Sender struct {
	client *http.Client
}

type Result struct {
	StatusCode int
	ErrorCode  enums.WebhookErrorCode
}

func NewSender() *Sender {
	d := dialer{lookup: net.DefaultResolver.LookupNetIP, connect: (&net.Dialer{}).DialContext}
	return &Sender{client: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Transport: &http.Transport{
			DialContext:            d.dial,
			TLSHandshakeTimeout:    10 * time.Second,
			DisableKeepAlives:      true,
			DisableCompression:     true,
			MaxResponseHeaderBytes: 16 << 10,
		},
	}}
}

func (s *Sender) Send(ctx context.Context, destination, eventID string, payload []byte, secrets [][]byte) Result {
	if ValidateURL(destination) != nil {
		return Result{ErrorCode: enums.WebhookErrorInvalidDestination}
	}
	if len(secrets) == 0 {
		return Result{ErrorCode: enums.WebhookErrorInvalidSecret}
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signatures := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if len(secret) == 0 {
			return Result{ErrorCode: enums.WebhookErrorInvalidSecret}
		}
		mac := hmac.New(sha256.New, secret)
		_, _ = io.WriteString(mac, eventID+"."+timestamp+".")
		_, _ = mac.Write(payload)
		signatures = append(signatures, "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, destination, bytes.NewReader(payload))
	if err != nil {
		return Result{ErrorCode: enums.WebhookErrorInvalidDestination}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("webhook-id", eventID)
	req.Header.Set("webhook-timestamp", timestamp)
	req.Header.Set("webhook-signature", strings.Join(signatures, " "))
	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, errDestination) {
			return Result{ErrorCode: enums.WebhookErrorInvalidDestination}
		}
		var failure net.Error
		if errors.As(err, &failure) && failure.Timeout() {
			return Result{ErrorCode: enums.WebhookErrorTimeout}
		}
		return Result{ErrorCode: enums.WebhookErrorNetwork}
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return Result{StatusCode: resp.StatusCode}
}

type dialer struct {
	lookup  func(context.Context, string, string) ([]netip.Addr, error)
	connect func(context.Context, string, string) (net.Conn, error)
}

func (d dialer) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errDestination
	}
	var ips []netip.Addr
	if ip, err := netip.ParseAddr(host); err == nil {
		ips = []netip.Addr{ip}
	} else {
		ips, err = d.lookup(ctx, "ip", host)
		if err != nil {
			return nil, errors.Wrap(err, "webhook DNS lookup failed")
		}
	}
	if len(ips) == 0 {
		return nil, errDestination
	}
	// Reject mixed public/private answers before opening any connection.
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errDestination
		}
	}
	for _, ip := range ips {
		var conn net.Conn
		conn, err = d.connect(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
	}
	return nil, errors.Wrap(err, "webhook connection failed")
}
