-- name: GetUserByIdentifier :one
SELECT
    id,
    issuer,
    identifier_subject
FROM users
WHERE issuer = $1 AND identifier_subject = $2;

-- name: InsertUser :one
INSERT INTO users (
    id,
    email,
    email_verified,
    username,
    name,
    issuer,
    identifier_subject,
    last_login_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8
)
RETURNING id, issuer, identifier_subject;

-- name: ListMemberships :many
SELECT
    tm.tenant_id,
    t.name AS tenant_name,
    tm.role
FROM tenant_memberships tm
JOIN tenants t ON t.tenant_id = tm.tenant_id
WHERE tm.user_id = $1
ORDER BY tm.created_at;

-- name: GetTenantByID :one
SELECT tenant_id, name
FROM tenants
WHERE tenant_id = $1;

-- name: GetFirstTenant :one
SELECT tenant_id
FROM tenants
ORDER BY created_at
LIMIT 1;

-- name: InsertTenant :exec
INSERT INTO tenants (tenant_id, name)
VALUES ($1, $2);

-- name: UpsertMembership :one
INSERT INTO tenant_memberships (user_id, tenant_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, tenant_id)
DO UPDATE SET role = EXCLUDED.role
RETURNING tenant_id, role;

-- name: GetMembershipByUserAndTenant :one
SELECT tenant_id, role
FROM tenant_memberships
WHERE user_id = $1 AND tenant_id = $2;
