package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/hkdf"

	"nautilus/internal/errors"
)

const MaxRecordSize uint32 = 4096

var ErrMaxPadExceeded = errors.New("payload has exceeded the maximum length")

// saltFunc generates a salt of 16 bytes.
var saltFunc = func() ([]byte, error) {
	salt := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, salt)
	if err != nil {
		return salt, errors.Wrap(err, "unable to read random salt")
	}

	return salt, nil
}

// HTTPClient is an interface for sending the notification HTTP request.
type HTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

// Options contains config and extra params needed to send a notification.
type Options struct {
	HTTPClient      HTTPClient
	RecordSize      uint32
	Subscriber      string
	Topic           string
	TTL             int
	Urgency         Urgency
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VapidExpiration time.Time
}

// Keys are the base64 encoded values from PushSubscription.getKey().
type Keys struct {
	Auth   string `json:"auth"`
	P256dh string `json:"p256dh"`
}

// Subscription represents a PushSubscription object from the Push API.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     Keys   `json:"keys"`
}

// SendNotification sends a push notification to a subscription's endpoint
// using Message Encryption for Web Push (RFC 8291) and VAPID protocols.
func SendNotification(ctx context.Context, message []byte, s *Subscription, options *Options) (*http.Response, error) {
	authSecret, err := decodeSubscriptionKey(s.Keys.Auth)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decode auth secret")
	}

	dh, err := decodeSubscriptionKey(s.Keys.P256dh)
	if err != nil {
		return nil, errors.Wrap(err, "unable to decode p256dh key")
	}

	salt, err := saltFunc()
	if err != nil {
		return nil, errors.Wrap(err, "unable to generate salt")
	}

	curve := ecdh.P256()
	localPrivateKey, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return nil, errors.Wrap(err, "unable to generate local key")
	}

	localPublicKey := localPrivateKey.PublicKey().Bytes()
	remotePublicKey, err := curve.NewPublicKey(dh)
	if err != nil {
		return nil, errors.Wrap(err, "invalid remote public key")
	}

	sharedECDHSecret, err := localPrivateKey.ECDH(remotePublicKey)
	if err != nil {
		return nil, errors.Wrap(err, "unable to derive shared secret")
	}

	hash := sha256.New

	prkInfoBuf := bytes.NewBuffer([]byte("WebPush: info\x00"))
	prkInfoBuf.Write(dh)
	prkInfoBuf.Write(localPublicKey)

	prkHKDF := hkdf.New(hash, sharedECDHSecret, authSecret, prkInfoBuf.Bytes())
	ikm, err := getHKDFKey(prkHKDF, 32)
	if err != nil {
		return nil, errors.Wrap(err, "unable to derive IKM")
	}

	contentEncryptionKeyInfo := []byte("Content-Encoding: aes128gcm\x00")
	contentHKDF := hkdf.New(hash, ikm, salt, contentEncryptionKeyInfo)
	contentEncryptionKey, err := getHKDFKey(contentHKDF, 16)
	if err != nil {
		return nil, errors.Wrap(err, "unable to derive content encryption key")
	}

	nonceInfo := []byte("Content-Encoding: nonce\x00")
	nonceHKDF := hkdf.New(hash, ikm, salt, nonceInfo)
	nonce, err := getHKDFKey(nonceHKDF, 12)
	if err != nil {
		return nil, errors.Wrap(err, "unable to derive nonce")
	}

	c, err := aes.NewCipher(contentEncryptionKey)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create cipher")
	}

	gcm, err := cipher.NewGCM(c)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create gcm cipher")
	}

	recordSize := options.RecordSize
	if recordSize == 0 {
		recordSize = MaxRecordSize
	}

	recordLength := int(recordSize) - 16

	recordBuf := bytes.NewBuffer(salt)

	rs := make([]byte, 4)
	binary.BigEndian.PutUint32(rs, recordSize)

	recordBuf.Write(rs)
	recordBuf.Write([]byte{byte(len(localPublicKey))})
	recordBuf.Write(localPublicKey)

	messageCopy := make([]byte, len(message))
	copy(messageCopy, message)
	dataBuf := bytes.NewBuffer(messageCopy)

	dataBuf.Write([]byte("\x02"))
	if err := pad(dataBuf, recordLength-recordBuf.Len()); err != nil {
		return nil, errors.Wrap(err, "unable to pad payload")
	}

	ciphertext := gcm.Seal([]byte{}, nonce, dataBuf.Bytes(), nil)
	recordBuf.Write(ciphertext)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.Endpoint, recordBuf)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create push request")
	}

	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(options.TTL))

	if len(options.Topic) > 0 {
		req.Header.Set("Topic", options.Topic)
	}

	if isValidUrgency(options.Urgency) {
		req.Header.Set("Urgency", string(options.Urgency))
	}

	expiration := options.VapidExpiration
	if expiration.IsZero() {
		expiration = time.Now().Add(time.Hour * 12)
	}

	vapidAuthHeader, err := getVAPIDAuthorizationHeader(
		s.Endpoint,
		options.Subscriber,
		options.VAPIDPublicKey,
		options.VAPIDPrivateKey,
		expiration,
	)
	if err != nil {
		return nil, errors.Wrap(err, "unable to build VAPID authorization header")
	}

	req.Header.Set("Authorization", vapidAuthHeader)

	var client HTTPClient
	if options.HTTPClient != nil {
		client = options.HTTPClient
	} else {
		client = &http.Client{}
	}

	return client.Do(req)
}

// decodeSubscriptionKey decodes a base64 subscription key, handling both
// standard and URL-safe encodings with or without padding.
func decodeSubscriptionKey(key string) ([]byte, error) {
	buf := bytes.NewBufferString(key)
	if rem := len(key) % 4; rem != 0 {
		buf.WriteString(strings.Repeat("=", 4-rem))
	}

	b, err := base64.StdEncoding.DecodeString(buf.String())
	if err == nil {
		return b, nil
	}

	b, err = base64.URLEncoding.DecodeString(buf.String())
	if err != nil {
		return nil, errors.Wrap(err, "unable to decode subscription key")
	}
	return b, nil
}

func getHKDFKey(hkdf io.Reader, length int) ([]byte, error) {
	key := make([]byte, length)
	n, err := io.ReadFull(hkdf, key)
	if err != nil {
		return key, errors.Wrap(err, "unable to derive HKDF key")
	}
	if n != len(key) {
		return key, errors.Errorf("unable to derive HKDF key: expected %d bytes, got %d", len(key), n)
	}

	return key, nil
}

func pad(payload *bytes.Buffer, maxPadLen int) error {
	payloadLen := payload.Len()
	if payloadLen > maxPadLen {
		return ErrMaxPadExceeded
	}

	padLen := maxPadLen - payloadLen

	padding := make([]byte, padLen)
	payload.Write(padding)

	return nil
}
