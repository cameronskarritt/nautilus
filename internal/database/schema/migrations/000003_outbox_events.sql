CREATE TABLE IF NOT EXISTS outbox_events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),
    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    topic TEXT NOT NULL,
    aggregate_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload JSONB NOT NULL DEFAULT '{}',
    available_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ,
    attempts INTEGER NOT NULL DEFAULT 0,
    lease_token UUID,
    lease_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(organization_id, idempotency_key)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_outbox_events_external_id ON outbox_events(external_id);
CREATE INDEX IF NOT EXISTS idx_outbox_events_available
    ON outbox_events(topic, available_at, id)
    WHERE processed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_outbox_events_aggregate
    ON outbox_events(organization_id, topic, aggregate_id, created_at);
