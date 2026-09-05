CREATE TABLE IF NOT EXISTS org_invites (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    invited_by BIGINT NOT NULL REFERENCES users(id),

    email TEXT NOT NULL,
    role TEXT NOT NULL,
    token_hash TEXT NOT NULL,

    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    redeemed_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_org_invites_token_hash ON org_invites(token_hash) WHERE deleted_at IS NULL AND redeemed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_invites_org_id ON org_invites(organization_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_invites_email ON org_invites(email) WHERE deleted_at IS NULL AND redeemed_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_invites_external_id ON org_invites(external_id);
