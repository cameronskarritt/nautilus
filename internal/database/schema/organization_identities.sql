CREATE TABLE IF NOT EXISTS organization_identities (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4(),

    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    provider TEXT NOT NULL,
    provider_id TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ,

    UNIQUE(provider, provider_id),
    UNIQUE(organization_id, provider)
);

CREATE INDEX IF NOT EXISTS idx_organization_identities_organization_id_active
    ON organization_identities(organization_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_organization_identities_external_id
    ON organization_identities(external_id);
