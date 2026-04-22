package password

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Hasher hashes and verifies passwords using Argon2id.
type Hasher interface {
	// Hash returns an encoded hash string (PHC format) for the given password.
	Hash(password string) (string, error)
	// Verify checks a password against an encoded hash string.
	// Returns (match, needsRehash, error). If needsRehash is true, the caller
	// should re-hash and update storage with current parameters.
	Verify(password, encodedHash string) (bool, bool, error)
}

// Argon2idHasher implements [Hasher] using Argon2id with configurable
// parameters. Hashes are stored in PHC string format for self-describing
// parameter migration.
type Argon2idHasher struct {
	cfg HashingConfig
}

// NewArgon2idHasher creates a new Argon2id hasher with the given parameters.
func NewArgon2idHasher(cfg HashingConfig) *Argon2idHasher {
	return &Argon2idHasher{cfg: cfg}
}

// Hash generates an Argon2id hash of the password in PHC string format:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<base64-salt>$<base64-hash>
func (h *Argon2idHasher) Hash(password string) (string, error) {
	salt := make([]byte, h.cfg.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, h.cfg.Iterations, h.cfg.Memory, h.cfg.Parallelism, h.cfg.KeyLength)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		h.cfg.Memory,
		h.cfg.Iterations,
		h.cfg.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// Verify checks a password against a stored PHC-format hash. It returns
// whether the password matches, whether the hash needs rehashing (parameters
// differ from current config), and any error.
func (h *Argon2idHasher) Verify(password, encodedHash string) (bool, bool, error) {
	params, salt, hash, err := decodePHC(encodedHash)
	if err != nil {
		return false, false, fmt.Errorf("failed to decode hash: %w", err)
	}

	computedHash := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)

	match := subtle.ConstantTimeCompare(hash, computedHash) == 1
	needsRehash := match && h.paramsDiffer(params)

	return match, needsRehash, nil
}

// paramsDiffer returns true if the stored hash parameters differ from the
// current hasher configuration, indicating the password should be re-hashed
// on next successful login.
func (h *Argon2idHasher) paramsDiffer(stored hashingParams) bool {
	return stored.Memory != h.cfg.Memory ||
		stored.Iterations != h.cfg.Iterations ||
		stored.Parallelism != h.cfg.Parallelism ||
		stored.SaltLength != h.cfg.SaltLength ||
		stored.KeyLength != h.cfg.KeyLength
}

type hashingParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// decodePHC parses a PHC string format hash into its components.
// Format: $argon2id$v=19$m=19456,t=2,p=1$<base64-salt>$<base64-hash>
func decodePHC(encoded string) (hashingParams, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	// Expected: ["", "argon2id", "v=19", "m=19456,t=2,p=1", "<salt>", "<hash>"]
	if len(parts) != 6 {
		return hashingParams{}, nil, nil, fmt.Errorf("invalid hash format: expected 6 parts, got %d", len(parts))
	}

	if parts[1] != "argon2id" {
		return hashingParams{}, nil, nil, fmt.Errorf("unsupported algorithm: %s", parts[1])
	}

	// Parse version
	versionStr := strings.TrimPrefix(parts[2], "v=")
	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return hashingParams{}, nil, nil, fmt.Errorf("invalid version: %s", parts[2])
	}
	if version != argon2.Version {
		return hashingParams{}, nil, nil, fmt.Errorf("unsupported argon2 version: %d", version)
	}

	// Parse parameters: m=19456,t=2,p=1
	params, err := parseParams(parts[3])
	if err != nil {
		return hashingParams{}, nil, nil, err
	}

	// Decode salt
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return hashingParams{}, nil, nil, fmt.Errorf("failed to decode salt: %w", err)
	}
	params.SaltLength = uint32(len(salt))

	// Decode hash
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return hashingParams{}, nil, nil, fmt.Errorf("failed to decode hash: %w", err)
	}
	params.KeyLength = uint32(len(hash))

	return params, salt, hash, nil
}

func parseParams(s string) (hashingParams, error) {
	var params hashingParams
	pairs := strings.Split(s, ",")
	for _, pair := range pairs {
		kv := strings.SplitN(pair, "=", 2)
		if len(kv) != 2 {
			return hashingParams{}, fmt.Errorf("invalid parameter: %s", pair)
		}
		switch kv[0] {
		case "m":
			v, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return hashingParams{}, fmt.Errorf("invalid memory: %s", kv[1])
			}
			params.Memory = uint32(v)
		case "t":
			v, err := strconv.ParseUint(kv[1], 10, 32)
			if err != nil {
				return hashingParams{}, fmt.Errorf("invalid iterations: %s", kv[1])
			}
			params.Iterations = uint32(v)
		case "p":
			v, err := strconv.ParseUint(kv[1], 10, 8)
			if err != nil {
				return hashingParams{}, fmt.Errorf("invalid parallelism: %s", kv[1])
			}
			params.Parallelism = uint8(v)
		default:
			return hashingParams{}, fmt.Errorf("unknown parameter: %s", kv[0])
		}
	}
	return params, nil
}
