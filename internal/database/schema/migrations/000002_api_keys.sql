CREATE TABLE IF NOT EXISTS api_keys (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    created_by BIGINT NOT NULL REFERENCES users(id),

    name TEXT NOT NULL,
    token_hash BYTEA NOT NULL,
    prefix TEXT NOT NULL,
    scopes TEXT[] NOT NULL DEFAULT '{}',

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,

    UNIQUE(organization_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_external_id ON api_keys(external_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_token_hash ON api_keys(token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_organization_name_active
    ON api_keys(organization_id, LOWER(name))
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_organization_created_active
    ON api_keys(organization_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;
