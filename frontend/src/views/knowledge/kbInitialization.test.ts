import assert from 'node:assert/strict'
import test from 'node:test'

import { requiresKnowledgeBaseInitialization } from './kbInitialization'

test('allows a CIS-managed retrieval knowledge base without a summary model', () => {
  assert.equal(
    requiresKnowledgeBaseInitialization({
      external_ref: 'cis:kb:4',
      embedding_model_id: 'embedding-model',
    }),
    false,
  )
})

test('still requires an embedding model for a CIS-managed RAG knowledge base', () => {
  assert.equal(
    requiresKnowledgeBaseInitialization({ external_ref: 'cis:kb:4' }),
    true,
  )
})

test('keeps the summary model requirement for standalone knowledge bases', () => {
  assert.equal(
    requiresKnowledgeBaseInitialization({ embedding_model_id: 'embedding-model' }),
    true,
  )
  assert.equal(
    requiresKnowledgeBaseInitialization({
      embedding_model_id: 'embedding-model',
      summary_model_id: 'summary-model',
    }),
    false,
  )
})
