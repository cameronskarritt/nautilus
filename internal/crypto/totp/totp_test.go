package totp

import (
	"testing"
	"time"

	"nautilus/internal/testutil/require"
)

const testSecret = "JBSWY3DPEHPK3PXP"

var testTime = time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)

func TestGenerate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Secret        string
		Algorithm     string
		Counter       uint64
		Expected      string
		ExpectedError string
	}{
		{Name: "sha1", Secret: testSecret, Algorithm: "SHA1", Counter: 0, Expected: "282760"},
		{Name: "sha256", Secret: testSecret, Algorithm: "SHA256", Counter: 0, Expected: "023015"},
		{Name: "sha512", Secret: testSecret, Algorithm: "SHA512", Counter: 0, Expected: "582788"},
		{Name: "next counter", Secret: testSecret, Algorithm: "SHA1", Counter: 1, Expected: "996554"},
		{Name: "invalid secret", Secret: "INVALID!", Algorithm: "SHA1", ExpectedError: "unable to decode secret"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			otp := newTestOTP(tt.Secret, tt.Algorithm, testTime)
			code, err := otp.Generate(tt.Counter)
			if tt.ExpectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.ExpectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, FormatValue(code, 6))
		})
	}
}

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Secret        string
		Code          string
		Now           time.Time
		Expected      bool
		ExpectedError string
	}{
		{Name: "current code", Secret: testSecret, Code: "546108", Now: testTime, Expected: true},
		{Name: "next code within drift", Secret: testSecret, Code: "602426", Now: testTime.Add(31 * time.Second), Expected: true},
		{Name: "old code outside drift", Secret: testSecret, Code: "546108", Now: testTime.Add(41 * time.Second)},
		{Name: "trimmed code", Secret: testSecret, Code: " 546108 ", Now: testTime, Expected: true},
		{Name: "wrong length", Secret: testSecret, Code: "12345", Now: testTime},
		{Name: "wrong value", Secret: testSecret, Code: "123456", Now: testTime},
		{Name: "invalid secret", Secret: "INVALID!", Code: "123456", Now: testTime, ExpectedError: "unable to decode secret"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			otp := newTestOTP(tt.Secret, "SHA1", tt.Now)
			valid, err := otp.Validate(tt.Code)
			if tt.ExpectedError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.ExpectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, valid)
		})
	}
}

func TestURI(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Issuer   string
		Account  string
		Expected string
	}{
		{
			Name:     "basic",
			Issuer:   "TestApp",
			Account:  "user@example.com",
			Expected: "otpauth://totp/TestApp:user@example.com?algorithm=SHA256&digits=6&issuer=TestApp&period=30&secret=JBSWY3DPEHPK3PXP",
		},
		{
			Name:     "query escaping",
			Issuer:   "My App",
			Account:  "user+test@example.com",
			Expected: "otpauth://totp/My App:user+test@example.com?algorithm=SHA256&digits=6&issuer=My+App&period=30&secret=JBSWY3DPEHPK3PXP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, TOTP(tt.Issuer, tt.Account, testSecret).URI())
		})
	}
}

func TestHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name      string
		Algorithm string
		Expected  string
	}{
		{Name: "sha1", Algorithm: "SHA1", Expected: "SHA1"},
		{Name: "sha256", Algorithm: "SHA256", Expected: "SHA256"},
		{Name: "sha512", Algorithm: "SHA512", Expected: "SHA512"},
		{Name: "dash normalized", Algorithm: "SHA-1", Expected: "SHA1"},
		{Name: "case normalized", Algorithm: "sha256", Expected: "SHA256"},
		{Name: "unknown defaults to sha1", Algorithm: "UNKNOWN", Expected: "SHA1"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			otp := &OTP{algorithm: tt.Algorithm}
			require.NotNil(t, otp.Hash())
			require.Equal(t, tt.Expected, otp.algorithm)
		})
	}
}

func TestFormatValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Value    int
		Length   int
		Expected string
	}{
		{Name: "zero", Value: 0, Length: 6, Expected: ""},
		{Name: "six digits", Value: 123456, Length: 6, Expected: "123456"},
		{Name: "modulo", Value: 1234567, Length: 6, Expected: "234567"},
		{Name: "minimum length", Value: 123, Length: 4, Expected: "000123"},
		{Name: "eight digits", Value: 123456, Length: 8, Expected: "00123456"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, FormatValue(tt.Value, tt.Length))
		})
	}
}

func TestTOTP(t *testing.T) {
	t.Parallel()

	otp := TOTP("TestApp", "user@example.com", testSecret)

	require.Equal(t, testSecret, otp.secret)
	require.Equal(t, "TestApp", otp.issuer)
	require.Equal(t, "user@example.com", otp.account)
	require.Equal(t, 6, otp.length)
	require.Equal(t, 30*time.Second, otp.interval)
	require.Equal(t, 10*time.Second, otp.drift)
	require.Equal(t, "SHA256", otp.algorithm)
	require.NotNil(t, otp.timeFunc)
}

func newTestOTP(secret, algorithm string, now time.Time) *OTP {
	return &OTP{
		secret:    secret,
		algorithm: algorithm,
		length:    6,
		interval:  30 * time.Second,
		drift:     10 * time.Second,
		timeFunc:  func() time.Time { return now },
	}
}
