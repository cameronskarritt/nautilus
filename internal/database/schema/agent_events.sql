CREATE TABLE IF NOT EXISTS agent_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
    stream_id BIGINT NOT NULL REFERENCES agent_streams(id),
    sequence BIGINT NOT NULL,
    type TEXT NOT NULL,
    source TEXT NOT NULL,
    idempotency_key TEXT,
    payload JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(stream_id, sequence)
);

CREATE INDEX IF NOT EXISTS idx_agent_events_stream_id ON agent_events(stream_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_events_external_id ON agent_events(external_id);
CREATE INDEX IF NOT EXISTS idx_agent_events_type ON agent_events(type);
CREATE INDEX IF NOT EXISTS idx_agent_events_source ON agent_events(source);
CREATE INDEX IF NOT EXISTS idx_agent_events_created_at ON agent_events(created_at);
CREATE INDEX IF NOT EXISTS idx_agent_events_stream_sequence ON agent_events(stream_id, sequence);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_events_idempotency_key
    ON agent_events(stream_id, type, idempotency_key)
    WHERE idempotency_key IS NOT NULL;
