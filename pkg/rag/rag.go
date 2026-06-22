package rag

import (
	internalrag "github.com/EngineerProjects/seshat/internal/rag"
	publicstorage "github.com/EngineerProjects/seshat/pkg/storage"
	publicvector "github.com/EngineerProjects/seshat/pkg/vector"
)

type (
	Chunk          = internalrag.Chunk
	Chunker        = internalrag.Chunker
	Embedder       = internalrag.Embedder
	IngestRequest  = internalrag.IngestRequest
	IngestResult   = internalrag.IngestResult
	Reranker       = internalrag.Reranker
	SearchRequest  = internalrag.SearchRequest
	SearchResponse = internalrag.SearchResponse
	SearchResult   = internalrag.SearchResult
	Service        = internalrag.Service
	VectorStore    = publicvector.Store
)

func NewService(artifacts publicstorage.ArtifactStore, vectors publicvector.Store, embedder Embedder, chunker Chunker) *Service {
	return internalrag.NewService(artifacts, vectors, embedder, chunker)
}
