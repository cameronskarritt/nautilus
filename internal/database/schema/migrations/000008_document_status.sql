CREATE TABLE IF NOT EXISTS documents (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    external_id UUID NOT NULL DEFAULT uuid_generate_v4() UNIQUE,
    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'uploading',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(organization_id, id)
);

ALTER TABLE documents ALTER COLUMN status SET DEFAULT 'uploading';

UPDATE documents SET status = 'uploaded' WHERE status = 'ready';
UPDATE documents SET status = 'failed' WHERE status = 'pending';

DROP INDEX IF EXISTS idx_documents_organization_status_id;
CREATE INDEX IF NOT EXISTS idx_documents_organization_id
    ON documents(organization_id, id DESC);
