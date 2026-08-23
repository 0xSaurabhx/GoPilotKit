// Package cache implements semantic caching for LLM responses.
//
// Caches semantically similar queries to avoid redundant LLM calls,
// providing cost savings on repeated or similar queries.
package cache

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// CacheEntry is a single cached query-response pair.
type CacheEntry struct {
	ID            string          `json:"id"`
	Query         string          `json:"query"`
	Response      string          `json:"response"`
	Embedding     []float64       `json:"-"` // TF-IDF vector
	ProviderID    string          `json:"provider_id,omitempty"`
	Model         string          `json:"model,omitempty"`
	TokenUsage    *TokenUsage     `json:"token_usage,omitempty"`
	CreatedAt     time.Time       `json:"created_at"`
	HitCount      int             `json:"hit_count"`
	LastHitAt     *time.Time      `json:"last_hit_at,omitempty"`
	TTL           time.Duration   `json:"ttl,omitempty"` // 0 = no expiry
}

// TokenUsage tracks token consumption for the cached response.
type TokenUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Config configures the semantic cache.
type Config struct {
	MaxEntries      int           `json:"max_entries"`       // Max cache size (0 = unlimited)
	DefaultTTL      time.Duration `json:"default_ttl"`       // Default TTL for entries (0 = no expiry)
	SimilarityThresh float64      `json:"similarity_threshold"` // Min cosine similarity to match (0-1)
	EnableTFIDF     bool          `json:"enable_tfidf"`      // Use TF-IDF vectors for similarity
}

// DefaultCacheConfig returns a sensible default configuration.
func DefaultCacheConfig() Config {
	return Config{
		MaxEntries:       10000,
		DefaultTTL:       24 * time.Hour,
		SimilarityThresh: 0.85,
		EnableTFIDF:      true,
	}
}

// Cache is a semantic cache for LLM responses.
type Cache struct {
	entries  []CacheEntry
	config   Config
	mu       sync.RWMutex
	idCounter int
}

// NewCache creates a new semantic cache.
func NewCache(cfg Config) *Cache {
	if cfg.SimilarityThresh <= 0 {
		cfg.SimilarityThresh = 0.85
	}
	return &Cache{
		config: cfg,
	}
}

// Lookup finds the closest matching cached response for the given query.
// Returns the cached entry and true if found above the similarity threshold.
func (c *Cache) Lookup(query string) (*CacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.entries) == 0 {
		return nil, false
	}

	queryVec := c.textToVector(query)
	bestScore := 0.0
	bestIdx := -1

	now := time.Now()
	for i := range c.entries {
		entry := &c.entries[i]

		// Skip expired entries.
		if entry.TTL > 0 && now.Sub(entry.CreatedAt) > entry.TTL {
			continue
		}

		if len(entry.Embedding) == 0 {
			continue
		}

		score := cosineSimilarity(queryVec, entry.Embedding)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}

	if bestIdx < 0 || bestScore < c.config.SimilarityThresh {
		return nil, false
	}

	entry := &c.entries[bestIdx]
	entry.HitCount++
	now2 := time.Now()
	entry.LastHitAt = &now2

	return entry, true
}

// Store adds a query-response pair to the cache.
func (c *Cache) Store(query, response, providerID, model string, tokenUsage *TokenUsage) *CacheEntry {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Evict oldest if at capacity.
	if c.config.MaxEntries > 0 && len(c.entries) >= c.config.MaxEntries {
		c.evictOldest()
	}

	entry := CacheEntry{
		ID:         fmt.Sprintf("cache_%d", time.Now().UnixNano()),
		Query:      query,
		Response:   response,
		Embedding:  c.textToVector(query),
		ProviderID: providerID,
		Model:      model,
		TokenUsage: tokenUsage,
		CreatedAt:  time.Now(),
		TTL:        c.config.DefaultTTL,
	}

	c.entries = append(c.entries, entry)
	return &c.entries[len(c.entries)-1]
}

// Size returns the number of entries in the cache.
func (c *Cache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Clear removes all entries from the cache.
func (c *Cache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// Stats returns cache statistics.
func (c *Cache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := CacheStats{
		TotalEntries: len(c.entries),
	}

	var totalHits int
	for _, e := range c.entries {
		totalHits += e.HitCount
	}
	stats.TotalHits = totalHits

	if stats.TotalEntries > 0 {
		stats.AvgHitsPerEntry = float64(totalHits) / float64(stats.TotalEntries)
	}

	return stats
}

// CacheStats holds cache statistics.
type CacheStats struct {
	TotalEntries     int     `json:"total_entries"`
	TotalHits        int     `json:"total_hits"`
	AvgHitsPerEntry  float64 `json:"avg_hits_per_entry"`
}

// evictOldest removes the oldest entry. Caller must hold lock.
func (c *Cache) evictOldest() {
	if len(c.entries) == 0 {
		return
	}
	// Remove the entry with the oldest CreatedAt.
	oldest := 0
	for i := 1; i < len(c.entries); i++ {
		if c.entries[i].CreatedAt.Before(c.entries[oldest].CreatedAt) {
			oldest = i
		}
	}
	c.entries = append(c.entries[:oldest], c.entries[oldest+1:]...)
}

// --- TF-IDF Vector Space Model ---

// textToVector converts text to a TF-IDF-like vector.
// Uses word frequency as a simple embedding (no external model needed).
func (c *Cache) textToVector(text string) []float64 {
	words := tokenize(text)
	if len(words) == 0 {
		return nil
	}

	// Term frequency
	tf := make(map[string]float64)
	for _, w := range words {
		tf[w]++
	}
	// Normalize by document length.
	for w := range tf {
		tf[w] /= float64(len(words))
	}

	// Build a sorted feature vector.
	features := make([]string, 0, len(tf))
	for w := range tf {
		features = append(features, w)
	}
	sort.Strings(features)

	vec := make([]float64, len(features))
	for i, f := range features {
		vec[i] = tf[f]
	}

	// L2 normalize.
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

// cosineSimilarity computes cosine similarity between two vectors.
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		// Pad the shorter vector with zeros.
		maxLen := len(a)
		if len(b) > maxLen {
			maxLen = len(b)
		}
		if maxLen == 0 {
			return 0
		}
		// Use the longer vector's dimension.
		paddedA := make([]float64, maxLen)
		paddedB := make([]float64, maxLen)
		copy(paddedA, a)
		copy(paddedB, b)
		a, b = paddedA, paddedB
	}

	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}

	denom := math.Sqrt(normA) * math.Sqrt(normB)
	if denom == 0 {
		return 0
	}
	return dot / denom
}

// tokenize splits text into lowercase words.
func tokenize(text string) []string {
	text = strings.ToLower(text)
	// Simple split on non-alphanumeric characters.
	var words []string
	current := strings.Builder{}
	for _, ch := range text {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			current.WriteRune(ch)
		} else {
			if current.Len() > 0 {
				words = append(words, current.String())
				current.Reset()
			}
		}
	}
	if current.Len() > 0 {
		words = append(words, current.String())
	}
	return words
}


