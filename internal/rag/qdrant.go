package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// QdrantConfig configures the Qdrant vector store.
type QdrantConfig struct {
	BaseURL    string `json:"base_url"`     // e.g. "http://localhost:6333"
	Collection string `json:"collection"`   // e.g. "gopilotkit"
	Dimension  int    `json:"dimension"`    // Vector dimension (e.g. 1536)
	APIKey     string `json:"api_key"`      // Optional API key
}

// QdrantStore implements VectorStore using Qdrant.
type QdrantStore struct {
	config   QdrantConfig
	client   *http.Client
	counter  int
}

// NewQdrantStore creates a new Qdrant vector store client.
func NewQdrantStore(cfg QdrantConfig) *QdrantStore {
	return &QdrantStore{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// qdrantPoint represents a point in Qdrant.
type qdrantPoint struct {
	ID      string            `json:"id"`
	Vector  []float64         `json:"vector"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// qdrantSearchRequest is the search request payload.
type qdrantSearchRequest struct {
	Vector      []float64 `json:"vector"`
	TopK        int       `json:"top"`
	ScoreThreshold float64 `json:"score_threshold,omitempty"`
	WithPayload bool      `json:"with_payload"`
}

// qdrantSearchResponse is the search response.
type qdrantSearchResponse struct {
	Result []qdrantSearchResult `json:"result"`
}

type qdrantSearchResult struct {
	ID      string             `json:"id"`
	Score   float64            `json:"score"`
	Payload map[string]interface{} `json:"payload"`
}

// Insert stores a chunk with its embedding vector in Qdrant.
func (s *QdrantStore) Insert(ctx context.Context, chunk Chunk) error {
	s.counter++
	pointID := chunk.ID
	if pointID == "" {
		pointID = fmt.Sprintf("point_%d", s.counter)
	}

	point := qdrantPoint{
		ID:     pointID,
		Vector: chunk.Embedding,
		Payload: map[string]interface{}{
			"doc_id":      chunk.DocID,
			"content":     chunk.Content,
			"position":    chunk.Position,
			"source":      chunk.Source,
			"token_count": chunk.TokenCount,
		},
	}

	body, err := json.Marshal(map[string]interface{}{
		"points": []qdrantPoint{point},
	})
	if err != nil {
		return fmt.Errorf("marshaling point: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points", s.config.BaseURL, s.config.Collection)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("api-key", s.config.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Qdrant PUT %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Qdrant PUT %s returned %d: %s", url, resp.StatusCode, respBody)
	}

	return nil
}

// Search finds the top-k most similar chunks using Qdrant's vector search.
func (s *QdrantStore) Search(ctx context.Context, queryVec []float64, topK int, minScore float64) ([]ScoredChunk, error) {
	searchReq := qdrantSearchRequest{
		Vector:      queryVec,
		TopK:        topK,
		ScoreThreshold: minScore,
		WithPayload: true,
	}

	body, err := json.Marshal(searchReq)
	if err != nil {
		return nil, fmt.Errorf("marshaling search request: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points/search", s.config.BaseURL, s.config.Collection)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("api-key", s.config.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Qdrant POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("Qdrant POST %s returned %d: %s", url, resp.StatusCode, respBody)
	}

	var searchResp qdrantSearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("decoding search response: %w", err)
	}

	var results []ScoredChunk
	for _, r := range searchResp.Result {
		chunk := Chunk{
			ID:         r.ID,
			DocID:      getString(r.Payload, "doc_id"),
			Content:    getString(r.Payload, "content"),
			Source:     getString(r.Payload, "source"),
			Position:   getInt(r.Payload, "position"),
			TokenCount: getInt(r.Payload, "token_count"),
		}
		results = append(results, ScoredChunk{
			Chunk: chunk,
			Score: r.Score,
		})
	}

	return results, nil
}

// DeleteByDocID removes all chunks belonging to a document from Qdrant.
func (s *QdrantStore) DeleteByDocID(ctx context.Context, docID string) error {
	// Qdrant delete by filter.
	body, err := json.Marshal(map[string]interface{}{
		"filter": map[string]interface{}{
			"must": []map[string]interface{}{
				{
					"key": "doc_id",
					"match": map[string]interface{}{
						"value": docID,
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("marshaling delete request: %w", err)
	}

	url := fmt.Sprintf("%s/collections/%s/points/delete", s.config.BaseURL, s.config.Collection)
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.config.APIKey != "" {
		req.Header.Set("api-key", s.config.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("Qdrant POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Qdrant POST %s returned %d: %s", url, resp.StatusCode, respBody)
	}

	return nil
}

// Count returns the total number of stored points in Qdrant.
func (s *QdrantStore) Count(ctx context.Context) (int, error) {
	url := fmt.Sprintf("%s/collections/%s", s.config.BaseURL, s.config.Collection)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("creating request: %w", err)
	}
	if s.config.APIKey != "" {
		req.Header.Set("api-key", s.config.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("Qdrant GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Qdrant GET %s returned %d", url, resp.StatusCode)
	}

	var collectionResp struct {
		Result struct {
			PointsCount int `json:"points_count"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&collectionResp); err != nil {
		return 0, fmt.Errorf("decoding collection info: %w", err)
	}

	return collectionResp.Result.PointsCount, nil
}

// Close is a no-op for Qdrant (stateless HTTP client).
func (s *QdrantStore) Close() error { return nil }

// --- Helper functions ---

func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getInt(m map[string]interface{}, key string) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		}
	}
	return 0
}
