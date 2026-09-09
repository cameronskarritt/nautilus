CREATE TABLE IF NOT EXISTS webhooks (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4() UNIQUE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    name TEXT NOT NULL,
    url TEXT NOT NULL,
    event_types TEXT[] NOT NULL DEFAULT '{}',
    enabled BOOLEAN NOT NULL DEFAULT true,
    signing_secret BYTEA NOT NULL,
    previous_signing_secret BYTEA,
    previous_secret_expires_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,
    UNIQUE(organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_webhooks_organization_active
    ON webhooks(organization_id, id DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS events (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4() UNIQUE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    type TEXT NOT NULL,
    schema_version INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL,
    payload JSONB NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(organization_id, idempotency_key),
    UNIQUE(organization_id, id)
);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4() UNIQUE,
    organization_id BIGINT NOT NULL,
    webhook_id BIGINT NOT NULL,
    event_id BIGINT NOT NULL,
    trigger TEXT NOT NULL,
    replayed_id BIGINT,
    request_key TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    UNIQUE(organization_id, request_key),
    UNIQUE(organization_id, id),
    FOREIGN KEY(organization_id, webhook_id) REFERENCES webhooks(organization_id, id),
    FOREIGN KEY(organization_id, event_id) REFERENCES events(organization_id, id),
    FOREIGN KEY(organization_id, replayed_id) REFERENCES webhook_deliveries(organization_id, id)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_deliveries_event
    ON webhook_deliveries(organization_id, webhook_id, event_id)
    WHERE trigger = 'event';

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_event_id
    ON webhook_deliveries(organization_id, event_id, id);

CREATE INDEX IF NOT EXISTS idx_webhook_deliveries_webhook_id
    ON webhook_deliveries(organization_id, webhook_id, id DESC);

CREATE TABLE IF NOT EXISTS webhook_attempts (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id BIGINT NOT NULL,
    delivery_id BIGINT NOT NULL,
    attempt_key TEXT NOT NULL,
    url TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    finished_at TIMESTAMPTZ,
    http_status INTEGER,
    error_code TEXT,
    UNIQUE(organization_id, delivery_id, attempt_key),
    FOREIGN KEY(organization_id, delivery_id) REFERENCES webhook_deliveries(organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_webhook_attempts_delivery_id
    ON webhook_attempts(organization_id, delivery_id, id DESC);
