export interface KnowledgeBaseInitializationState {
  external_ref?: string
  embedding_model_id?: string
  summary_model_id?: string
  indexing_strategy?: {
    vector_enabled?: boolean
    keyword_enabled?: boolean
  }
}

const isCisManagedKnowledgeBase = (kb: KnowledgeBaseInitializationState) =>
  kb.external_ref?.trim().startsWith('cis:kb:') === true

/**
 * CIS-managed knowledge bases use WeKnora for indexing and retrieval while CIS
 * owns answer generation, so they do not require a WeKnora summary model just
 * to open and inspect the document list.
 */
export const requiresKnowledgeBaseInitialization = (kb: KnowledgeBaseInitializationState) => {
  const strategy = kb.indexing_strategy
  const needsEmbedding = !strategy || strategy.vector_enabled || strategy.keyword_enabled
  if (needsEmbedding && !kb.embedding_model_id?.trim()) return true
  return !isCisManagedKnowledgeBase(kb) && !kb.summary_model_id?.trim()
}
