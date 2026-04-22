package password

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

// GenerateToken generates a cryptographically secure random token and its
// SHA-256 hash. The plaintext token is intended to be sent to the user
// (e.g. in an email link). Only the SHA-256 hash is stored in the database.
func GenerateToken(length int) (plaintext string, hash []byte, err error) {
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("failed to generate token: %w", err)
	}

	plaintext = base64.RawURLEncoding.EncodeToString(raw)
	hash = sha256Sum(plaintext)
	return plaintext, hash, nil
}

// sha256Sum returns the SHA-256 hash of the input string. Used for token
// storage and lookup.
func sha256Sum(s string) []byte {
	h := sha256.Sum256([]byte(s))
	return h[:]
}
