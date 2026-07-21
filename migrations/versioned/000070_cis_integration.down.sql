DROP INDEX IF EXISTS idx_knowledges_document_id;
ALTER TABLE knowledges DROP COLUMN IF EXISTS document_id;

DROP TABLE IF EXISTS documents;

DROP INDEX IF EXISTS idx_datasource_tenant_external_ref;
ALTER TABLE data_sources DROP COLUMN IF EXISTS external_ref;

DROP INDEX IF EXISTS idx_kb_tenant_external_ref;
ALTER TABLE knowledge_bases DROP COLUMN IF EXISTS external_ref;
