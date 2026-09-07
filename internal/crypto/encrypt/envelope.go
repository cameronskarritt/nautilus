package encrypt

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"io"

	"nautilus/internal/errors"
)

// MaxPlaintextSize is the maximum number of plaintext bytes in one envelope.
const MaxPlaintextSize = 16 << 20

const (
	envelopeMagic      = "NTLE"
	envelopeVersion    = 1
	maxBindingSize     = 1024
	envelopeHeaderSize = 4 + 1 + 4 + 12 + 12
	wrappedKeySize     = 32 + 16
	envelopeOverhead   = envelopeHeaderSize + wrappedKeySize + 16
)

var (
	ErrInvalidEnvelope   = errors.New("invalid encrypted envelope")
	ErrInvalidBinding    = errors.New("encryption binding requires purpose and record ID of at most 1024 bytes each")
	ErrUnscoped          = errors.New("envelope encryption requires a scoped encrypter")
	ErrPlaintextTooLarge = errors.New("envelope plaintext exceeds 16 MiB")
)

// Binding identifies the use and immutable record identity of a secret.
// Callers must derive it from trusted application state, never from the envelope.
type Binding struct {
	Purpose  string
	RecordID string
}

// Seal encrypts each value with a fresh data key wrapped by the scoped application key.
func (e *Encrypter) Seal(ctx context.Context, plaintext []byte, binding Binding) ([]byte, error) {
	if err := e.validateBinding(binding); err != nil {
		return nil, err
	}
	if len(plaintext) > MaxPlaintextSize {
		return nil, ErrPlaintextTooLarge
	}
	wrap, err := e.cipher(ctx)
	if err != nil {
		return nil, err
	}
	key := make([]byte, 32)
	defer clear(key)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, errors.Wrap(err, "failed to generate envelope data key")
	}
	data, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	frame := make([]byte, envelopeHeaderSize, envelopeOverhead+len(plaintext))
	copy(frame, envelopeMagic)
	frame[4] = envelopeVersion
	binary.BigEndian.PutUint32(frame[5:9], uint32(len(plaintext)))
	if _, err := io.ReadFull(rand.Reader, frame[9:envelopeHeaderSize]); err != nil {
		return nil, errors.Wrap(err, "failed to generate envelope nonces")
	}
	header := frame[:envelopeHeaderSize]
	frame = wrap.Seal(frame, header[9:21], key, e.associatedData(header, binding, "wrap"))
	frame = data.Seal(frame, header[21:33], plaintext, e.associatedData(header, binding, "data"))
	return frame, nil
}

// Open accepts only versioned envelopes bound to this scope, purpose, and record.
func (e *Encrypter) Open(ctx context.Context, frame []byte, binding Binding) ([]byte, error) {
	if err := e.validateBinding(binding); err != nil {
		return nil, err
	}
	if len(frame) < envelopeOverhead || len(frame) > MaxPlaintextSize+envelopeOverhead {
		return nil, ErrInvalidEnvelope
	}
	if string(frame[:4]) != envelopeMagic || frame[4] != envelopeVersion ||
		uint64(binary.BigEndian.Uint32(frame[5:9])) != uint64(len(frame)-envelopeOverhead) {
		return nil, ErrInvalidEnvelope
	}
	wrap, err := e.cipher(ctx)
	if err != nil {
		return nil, err
	}
	header := frame[:envelopeHeaderSize]
	key, err := wrap.Open(nil, header[9:21], frame[envelopeHeaderSize:envelopeHeaderSize+wrappedKeySize], e.associatedData(header, binding, "wrap"))
	defer clear(key)
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	data, err := newCipher(key)
	if err != nil {
		return nil, err
	}
	plaintext, err := data.Open(nil, header[21:33], frame[envelopeHeaderSize+wrappedKeySize:], e.associatedData(header, binding, "data"))
	if err != nil {
		return nil, ErrInvalidEnvelope
	}
	return plaintext, nil
}

func (e *Encrypter) validateBinding(binding Binding) error {
	if e == nil || e.scope == "" {
		return ErrUnscoped
	}
	if len(binding.Purpose) == 0 || len(binding.Purpose) > maxBindingSize ||
		len(binding.RecordID) == 0 || len(binding.RecordID) > maxBindingSize {
		return ErrInvalidBinding
	}
	return nil
}

func (e *Encrypter) associatedData(header []byte, binding Binding, domain string) []byte {
	aad := append([]byte(nil), header...)
	for _, value := range []string{domain, e.scope, binding.Purpose, binding.RecordID} {
		aad = binary.BigEndian.AppendUint64(aad, uint64(len(value)))
		aad = append(aad, value...)
	}
	return aad
}
