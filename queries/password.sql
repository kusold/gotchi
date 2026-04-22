-- Password authentication queries for sqlc code generation.

-- name: GetPasswordCredential :one
SELECT user_id, password_hash, hash_algorithm, created_at, updated_at
FROM password_credentials
WHERE user_id = $1;

-- name: UpsertPasswordCredential :exec
INSERT INTO password_credentials (user_id, password_hash, hash_algorithm)
VALUES ($1, $2, $3)
ON CONFLICT (user_id)
DO UPDATE SET password_hash = EXCLUDED.password_hash,
              hash_algorithm = EXCLUDED.hash_algorithm,
              updated_at = NOW();

-- name: GetUserByEmailAndIssuer :one
SELECT id, email, email_verified, username, name, issuer, identifier_subject, last_login_at, created_at, updated_at
FROM users
WHERE email = $1 AND issuer = $2;

-- name: GetUserByID :one
SELECT id, email, email_verified, username, name, issuer, identifier_subject, last_login_at, created_at, updated_at
FROM users
WHERE id = $1;

-- name: RecordLoginAttempt :exec
INSERT INTO login_attempts (user_id, ip_address, success)
VALUES ($1, $2, $3);

-- name: CountRecentFailedAttempts :one
SELECT COUNT(*)
FROM login_attempts
WHERE user_id = $1
  AND success = FALSE
  AND attempted_at > $2;

-- name: InsertAuthToken :exec
INSERT INTO auth_tokens (user_id, token_hash, token_type, expires_at)
VALUES ($1, $2, $3, $4);

-- name: ConsumeAuthToken :one
UPDATE auth_tokens
SET consumed_at = NOW()
WHERE token_hash = $1
  AND token_type = $2
  AND consumed_at IS NULL
  AND expires_at > NOW()
RETURNING user_id;

-- name: InvalidateUserTokens :exec
UPDATE auth_tokens
SET consumed_at = NOW()
WHERE user_id = $1
  AND token_type = $2
  AND consumed_at IS NULL;

-- name: UpdateEmailVerified :exec
UPDATE users
SET email_verified = TRUE, updated_at = NOW()
WHERE id = $1;

-- name: UpdateLastLoginAt :exec
UPDATE users
SET last_login_at = NOW(), updated_at = NOW()
WHERE id = $1;

-- name: UpdatePasswordHash :exec
UPDATE password_credentials
SET password_hash = $2, updated_at = NOW()
WHERE user_id = $1;

-- name: DeletePasswordCredential :exec
DELETE FROM password_credentials
WHERE user_id = $1;
