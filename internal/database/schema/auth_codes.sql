CREATE TABLE IF NOT EXISTS auth_codes (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id),

    type TEXT NOT NULL,
    token_hash TEXT,
    data JSONB,

    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_auth_codes_token_hash_valid ON auth_codes(token_hash) WHERE deleted_at IS NULL;
