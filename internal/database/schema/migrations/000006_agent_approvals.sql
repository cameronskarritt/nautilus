CREATE TABLE IF NOT EXISTS agent_approvals (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
    stream_id BIGINT NOT NULL REFERENCES agent_streams(id),
    status TEXT NOT NULL DEFAULT 'pending',
    tool_calls JSONB NOT NULL,
    reason TEXT,
    approved_by BIGINT REFERENCES users(id),
    approver_message TEXT,
    requested_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_approvals_external_id ON agent_approvals(external_id);
CREATE INDEX IF NOT EXISTS idx_agent_approvals_stream_id ON agent_approvals(stream_id);
CREATE INDEX IF NOT EXISTS idx_agent_approvals_status ON agent_approvals(status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_approvals_stream_pending
    ON agent_approvals(stream_id)
    WHERE status = 'pending';
