// Package crypto provides Fernet-based encryption compatible with the
// Python `cryptography` library's Fernet implementation, so values encrypted
// by the original Django service keep decrypting after the Go cutover, and
// vice versa (e.g. the example client SDKs in examples/).
package crypto

import (
	"errors"
	"fmt"

	"github.com/fernet/fernet-go"
)

// ErrInvalidToken mirrors cryptography.fernet.InvalidToken - returned when a
// value isn't a valid Fernet token under the given key (wrong/rotated key,
// corrupted data, or legacy plaintext that was never encrypted).
var ErrInvalidToken = errors.New("invalid fernet token")

// GenerateKey returns a new base64-encoded Fernet key, matching
// common/encryption.py's generate_encryption_key().
func GenerateKey() (string, error) {
	var k fernet.Key
	if err := k.Generate(); err != nil {
		return "", fmt.Errorf("generate fernet key: %w", err)
	}
	return k.Encode(), nil
}

// EncryptValue encrypts value with the given base64-encoded Fernet key.
// Mirrors common/encryption.py's encrypt_value().
func EncryptValue(value, encryptionKey string) (string, error) {
	if value == "" {
		return value, nil
	}

	key, err := fernet.DecodeKey(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decode fernet key: %w", err)
	}

	token, err := fernet.EncryptAndSign([]byte(value), key)
	if err != nil {
		return "", fmt.Errorf("fernet encrypt: %w", err)
	}
	return string(token), nil
}

// DecryptValue decrypts an encryptedValue produced by EncryptValue (or the
// Python EncryptionService) using the given base64-encoded Fernet key.
// Mirrors common/encryption.py's decrypt_value().
func DecryptValue(encryptedValue, encryptionKey string) (string, error) {
	if encryptedValue == "" {
		return encryptedValue, nil
	}

	key, err := fernet.DecodeKey(encryptionKey)
	if err != nil {
		return "", fmt.Errorf("decode fernet key: %w", err)
	}

	// A zero ttl (time-to-live) disables expiry checking, matching the
	// Python side which never passes a ttl to Fernet.decrypt().
	msg := fernet.VerifyAndDecrypt([]byte(encryptedValue), 0, []*fernet.Key{key})
	if msg == nil {
		return "", ErrInvalidToken
	}
	return string(msg), nil
}

// EncryptionService mirrors common/encryption.py's EncryptionService.
type EncryptionService struct {
	MasterKey string
}

func NewEncryptionService(masterKey string) *EncryptionService {
	return &EncryptionService{MasterKey: masterKey}
}

// EncryptForStorage encrypts value with the master key for storage in the DB.
func (s *EncryptionService) EncryptForStorage(value string) (string, error) {
	return EncryptValue(value, s.MasterKey)
}

// DecryptFromStorage decrypts a value previously stored with EncryptForStorage.
func (s *EncryptionService) DecryptFromStorage(encryptedValue string) (string, error) {
	return DecryptValue(encryptedValue, s.MasterKey)
}

// ReEncryptForClient re-encrypts an already-decrypted value with a client's
// own encryption key, for handing off over the S2S API.
func (s *EncryptionService) ReEncryptForClient(decryptedValue, clientKey string) (string, error) {
	return EncryptValue(decryptedValue, clientKey)
}
