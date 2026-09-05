CREATE TABLE IF NOT EXISTS sessions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),
    org_member_id BIGINT REFERENCES org_members(id),
    assumed_by BIGINT REFERENCES users(id),
    assumed_org_id BIGINT REFERENCES organizations(id),

    token_hash TEXT NOT NULL,
    ip_addr TEXT,
    user_agent TEXT,

    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_sessions_token_hash_active ON sessions(token_hash) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id_active ON sessions(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_org_member_id_active ON sessions(org_member_id) WHERE deleted_at IS NULL;
