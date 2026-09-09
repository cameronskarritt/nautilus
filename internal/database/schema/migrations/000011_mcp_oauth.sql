CREATE TABLE IF NOT EXISTS mcp_oauth_clients (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    redirect_uris TEXT[] NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS mcp_oauth_grants (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    client_id TEXT NOT NULL REFERENCES mcp_oauth_clients(id),
    user_id BIGINT NOT NULL REFERENCES users(id),
    member_id BIGINT NOT NULL REFERENCES org_members(id),
    scope TEXT NOT NULL,
    resource TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS mcp_oauth_codes (
    hash BYTEA PRIMARY KEY,
    grant_id UUID NOT NULL UNIQUE REFERENCES mcp_oauth_grants(id),
    redirect_uri TEXT NOT NULL,
    challenge TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS mcp_oauth_tokens (
    access_hash BYTEA PRIMARY KEY,
    refresh_hash BYTEA NOT NULL UNIQUE,
    grant_id UUID NOT NULL REFERENCES mcp_oauth_grants(id),
    scope TEXT NOT NULL,
    access_expires_at TIMESTAMPTZ NOT NULL,
    refresh_used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_mcp_oauth_tokens_grant ON mcp_oauth_tokens(grant_id);
