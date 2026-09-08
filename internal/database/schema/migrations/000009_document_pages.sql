ALTER TABLE documents ADD COLUMN IF NOT EXISTS page_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE documents ADD COLUMN IF NOT EXISTS pdf_key TEXT NOT NULL DEFAULT '';

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
