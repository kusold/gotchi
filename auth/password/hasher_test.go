package password

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHasher() *Argon2idHasher {
	return NewArgon2idHasher(HashingConfig{}.withDefaults())
}

func TestArgon2idHasher_HashAndVerify(t *testing.T) {
	h := newTestHasher()
	password := "correct-horse-battery-staple"

	hash, err := h.Hash(password)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)

	// Hash should be in PHC format
	assert.True(t, strings.HasPrefix(hash, "$argon2id$v="), "hash should start with $argon2id$v=")

	match, needsRehash, err := h.Verify(password, hash)
	require.NoError(t, err)
	assert.True(t, match)
	assert.False(t, needsRehash)
}

func TestArgon2idHasher_WrongPassword(t *testing.T) {
	h := newTestHasher()
	hash, err := h.Hash("correct-password")
	require.NoError(t, err)

	match, needsRehash, err := h.Verify("wrong-password", hash)
	require.NoError(t, err)
	assert.False(t, match)
	assert.False(t, needsRehash)
}

func TestArgon2idHasher_NeedsRehash(t *testing.T) {
	// Hash with default params
	defaultHasher := newTestHasher()
	hash, err := defaultHasher.Hash("my-password")
	require.NoError(t, err)

	// Verify with default hasher: no rehash needed
	match, needsRehash, err := defaultHasher.Verify("my-password", hash)
	require.NoError(t, err)
	assert.True(t, match)
	assert.False(t, needsRehash)

	// Verify with different params: rehash needed
	differentHasher := NewArgon2idHasher(HashingConfig{
		Memory:      32768, // different from default 19456
		Iterations:  2,
		Parallelism: 1,
		SaltLength:  16,
		KeyLength:   32,
	})
	match, needsRehash, err = differentHasher.Verify("my-password", hash)
	require.NoError(t, err)
	assert.True(t, match)
	assert.True(t, needsRehash, "should need rehash when params differ")
}

func TestArgon2idHasher_UniqueSalts(t *testing.T) {
	h := newTestHasher()
	password := "same-password"

	hash1, err := h.Hash(password)
	require.NoError(t, err)
	hash2, err := h.Hash(password)
	require.NoError(t, err)

	assert.NotEqual(t, hash1, hash2, "two hashes of the same password should differ (different salts)")

	// Both should verify correctly
	match, _, err := h.Verify(password, hash1)
	require.NoError(t, err)
	assert.True(t, match)

	match, _, err = h.Verify(password, hash2)
	require.NoError(t, err)
	assert.True(t, match)
}

func TestArgon2idHasher_InvalidHashFormat(t *testing.T) {
	h := newTestHasher()

	tests := []struct {
		name string
		hash string
	}{
		{"empty string", ""},
		{"wrong algorithm", "$bcrypt$v=1$m=10$abc$def"},
		{"too few parts", "$argon2id$v=19"},
		{"invalid base64", "$argon2id$v=19$m=19456,t=2,p=1$!!!$def"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := h.Verify("password", tt.hash)
			assert.Error(t, err)
		})
	}
}

func TestArgon2idHasher_SpecialCharacters(t *testing.T) {
	h := newTestHasher()
	passwords := []string{
		"P@$$w0rd!#%^&*()",
		"unicode: \u00e9\u00e8\u00ea\u00eb",
		"emoji: \U0001f600\U0001f60e",
		"spaces and\ttabs\nnewlines",
		strings.Repeat("a", 128),
	}
	for _, password := range passwords {
		t.Run("password_len_"+string(rune(len(password))), func(t *testing.T) {
			hash, err := h.Hash(password)
			require.NoError(t, err)

			match, _, err := h.Verify(password, hash)
			require.NoError(t, err)
			assert.True(t, match)
		})
	}
}
