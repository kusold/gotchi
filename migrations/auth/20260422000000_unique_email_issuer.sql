-- +goose Up
-- +goose StatementBegin

-- Ensures no two users share the same email within the same issuer.
-- This prevents a TOCTOU race during concurrent registration where two
-- requests for the same email could both pass the application-level check
-- before either inserts a row.
-- The partial index (WHERE email <> '') intentionally excludes rows with no
-- email address; empty email is not an identity and must not be treated as
-- unique, so multiple rows with email='' are allowed.
CREATE UNIQUE INDEX IF NOT EXISTS users_email_issuer_uniq
    ON users (email, issuer)
    WHERE email <> '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS users_email_issuer_uniq;
-- +goose StatementEnd
