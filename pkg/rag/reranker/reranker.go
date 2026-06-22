package reranker

import internalreranker "github.com/EngineerProjects/seshat/internal/rag/reranker"

type LangSearchReranker = internalreranker.LangSearchReranker

func NewLangSearchRerankerWithKey(apiKey string) *LangSearchReranker {
	return internalreranker.NewLangSearchRerankerWithKey(apiKey)
}
