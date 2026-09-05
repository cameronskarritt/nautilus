CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    -- Actor who performed the action
    actor_id BIGINT NOT NULL REFERENCES users(id),

    -- Event classification
    type TEXT NOT NULL,

    -- Optional target (org for impersonation, could be user/resource for other events)
    target_org_id BIGINT REFERENCES organizations(id),

    -- Flexible payload for event-specific data
    payload JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_audit_logs_actor_id ON audit_logs(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_logs_type ON audit_logs(type);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON audit_logs(created_at);
