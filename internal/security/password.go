// Package security hashes and verifies passwords (and S2S API-key secrets,
// which use the same hasher) using a format compatible with Django's
// PBKDF2PasswordHasher ("pbkdf2_sha256$<iterations>$<salt>$<base64hash>"),
// so passwords/API keys created before the Go cutover keep verifying
// without a forced reset.
package security

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

const (
	algorithm = "pbkdf2_sha256"

	// Iterations used for NEW hashes created by this Go service. Existing
	// Django-created hashes embed their own iteration count and are verified
	// against that, so this only needs to be a solid modern default (OWASP
	// currently recommends >=600,000 for PBKDF2-HMAC-SHA256), not match
	// Django's release-specific default.
	defaultIterations = 600_000

	saltAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	saltLength   = 12
	keyLength    = 32 // sha256 digest size
)

func generateSalt() (string, error) {
	b := make([]byte, saltLength)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	out := make([]byte, saltLength)
	for i, v := range b {
		out[i] = saltAlphabet[int(v)%len(saltAlphabet)]
	}
	return string(out), nil
}

// HashPassword hashes a plaintext password/secret into Django's
// "pbkdf2_sha256$iterations$salt$hash" format.
func HashPassword(password string) (string, error) {
	salt, err := generateSalt()
	if err != nil {
		return "", err
	}
	return encode(password, salt, defaultIterations), nil
}

func encode(password, salt string, iterations int) string {
	hash := pbkdf2.Key([]byte(password), []byte(salt), iterations, keyLength, sha256.New)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	return fmt.Sprintf("%s$%d$%s$%s", algorithm, iterations, salt, encodedHash)
}

// VerifyPassword checks a plaintext password/secret against an encoded hash
// produced by either this package or Django's make_password().
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != algorithm {
		return false
	}

	iterations, err := strconv.Atoi(parts[1])
	if err != nil || iterations <= 0 {
		return false
	}
	salt := parts[2]
	expectedHash := parts[3]

	candidate := encode(password, salt, iterations)
	candidateParts := strings.SplitN(candidate, "$", 4)
	if len(candidateParts) != 4 {
		return false
	}

	return subtle.ConstantTimeCompare([]byte(candidateParts[3]), []byte(expectedHash)) == 1
}
