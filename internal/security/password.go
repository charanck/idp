// Package security hashes and verifies passwords (and S2S API-key secrets,
// which use the same hasher). New hashes use argon2id. Hashes created before
// the argon2id cutover use a legacy PBKDF2 format
// ("pbkdf2_sha256$<iterations>$<salt>$<base64hash>") that verification keeps
// supporting indefinitely, so existing passwords/API keys keep working
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

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

const (
	algorithm = "pbkdf2_sha256"

	// Iterations used for NEW legacy-format hashes. No longer used by
	// HashPassword (which now produces argon2id), kept only so
	// verifyPBKDF2 has a symmetric encode/decode pair for testing. Existing
	// legacy hashes embed their own iteration count and are verified
	// against that.
	defaultIterations = 600_000

	saltAlphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	saltLength   = 12
	keyLength    = 32 // sha256 digest size
)

const (
	argon2idPrefix    = "argon2id"
	argon2Memory      = 19 * 1024 // KiB (19 MiB) - OWASP baseline for a server also handling S2S API-key auth
	argon2Iterations  = 2
	argon2Parallelism = 1
	argon2SaltLength  = 16
	argon2KeyLength   = 32
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

// HashPassword hashes a plaintext password/secret into argon2id format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argon2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	hash := argon2.IDKey([]byte(password), salt, argon2Iterations, argon2Memory, argon2Parallelism, argon2KeyLength)
	return fmt.Sprintf("%s$v=19$m=%d,t=%d,p=%d$%s$%s",
		argon2idPrefix, argon2Memory, argon2Iterations, argon2Parallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(hash)), nil
}

func encode(password, salt string, iterations int) string {
	hash := pbkdf2.Key([]byte(password), []byte(salt), iterations, keyLength, sha256.New)
	encodedHash := base64.StdEncoding.EncodeToString(hash)
	return fmt.Sprintf("%s$%d$%s$%s", algorithm, iterations, salt, encodedHash)
}

// IsLegacyHash reports whether encoded is in the legacy PBKDF2 format,
// i.e. it has not yet been rehashed to argon2id.
func IsLegacyHash(encoded string) bool {
	return strings.HasPrefix(encoded, algorithm+"$")
}

// VerifyPassword checks a plaintext password/secret against an encoded hash
// produced by either this package's argon2id HashPassword or the legacy
// PBKDF2 format.
func VerifyPassword(password, encoded string) bool {
	if strings.HasPrefix(encoded, argon2idPrefix+"$") {
		return verifyArgon2id(password, encoded)
	}
	return verifyPBKDF2(password, encoded)
}

// verifyArgon2id re-derives the hash using the parameters embedded in
// encoded (not the current constants), so previously issued hashes keep
// verifying correctly even if the constants are tuned later.
func verifyArgon2id(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != argon2idPrefix {
		return false
	}

	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	candidate := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(expectedHash)))
	return subtle.ConstantTimeCompare(candidate, expectedHash) == 1
}

// verifyPBKDF2 checks password against the legacy
// "pbkdf2_sha256$iterations$salt$hash" format.
func verifyPBKDF2(password, encoded string) bool {
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
