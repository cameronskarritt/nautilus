CREATE TABLE IF NOT EXISTS document_downloads (
    token_hash BYTEA PRIMARY KEY,
    organization_id BIGINT NOT NULL REFERENCES organizations(id),
    document_id BIGINT NOT NULL,
    api_key_id BIGINT,
    oauth_access_hash BYTEA REFERENCES mcp_oauth_tokens(access_hash),
    resource TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY(organization_id, document_id) REFERENCES documents(organization_id, id),
    FOREIGN KEY(organization_id, api_key_id) REFERENCES api_keys(organization_id, id)
);

CREATE INDEX IF NOT EXISTS idx_document_downloads_expires_at ON document_downloads(expires_at);
