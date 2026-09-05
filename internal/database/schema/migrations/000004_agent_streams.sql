CREATE TABLE IF NOT EXISTS agent_streams (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
    user_id BIGINT NOT NULL REFERENCES users(id),
    org_id BIGINT NOT NULL REFERENCES organizations(id),
    status TEXT NOT NULL DEFAULT 'pending',
    fence_token BIGINT NOT NULL DEFAULT 0,
    title TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_streams_external_id ON agent_streams(external_id);
CREATE INDEX IF NOT EXISTS idx_agent_streams_status ON agent_streams(status);
CREATE INDEX IF NOT EXISTS idx_agent_streams_user_id ON agent_streams(user_id);
CREATE INDEX IF NOT EXISTS idx_agent_streams_org_id ON agent_streams(org_id);
