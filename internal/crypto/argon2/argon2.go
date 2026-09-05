package argon2

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"

	"nautilus/internal/errors"
)

type parameters struct {
	memory  uint32 // m
	time    uint32 // t
	threads uint8  // p
	saltLen int
	keyLen  uint32
}

// OWASP recommended params
// https://cheatsheetseries.owasp.org/cheatsheets/Password_Storage_Cheat_Sheet.html#argon2id
var defaultParams = parameters{
	memory:  19456, // 19mb
	time:    2,
	threads: 1,
	saltLen: 16,
	keyLen:  32,
}

func argon2idkey(plaintext string, salt []byte, params parameters) []byte {
	return argon2.IDKey(
		[]byte(plaintext),
		salt,
		params.time,
		params.memory,
		params.threads,
		params.keyLen,
	)
}

func GenerateHash(plaintext string) (string, error) {
	return generateHash(plaintext, defaultParams)
}

func generateHash(plaintext string, params parameters) (string, error) {
	salt := make([]byte, params.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.Wrap(err, "unable to read random source")
	}

	hash := argon2idkey(plaintext, salt, params)

	// The format of the encoded hash string is taken from the reference implementation
	// https://github.com/P-H-C/phc-winner-argon2
	encoded := fmt.Sprintf(
		"$%s$v=%d$m=%d,t=%d,p=%d$%s$%s",
		"argon2id",
		argon2.Version,
		params.memory,
		params.time,
		params.threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	)

	return encoded, nil
}

func Compare(plaintext string, hash string) (bool, error) {
	// ["", "argon2id", version, params, salt, hash]
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false, errors.New("malformed hash string")
	}

	if parts[1] != "argon2id" {
		return false, errors.New("unsupported argon2 variant")
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, errors.Wrap(err, "unable to scan version")
	}

	if version != argon2.Version {
		return false, errors.New("argon2 version mismatch")
	}

	var params parameters

	_, err := fmt.Sscanf(
		parts[3],
		"m=%d,t=%d,p=%d",
		&params.memory,
		&params.time,
		&params.threads,
	)
	if err != nil {
		return false, errors.Wrap(err, "unable to scan parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errors.New("unable to decode salt")
	}

	h, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errors.New("unable to decode hash")
	}
	params.keyLen = uint32(len(h))

	compareTo := argon2idkey(plaintext, salt, params)
	return subtle.ConstantTimeCompare(h, compareTo) == 1, nil
}
