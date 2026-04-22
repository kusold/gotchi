-- +goose Up
-- +goose StatementBegin

-- Stores hashed password credentials linked to users.
-- A user may have zero or one password credential.
-- Separating from the users table allows OIDC-only users to coexist
-- and enables future credential types (passkeys, API keys).
CREATE TABLE IF NOT EXISTS password_credentials (
    user_id         UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    password_hash   TEXT NOT NULL,
    hash_algorithm  VARCHAR(32) NOT NULL DEFAULT 'argon2id',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Tracks failed login attempts for account lockout.
-- Uses a sliding window model: rows older than the lockout window
-- are periodically pruned.
CREATE TABLE IF NOT EXISTS login_attempts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    attempted_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ip_address      INET,
    success         BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX IF NOT EXISTS login_attempts_user_id_idx
    ON login_attempts (user_id, attempted_at DESC);
CREATE INDEX IF NOT EXISTS login_attempts_ip_idx
    ON login_attempts (ip_address, attempted_at DESC);

-- Stores single-use tokens for password resets and email verification.
-- Tokens are hashed (SHA-256) before storage; the plaintext is sent
-- to the user and never persisted.
CREATE TABLE IF NOT EXISTS auth_tokens (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      BYTEA NOT NULL,
    token_type      VARCHAR(32) NOT NULL,
    expires_at      TIMESTAMPTZ NOT NULL,
    consumed_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT auth_tokens_type_check CHECK (token_type IN ('password_reset', 'email_verification'))
);
CREATE INDEX IF NOT EXISTS auth_tokens_user_type_idx
    ON auth_tokens (user_id, token_type, expires_at)
    WHERE consumed_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS auth_tokens_user_type_idx;
DROP TABLE IF EXISTS auth_tokens;
DROP INDEX IF EXISTS login_attempts_ip_idx;
DROP INDEX IF EXISTS login_attempts_user_id_idx;
DROP TABLE IF EXISTS login_attempts;
DROP TABLE IF EXISTS password_credentials;
-- +goose StatementEnd
