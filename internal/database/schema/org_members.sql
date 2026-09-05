CREATE TABLE IF NOT EXISTS org_members (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    user_id BIGINT NOT NULL REFERENCES users(id),
    organization_id BIGINT NOT NULL REFERENCES organizations(id),

    role TEXT NOT NULL DEFAULT 'member',
    display_name TEXT,
    permissions JSONB,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,

    UNIQUE(user_id, organization_id)
);

CREATE INDEX IF NOT EXISTS idx_org_members_user_id_active ON org_members(user_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_members_organization_id_active ON org_members(organization_id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_org_members_external_id ON org_members(external_id);
