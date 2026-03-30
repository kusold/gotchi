-- +goose Up
-- +goose StatementBegin
-- Remove tenant_id from users table
-- User-tenant relationships are now exclusively managed via tenant_memberships

-- First, create memberships for any users that don't have one but have a tenant_id
INSERT INTO tenant_memberships (user_id, tenant_id, role, created_at, updated_at)
SELECT id, tenant_id, 'member', NOW(), NOW()
FROM users
WHERE tenant_id IS NOT NULL
AND NOT EXISTS (
    SELECT 1 FROM tenant_memberships tm WHERE tm.user_id = users.id
);

-- Now drop the tenant_id column
ALTER TABLE users DROP COLUMN tenant_id;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Re-add tenant_id column
ALTER TABLE users ADD COLUMN tenant_id UUID REFERENCES tenants(tenant_id) ON DELETE RESTRICT;

-- Migrate back: set tenant_id to the first membership's tenant
UPDATE users u
SET tenant_id = (
    SELECT tm.tenant_id 
    FROM tenant_memberships tm 
    WHERE tm.user_id = u.id 
    ORDER BY tm.created_at 
    LIMIT 1
)
WHERE EXISTS (
    SELECT 1 FROM tenant_memberships tm WHERE tm.user_id = u.id
);
-- +goose StatementEnd
