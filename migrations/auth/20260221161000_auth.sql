-- +goose Up
CREATE TABLE IF NOT EXISTS tenants (
    tenant_id UUID PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY,
    tenant_id UUID REFERENCES tenants(tenant_id) ON DELETE RESTRICT,
    email VARCHAR(255) NOT NULL,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    username VARCHAR(255),
    name VARCHAR(255),
    issuer VARCHAR(255) NOT NULL,
    identifier_subject VARCHAR(255) NOT NULL,
    last_login_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT unique_issuer_subject UNIQUE (issuer, identifier_subject)
);

CREATE TABLE IF NOT EXISTS tenant_memberships (
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tenant_id UUID NOT NULL REFERENCES tenants(tenant_id) ON DELETE CASCADE,
    role VARCHAR(16) NOT NULL DEFAULT 'member',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_memberships_pk PRIMARY KEY (user_id, tenant_id),
    CONSTRAINT tenant_memberships_role_check CHECK (role IN ('owner', 'admin', 'member'))
);
CREATE INDEX IF NOT EXISTS tenant_memberships_user_id_idx ON tenant_memberships (user_id);
CREATE INDEX IF NOT EXISTS tenant_memberships_tenant_id_idx ON tenant_memberships (tenant_id);

-- +goose Down
DROP INDEX IF EXISTS tenant_memberships_tenant_id_idx;
DROP INDEX IF EXISTS tenant_memberships_user_id_idx;
DROP TABLE IF EXISTS tenant_memberships;
DROP TABLE IF EXISTS users;
DROP TABLE IF EXISTS tenants;
