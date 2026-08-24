package rag

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PgvectorConfig configures the pgvector store.
type PgvectorConfig struct {
	ConnectionString string `json:"connection_string"` // e.g. "postgres://user:pass@localhost:5432/db"
	Collection       string `json:"collection"`        // Table name, e.g. "gopilotkit_chunks"
	Dimension        int    `json:"dimension"`         // Vector dimension
}

// PgvectorStore implements VectorStore using PostgreSQL with the pgvector extension.
type PgvectorStore struct {
	config     PgvectorConfig
	pool       *pgxpool.Pool
	collection string
}

// NewPgvectorStore creates a new pgvector store and ensures the schema exists.
func NewPgvectorStore(ctx context.Context, cfg PgvectorConfig) (*PgvectorStore, error) {
	if cfg.Collection == "" {
		cfg.Collection = "rag_chunks"
	}
	if cfg.Dimension <= 0 {
		cfg.Dimension = 1536
	}

	pool, err := pgxpool.New(ctx, cfg.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("pgvector: connecting to database: %w", err)
	}

	// Verify connectivity.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgvector: pinging database: %w", err)
	}

	s := &PgvectorStore{
		config:     cfg,
		pool:       pool,
		collection: cfg.Collection,
	}

	// Ensure the schema.
	if err := s.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgvector: ensuring schema: %w", err)
	}

	return s, nil
}

// ensureSchema creates the pgvector extension and the chunks table.
func (s *PgvectorStore) ensureSchema(ctx context.Context) error {
	// Enable pgvector extension.
	if _, err := s.pool.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("creating vector extension: %w", err)
	}

	// Create the chunks table.
	createTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s (
			id          TEXT PRIMARY KEY,
			doc_id      TEXT NOT NULL,
			content     TEXT NOT NULL,
			position    INTEGER NOT NULL,
			source      TEXT DEFAULT '',
			token_count INTEGER DEFAULT 0,
			embedding   vector(%d),
			created_at  TIMESTAMPTZ DEFAULT NOW()
		)
	`, s.collection, s.config.Dimension)

	if _, err := s.pool.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("creating table: %w", err)
	}

	// Create an HNSW index for fast approximate nearest neighbor search.
	indexName := fmt.Sprintf("%s_embedding_idx", s.collection)
	createIndex := fmt.Sprintf(`
		CREATE INDEX IF NOT EXISTS %s ON %s 
		USING hnsw (embedding vector_cosine_ops)
	`, indexName, s.collection)

	if _, err := s.pool.Exec(ctx, createIndex); err != nil {
		return fmt.Errorf("creating index: %w", err)
	}

	return nil
}

// Insert stores a chunk with its embedding vector in PostgreSQL.
func (s *PgvectorStore) Insert(ctx context.Context, chunk Chunk) error {
	embeddingStr := vectorToPgString(chunk.Embedding)

	_, err := s.pool.Exec(ctx, fmt.Sprintf(`
		INSERT INTO %s (id, doc_id, content, position, source, token_count, embedding)
		VALUES ($1, $2, $3, $4, $5, $6, $7::vector)
		ON CONFLICT (id) DO UPDATE SET
			content = EXCLUDED.content,
			embedding = EXCLUDED.embedding
	`, s.collection), chunk.ID, chunk.DocID, chunk.Content, chunk.Position, chunk.Source, chunk.TokenCount, embeddingStr)

	if err != nil {
		return fmt.Errorf("pgvector: inserting chunk %s: %w", chunk.ID, err)
	}

	return nil
}

// Search finds the top-k most similar chunks using cosine distance.
func (s *PgvectorStore) Search(ctx context.Context, queryVec []float64, topK int, minScore float64) ([]ScoredChunk, error) {
	queryStr := vectorToPgString(queryVec)

	// Use cosine distance operator (<=>) and convert to similarity (1 - distance).
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT id, doc_id, content, position, source, token_count,
		       1 - (embedding <=> $1::vector) AS similarity
		FROM %s
		WHERE 1 - (embedding <=> $1::vector) >= $2
		ORDER BY embedding <=> $1::vector
		LIMIT $3
	`, s.collection), queryStr, minScore, topK)
	if err != nil {
		return nil, fmt.Errorf("pgvector: searching: %w", err)
	}
	defer rows.Close()

	var results []ScoredChunk
	for rows.Next() {
		var sc ScoredChunk
		if err := rows.Scan(
			&sc.Chunk.ID,
			&sc.Chunk.DocID,
			&sc.Chunk.Content,
			&sc.Chunk.Position,
			&sc.Chunk.Source,
			&sc.Chunk.TokenCount,
			&sc.Score,
		); err != nil {
			return nil, fmt.Errorf("pgvector: scanning row: %w", err)
		}
		results = append(results, sc)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("pgvector: iterating rows: %w", err)
	}

	// Sort by score descending (in case the query planner didn't).
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// DeleteByDocID removes all chunks belonging to a document.
func (s *PgvectorStore) DeleteByDocID(ctx context.Context, docID string) error {
	tag, err := s.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE doc_id = $1`, s.collection), docID)
	if err != nil {
		return fmt.Errorf("pgvector: deleting by doc_id %s: %w", docID, err)
	}
	_ = tag // Rows affected not critical.
	return nil
}

// Count returns the total number of stored chunks.
func (s *PgvectorStore) Count(ctx context.Context) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s`, s.collection)).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("pgvector: counting chunks: %w", err)
	}
	return count, nil
}

// Close releases the connection pool.
func (s *PgvectorStore) Close() error {
	s.pool.Close()
	return nil
}

// --- Helpers ---

// vectorToPgString converts a float64 slice to the pgvector string format: "[0.1,0.2,0.3]"
func vectorToPgString(vec []float64) string {
	if len(vec) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteString("[")
	for i, v := range vec {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%g", v)
	}
	sb.WriteString("]")
	return sb.String()
}

// Unused but available for testing.
func pgStringToVector(s string) []float64 {
	s = strings.Trim(s, "[]")
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	vec := make([]float64, len(parts))
	for i, p := range parts {
		fmt.Sscanf(strings.TrimSpace(p), "%f", &vec[i])
	}
	return vec
}

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarityV2(a, b []float64) float64 {
	maxLen := len(a)
	if len(b) > maxLen {
		maxLen = len(b)
	}
	if maxLen == 0 {
		return 0
	}
	var dot, nA, nB float64
	for i := 0; i < maxLen; i++ {
		var va, vb float64
		if i < len(a) {
			va = a[i]
		}
		if i < len(b) {
			vb = b[i]
		}
		dot += va * vb
		nA += va * va
		nB += vb * vb
	}
	denom := math.Sqrt(nA) * math.Sqrt(nB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// Ensure PgvectorStore implements VectorStore.
var _ VectorStore = (*PgvectorStore)(nil)

// Ensure QdrantStore implements VectorStore.
var _ VectorStore = (*QdrantStore)(nil)

// Ensure EmbeddedStore implements VectorStore.
var _ VectorStore = (*EmbeddedStore)(nil)
