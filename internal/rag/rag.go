// Package rag implements Agentic RAG (Retrieval-Augmented Generation).
//
// Provides document ingestion, vector storage, retrieval, and agentic
// retrieval patterns where the agent decides when and how to search.
package rag

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Document represents an ingested document.
type Document struct {
	ID        string          `json:"id"`
	Content   string          `json:"content"`
	Source    string          `json:"source"`    // File path, URL, etc.
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	Chunks    []Chunk         `json:"chunks,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Chunk is a chunk of a document for embedding and retrieval.
type Chunk struct {
	ID         string    `json:"id"`
	DocID      string    `json:"doc_id"`
	Content    string    `json:"content"`
	Position   int       `json:"position"`    // Position within the document
	Embedding  []float64 `json:"-"`           // TF-IDF vector
	Source     string    `json:"source"`
	TokenCount int       `json:"token_count"`
}

// RetrievalResult is the result of a retrieval query.
type RetrievalResult struct {
	Query    string      `json:"query"`
	Chunks   []Chunk     `json:"chunks"`
	Scores   []float64   `json:"scores"`
	TotalDocs int        `json:"total_docs"`
	TimeMs   int64       `json:"time_ms"`
}

// Config configures the RAG engine.
type Config struct {
	ChunkSize    int     `json:"chunk_size"`     // Characters per chunk
	ChunkOverlap int     `json:"chunk_overlap"`  // Overlap between chunks
	TopK         int     `json:"top_k"`          // Number of results to return
	MinScore     float64 `json:"min_score"`      // Minimum similarity score
}

// DefaultRAGConfig returns default RAG configuration.
func DefaultRAGConfig() Config {
	return Config{
		ChunkSize:    500,
		ChunkOverlap: 50,
		TopK:         5,
		MinScore:     0.3,
	}
}

// Engine is the RAG engine.
type Engine struct {
	config   Config
	documents map[string]*Document
	chunks   []Chunk
	mu       sync.RWMutex
	counter  int
}

// NewEngine creates a new RAG engine.
func NewEngine(cfg Config) *Engine {
	if cfg.ChunkSize <= 0 {
		cfg.ChunkSize = 500
	}
	if cfg.TopK <= 0 {
		cfg.TopK = 5
	}
	return &Engine{
		config:    cfg,
		documents: make(map[string]*Document),
	}
}

// Ingest adds a document to the RAG store.
func (e *Engine) Ingest(content, source string, metadata json.RawMessage) *Document {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.counter++
	docID := fmt.Sprintf("doc_%d", e.counter)

	// Split into chunks.
	textChunks := e.splitChunks(content)

	var chunks []Chunk
	for i, text := range textChunks {
		chunkID := fmt.Sprintf("chunk_%d_%d", e.counter, i)
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
		e.chunks = append(e.chunks, chunk)
	}

	doc := &Document{
		ID:        docID,
		Content:   content,
		Source:    source,
		Metadata:  metadata,
		Chunks:    chunks,
		CreatedAt: time.Now().UTC(),
	}
	e.documents[docID] = doc
	return doc
}

// Retrieve finds the most relevant chunks for a query.
func (e *Engine) Retrieve(query string) *RetrievalResult {
	start := time.Now()
	e.mu.RLock()
	defer e.mu.RUnlock()

	queryVec := textToVector(query)

	type scored struct {
		chunk Chunk
		score float64
	}

	var results []scored
	for _, chunk := range e.chunks {
		if len(chunk.Embedding) == 0 {
			continue
		}
		score := cosineSimilarity(queryVec, chunk.Embedding)
		if score >= e.config.MinScore {
			results = append(results, scored{chunk: chunk, score: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	k := e.config.TopK
	if k > len(results) {
		k = len(results)
	}

	var chunks []Chunk
	var scores []float64
	for i := 0; i < k; i++ {
		chunks = append(chunks, results[i].chunk)
		scores = append(scores, results[i].score)
	}

	return &RetrievalResult{
		Query:     query,
		Chunks:    chunks,
		Scores:    scores,
		TotalDocs: len(e.documents),
		TimeMs:    time.Since(start).Milliseconds(),
	}
}

// AgenticRetrieve decides whether retrieval is needed and performs it.
// The agent can choose to skip retrieval if the query doesn't need it.
func (e *Engine) AgenticRetrieve(query string) (*RetrievalResult, bool) {
	// Simple heuristic: retrieval is useful for factual queries.
	factualIndicators := []string{"what", "how", "when", "where", "who", "which", "tell me about", "explain", "describe"}
	needRetrieval := false
	lower := strings.ToLower(query)
	for _, indicator := range factualIndicators {
		if strings.Contains(lower, indicator) {
			needRetrieval = true
			break
		}
	}

	if !needRetrieval {
		return nil, false
	}

	result := e.Retrieve(query)
	return result, len(result.Chunks) > 0
}

// ListDocuments returns all ingested documents.
func (e *Engine) ListDocuments() []*Document {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var docs []*Document
	for _, doc := range e.documents {
		docs = append(docs, doc)
	}
	return docs
}

// DocumentCount returns the number of ingested documents.
func (e *Engine) DocumentCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.documents)
}

// ChunkCount returns the total number of chunks.
func (e *Engine) ChunkCount() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return len(e.chunks)
}

// splitChunks splits text into overlapping chunks.
func (e *Engine) splitChunks(text string) []string {
	if len(text) <= e.config.ChunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0
	for start < len(text) {
		end := start + e.config.ChunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
		start += e.config.ChunkSize - e.config.ChunkOverlap
	}
	return chunks
}

// --- TF-IDF Helpers ---

func textToVector(text string) []float64 {
	words := tokenizeRAG(text)
	if len(words) == 0 {
		return nil
	}
	tf := make(map[string]float64)
	for _, w := range words {
		tf[w]++
	}
	for w := range tf {
		tf[w] /= float64(len(words))
	}
	features := make([]string, 0, len(tf))
	for w := range tf {
		features = append(features, w)
	}
	sort.Strings(features)
	vec := make([]float64, len(features))
	for i, f := range features {
		vec[i] = tf[f]
	}
	norm := 0.0
	for _, v := range vec {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm > 0 {
		for i := range vec {
			vec[i] /= norm
		}
	}
	return vec
}

func tokenizeRAG(text string) []string {
	text = strings.ToLower(text)
	var words []string
	current := ""
	for _, ch := range text {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			current += string(ch)
		} else if current != "" {
			words = append(words, current)
			current = ""
		}
	}
	if current != "" {
		words = append(words, current)
	}
	return words
}

func cosineSimilarity(a, b []float64) float64 {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	pA := make([]float64, maxLen)
	pB := make([]float64, maxLen)
	copy(pA, a)
	copy(pB, b)
	var dot, nA, nB float64
	for i := range pA {
		dot += pA[i] * pB[i]
		nA += pA[i] * pA[i]
		nB += pB[i] * pB[i]
	}
	denom := math.Sqrt(nA) * math.Sqrt(nB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}
