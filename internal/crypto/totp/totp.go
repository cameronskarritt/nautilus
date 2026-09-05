package totp

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"hash"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"nautilus/internal/errors"
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

type OTP struct {
	secret    string
	algorithm string
	issuer    string
	account   string
	length    int
	interval  time.Duration
	drift     time.Duration

	timeFunc func() time.Time
}

func (otp *OTP) URI() string {
	params := make(url.Values)

	// https://github.com/google/google-authenticator/wiki/Key-Uri-Format
	params.Set("digits", strconv.Itoa(otp.length))
	params.Set("issuer", otp.issuer)
	params.Set("secret", otp.secret)
	params.Set("algorithm", otp.algorithm)
	params.Set("period", strconv.Itoa(int(otp.interval.Seconds())))

	uri := url.URL{
		Scheme:   "otpauth",
		Opaque:   fmt.Sprintf("//totp/%s:%s", otp.issuer, otp.account),
		RawQuery: params.Encode(),
	}

	return uri.String()
}

func FormatValue(val int, length int) string {
	if val == 0 {
		return ""
	}
	if length < 6 {
		length = 6
	}

	format := fmt.Sprintf("%%0%dd", length)
	return fmt.Sprintf(format, val%int(math.Pow10(length)))
}

func (otp *OTP) Hash() func() hash.Hash {
	normalized := strings.ReplaceAll(strings.ToUpper(otp.algorithm), "-", "")

	// These are the only algorithms currently supported by Google Authenticator
	switch normalized {
	case "SHA1":
		otp.algorithm = "SHA1"
		return sha1.New
	case "SHA256":
		otp.algorithm = "SHA256"
		return sha256.New
	case "SHA512":
		otp.algorithm = "SHA512"
		return sha512.New
	default:
		otp.algorithm = "SHA1"
		return sha1.New
	}
}

func (otp *OTP) Generate(counter uint64) (int, error) {
	b, err := b32.DecodeString(otp.secret)
	if err != nil {
		return -1, errors.Wrap(err, "unable to decode secret")
	}

	mac := hmac.New(otp.Hash(), b)

	cb := make([]byte, 8)
	binary.BigEndian.PutUint64(cb, counter)

	mac.Write(cb)
	sum := mac.Sum(nil)

	// https://tools.ietf.org/html/rfc4226#section-5.4
	offset := sum[len(sum)-1] & 0xf
	val := ((int(sum[offset]) & 0x7f) << 24) |
		((int(sum[offset+1] & 0xff)) << 16) |
		((int(sum[offset+2] & 0xff)) << 8) |
		(int(sum[offset+3]) & 0xff)

	return val, nil
}

// TOTP creates a new OTP instance with issuer and account for URI generation.
func TOTP(issuer string, account string, secret string) *OTP {
	return &OTP{
		secret:    secret,
		length:    6,
		interval:  30 * time.Second,
		drift:     10 * time.Second,
		algorithm: "SHA256",
		issuer:    issuer,
		account:   account,
		timeFunc:  time.Now,
	}
}

// Validate is a convenience function to validate a TOTP code against a secret.
// Use this when you only need to verify a code and don't need to generate a URI.
func Validate(secret, code string) (bool, error) {
	otp := &OTP{
		secret:    secret,
		length:    6,
		interval:  30 * time.Second,
		drift:     10 * time.Second,
		algorithm: "SHA256",
		timeFunc:  time.Now,
	}
	return otp.Validate(code)
}

func (otp *OTP) Validate(code string) (bool, error) {
	interval := int64(otp.interval.Seconds())
	now := otp.timeFunc().Unix()
	drift := int64(otp.drift.Seconds())
	minCounter := (now - drift) / interval
	maxCounter := (now + drift) / interval

	for counter := minCounter; counter <= maxCounter; counter++ {
		valid, err := otp.validate(code, uint64(counter))
		if err != nil {
			return false, errors.Wrap(err, "unable to validate code")
		}

		if valid {
			return true, nil
		}
	}

	return false, nil
}

func (otp *OTP) validate(code string, counter uint64) (bool, error) {
	code = strings.TrimSpace(code)

	if len(code) != otp.length {
		return false, nil
	}

	val, err := otp.Generate(counter)
	if err != nil {
		return false, errors.Wrap(err, "unable to generate code")
	}
	codeVal := FormatValue(val, otp.length)

	return subtle.ConstantTimeCompare([]byte(codeVal), []byte(code)) == 1, nil
}
