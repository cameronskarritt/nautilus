CREATE TABLE IF NOT EXISTS kms_keys (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    organization_id BIGINT UNIQUE REFERENCES organizations(id),
    provider_key_id TEXT UNIQUE NOT NULL,
    ciphertext BYTEA NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_kms_keys_users
    ON kms_keys((1)) WHERE organization_id IS NULL;
