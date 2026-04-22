package password

import (
	"crypto/sha256"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateToken(t *testing.T) {
	plaintext, hash, err := GenerateToken(32)
	require.NoError(t, err)
	assert.NotEmpty(t, plaintext)
	assert.Len(t, hash, 32, "SHA-256 hash should be 32 bytes")

	// Verify the hash matches
	expected := sha256.Sum256([]byte(plaintext))
	assert.Equal(t, expected[:], hash)
}

func TestGenerateToken_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		plaintext, _, err := GenerateToken(32)
		require.NoError(t, err)
		assert.False(t, seen[plaintext], "token should be unique, got duplicate at iteration %d", i)
		seen[plaintext] = true
	}
}

func TestGenerateToken_Length(t *testing.T) {
	tests := []struct {
		name       string
		rawBytes   int
		wantEncLen int // base64url encoded length (no padding)
	}{
		{"16 bytes", 16, 22},
		{"32 bytes", 32, 43},
		{"48 bytes", 48, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plaintext, _, err := GenerateToken(tt.rawBytes)
			require.NoError(t, err)

			// Verify the plaintext is valid base64url
			decoded, err := base64.RawURLEncoding.DecodeString(plaintext)
			require.NoError(t, err)
			assert.Len(t, decoded, tt.rawBytes)

			// Verify no + or / characters (url-safe)
			assert.False(t, strings.ContainsAny(plaintext, "+/="))
		})
	}
}

func TestGenerateToken_Base64URLEncoding(t *testing.T) {
	plaintext, _, err := GenerateToken(32)
	require.NoError(t, err)

	// Should only contain base64url characters
	for _, c := range plaintext {
		assert.True(t,
			(c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_',
			"unexpected character in token: %c", c,
		)
	}
}

func TestSha256Sum(t *testing.T) {
	input := "test-token-value"
	got := sha256Sum(input)
	expected := sha256.Sum256([]byte(input))
	assert.Equal(t, expected[:], got)
}
