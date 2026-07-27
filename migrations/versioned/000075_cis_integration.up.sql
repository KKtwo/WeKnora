ALTER TABLE knowledge_bases
    ADD COLUMN IF NOT EXISTS external_ref VARCHAR(255) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_kb_tenant_external_ref
    ON knowledge_bases (tenant_id, external_ref)
    WHERE external_ref <> '' AND deleted_at IS NULL;

ALTER TABLE data_sources
    ADD COLUMN IF NOT EXISTS external_ref VARCHAR(255) NOT NULL DEFAULT '';

CREATE UNIQUE INDEX IF NOT EXISTS idx_datasource_tenant_external_ref
    ON data_sources (tenant_id, external_ref)
    WHERE external_ref <> '' AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS documents (
    id VARCHAR(36) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    datasource_id VARCHAR(36) NOT NULL DEFAULT '',
    external_key VARCHAR(1024) NOT NULL,
    current_knowledge_id VARCHAR(36) NOT NULL,
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_document_datasource_external_key
    ON documents (datasource_id, external_key)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_documents_tenant_kb
    ON documents (tenant_id, knowledge_base_id)
    WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_documents_current_knowledge
    ON documents (current_knowledge_id);

ALTER TABLE knowledges
    ADD COLUMN IF NOT EXISTS document_id VARCHAR(36);

UPDATE knowledges
SET document_id = id
WHERE document_id IS NULL OR document_id = '';

INSERT INTO documents (
    id, tenant_id, knowledge_base_id, datasource_id, external_key,
    current_knowledge_id, status, created_at, updated_at
)
SELECT
    k.document_id,
    k.tenant_id,
    k.knowledge_base_id,
    COALESCE(k.metadata ->> 'datasource_id', ''),
    COALESCE(NULLIF(k.metadata ->> 'external_id', ''), k.document_id),
    k.id,
    k.parse_status,
    k.created_at,
    k.updated_at
FROM knowledges k
WHERE k.deleted_at IS NULL
ON CONFLICT DO NOTHING;

ALTER TABLE knowledges
    ALTER COLUMN document_id SET NOT NULL;

CREATE INDEX IF NOT EXISTS idx_knowledges_document_id
    ON knowledges (document_id);
