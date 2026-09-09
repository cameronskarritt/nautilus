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
    page_count INTEGER NOT NULL DEFAULT 0,
    pdf_key TEXT NOT NULL DEFAULT '',
    sha256 TEXT NOT NULL DEFAULT '',
    UNIQUE(organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_documents_organization_id
    ON documents(organization_id, id DESC);

CREATE TABLE IF NOT EXISTS document_pages (
    organization_id BIGINT NOT NULL,
    document_id BIGINT NOT NULL,
    number INTEGER NOT NULL,
    content_type TEXT NOT NULL,
    size BIGINT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    PRIMARY KEY(organization_id, document_id, number),
    FOREIGN KEY(organization_id, document_id) REFERENCES documents(organization_id, id)
);
