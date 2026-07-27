# CIS management-plane integration

WeKnora owns source configuration, synchronization, parsing, original objects and retrieval. CIS owns users, ACL, Agent scope and the user-facing management plane. CIS uses one tenant and two server-side API keys:

- retrieve: `retrieve`, optionally restricted to the CIS-managed KB allow-list;
- manage: `manage_kbs`, `ingest`, `manage_datasources`, `retrieve`.

The keys and the WeKnora URL must never be sent to browsers or Agent workspaces.

## Stable integration identifiers

Knowledge-base create accepts `external_ref=cis:kb:{cis_kb_id}` and datasource create accepts `external_ref=cis:kb:{cis_kb_id}:source:{uuid}`. Each value is tenant-unique; a repeated create returns the existing object.

`documents.id` is a stable logical UUID. `knowledges.document_id` points to it while `documents.current_knowledge_id` selects the current engine object. Connector metadata must include `document_id`, `datasource_type`, `external_id`, `source_url`, `source_revision`, and `source_updated_at`.

File ingestion is idempotent for an unchanged current resource with the same non-empty `(datasource_id, external_id)`: it returns the existing Knowledge as success. A changed or reverted body creates a new engine Knowledge and advances `documents.current_knowledge_id`; historical hashes never suppress a new current version. User-facing lists and counters include only the current Knowledge for each logical document. Duplicate uploads from another source keep the normal `409` behavior.

Retrieval accepts stable `document_ids` only together with explicit `knowledge_base_ids`; it resolves them to current Knowledge IDs inside the tenant and KB scope. Search results include both stable source fields and engine `knowledge_id` / chunk ID.

## Readiness

`GET /api/v1/knowledge-bases/{id}/readiness` returns parse-state counts, running sync count, latest sync and `ready`. Ready means no synchronization is running and every effective current Knowledge is enabled and completed/finalizing.

`GET /api/v1/knowledge-bases/{id}/documents/{document_id}` resolves a logical document without exposing a tenant-wide lookup.

## Storage migration

Apply `migrations/versioned/000075_cis_integration.up.sql` before enabling CIS. PostgreSQL is the supported retrieval driver for the initial CIS rollout. PostgreSQL and object storage must be backed up together; Redis is never a knowledge fact source.

The production application image is built with the `postgres_only` Go build tag. This excludes the SQLite database, migration and retrieval drivers from the server binary. Production deployments must set both `DB_DRIVER=postgres` and `RETRIEVE_DRIVER=postgres`; use the regular or Lite build when SQLite support is required.

Immutable document versions and atomic source releases remain disabled until their database-level pre-recall filtering is implemented. They must not be emulated by over-fetching and filtering after top-k retrieval.
