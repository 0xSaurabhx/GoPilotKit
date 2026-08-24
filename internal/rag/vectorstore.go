// Package rag implements Agentic RAG (Retrieval-Augmented Generation).
//
// Provides document ingestion, vector storage, retrieval, and agentic
// retrieval patterns where the agent decides when and how to search.
//
// VectorStore backends:
//   - embedded: In-memory TF-IDF (default, no external dependencies)
//   - qdrant: Qdrant vector database (requires Qdrant server)
//   - pgvector: PostgreSQL pgvector extension (requires pgvector-enabled Postgres)
package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// --- VectorStore interface ---

// VectorStore is the interface that all vector storage backends must implement.
// It handles chunk storage and similarity search.
type VectorStore interface {
	// Insert stores a chunk with its embedding vector.
	Insert(ctx context.Context, chunk Chunk) error

	// Search finds the top-k most similar chunks to the query vector.
	Search(ctx context.Context, queryVec []float64, topK int, minScore float64) ([]ScoredChunk, error)

	// DeleteByDocID removes all chunks belonging to a document.
	DeleteByDocID(ctx context.Context, docID string) error

	// Count returns the total number of stored chunks.
	Count(ctx context.Context) (int, error)

	// Close releases resources held by the store.
	Close() error
}

// ScoredChunk is a chunk with its similarity score.
type ScoredChunk struct {
	Chunk Chunk
	Score float64
}

// --- Embedded store (in-memory, no external dependencies) ---

// EmbeddedStore is the default in-memory vector store using TF-IDF.
type EmbeddedStore struct {
	chunks []Chunk
	mu     sync.RWMutex
}

// NewEmbeddedStore creates a new in-memory vector store.
func NewEmbeddedStore() *EmbeddedStore {
	return &EmbeddedStore{}
}

// Insert adds a chunk to the in-memory store.
func (s *EmbeddedStore) Insert(ctx context.Context, chunk Chunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.chunks = append(s.chunks, chunk)
	return nil
}

// Search finds the top-k most similar chunks using cosine similarity.
func (s *EmbeddedStore) Search(ctx context.Context, queryVec []float64, topK int, minScore float64) ([]ScoredChunk, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type scoredItem struct {
		idx   int
		score float64
	}
	var results []scoredItem
	for i, chunk := range s.chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(queryVec, chunk.Embedding)
		if score >= minScore {
			results = append(results, scoredItem{idx: i, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	if topK > len(results) {
		topK = len(results)
	}

	var scored []ScoredChunk
	for i := 0; i < topK; i++ {
		scored = append(scored, ScoredChunk{
			Chunk: s.chunks[results[i].idx],
			Score: results[i].score,
		})
	}
	return scored, nil
}

// DeleteByDocID removes all chunks belonging to a document.
func (s *EmbeddedStore) DeleteByDocID(ctx context.Context, docID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var filtered []Chunk
	for _, chunk := range s.chunks {
		if chunk.DocID != docID {
			filtered = append(filtered, chunk)
		}
	}
	s.chunks = filtered
	return nil
}

// Count returns the total number of stored chunks.
func (s *EmbeddedStore) Count(ctx context.Context) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.chunks), nil
}

// Close is a no-op for the in-memory store.
func (s *EmbeddedStore) Close() error { return nil }

// --- Document store (manages documents alongside vector store) ---

// DocumentStore manages the document metadata alongside a VectorStore for embeddings.
type DocumentStore struct {
	vectorStore VectorStore
	documents   map[string]*Document
	mu          sync.RWMutex
	counter     int
}

// NewDocumentStore creates a new document store backed by the given VectorStore.
func NewDocumentStore(vs VectorStore) *DocumentStore {
	return &DocumentStore{
		vectorStore: vs,
		documents:   make(map[string]*Document),
	}
}

// Ingest adds a document, chunks it, and stores the chunks in the vector store.
func (ds *DocumentStore) Ingest(ctx context.Context, content, source string, metadata json.RawMessage, chunkSize, chunkOverlap int) (*Document, error) {
	ds.mu.Lock()
	defer ds.mu.Unlock()

	ds.counter++
	docID := fmt.Sprintf("doc_%d", ds.counter)

	textChunks := splitChunks(content, chunkSize, chunkOverlap)

	var chunks []Chunk
	for i, text := range textChunks {
		chunkID := fmt.Sprintf("chunk_%d_%d", ds.counter, i)
		chunk := Chunk{
			ID:         chunkID,
			DocID:      docID,
			Content:    text,
			Position:   i,
			Embedding:  textToVector(text),
			Source:     source,
			TokenCount: len(strings.Fields(text)),
		}
		chunks = append(chunks, chunk)

		if err := ds.vectorStore.Insert(ctx, chunk); err != nil {
			return nil, fmt.Errorf("inserting chunk %s: %w", chunkID, err)
		}
	}

	doc := &Document{
		ID:        docID,
		Content:   content,
		Source:    source,
		Metadata:  metadata,
		Chunks:    chunks,
		CreatedAt: time.Now().UTC(),
	}
	ds.documents[docID] = doc
	return doc, nil
}

// Retrieve finds the most relevant chunks for a query.
func (ds *DocumentStore) Retrieve(ctx context.Context, query string, topK int, minScore float64) (*RetrievalResult, error) {
	start := time.Now()

	queryVec := textToVector(query)
	scored, err := ds.vectorStore.Search(ctx, queryVec, topK, minScore)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	var chunks []Chunk
	var scores []float64
	for _, s := range scored {
		chunks = append(chunks, s.Chunk)
		scores = append(scores, s.Score)
	}

	ds.mu.RLock()
	totalDocs := len(ds.documents)
	ds.mu.RUnlock()


	return &RetrievalResult{
		Query:     query,
		Chunks:    chunks,
		Scores:    scores,
		TotalDocs: totalDocs,
		TimeMs:    time.Since(start).Milliseconds(),
	}, nil
}

// DocumentCount returns the number of ingested documents.
func (ds *DocumentStore) DocumentCount() int {
	ds.mu.RLock()
	defer ds.mu.RUnlock()
	return len(ds.documents)
}

// ChunkCount returns the total number of chunks stored.
func (ds *DocumentStore) ChunkCount(ctx context.Context) (int, error) {
	return ds.vectorStore.Count(ctx)
}

// Close releases resources held by the document store.
func (ds *DocumentStore) Close() error {
	return ds.vectorStore.Close()
}

// --- Utility functions ---

func splitChunks(text string, chunkSize, chunkOverlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 500
	}
	if chunkOverlap < 0 {
		chunkOverlap = 50
	}
	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
		start += chunkSize - chunkOverlap
	}
	return chunks
}
