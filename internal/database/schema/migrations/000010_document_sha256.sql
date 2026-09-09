ALTER TABLE documents ADD COLUMN IF NOT EXISTS sha256 TEXT NOT NULL DEFAULT '';

UPDATE documents
SET sha256 = split_part(pdf_key, '/', 4)
WHERE sha256 = ''
    AND pdf_key ~ ('^documents/' || external_id::text || '/pdf/[0-9a-f]{64}$');
