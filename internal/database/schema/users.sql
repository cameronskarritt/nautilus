CREATE TABLE IF NOT EXISTS users (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    email TEXT UNIQUE,
    username TEXT UNIQUE NOT NULL,
    auth_provider TEXT NOT NULL DEFAULT 'local',
    auth_token TEXT,
    password_hash TEXT,
    totp_secret BYTEA,
    totp_pending_at TIMESTAMPTZ,
    mfa_enabled BOOLEAN NOT NULL DEFAULT false,

    verified BOOLEAN NOT NULL DEFAULT false,
    admin BOOLEAN NOT NULL DEFAULT false,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);


CREATE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);
