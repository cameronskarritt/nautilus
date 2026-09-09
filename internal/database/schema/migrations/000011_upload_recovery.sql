ALTER TABLE documents ADD COLUMN IF NOT EXISTS upload_token UUID NOT NULL DEFAULT uuid_generate_v4();
ALTER TABLE documents ADD COLUMN IF NOT EXISTS upload_expires_at TIMESTAMPTZ NOT NULL DEFAULT (CURRENT_TIMESTAMP + INTERVAL '15 minutes');
ALTER TABLE documents ADD COLUMN IF NOT EXISTS upload_ready BOOLEAN NOT NULL DEFAULT FALSE;
CREATE INDEX IF NOT EXISTS idx_documents_upload_recovery ON documents(id) WHERE status = 'uploading';
