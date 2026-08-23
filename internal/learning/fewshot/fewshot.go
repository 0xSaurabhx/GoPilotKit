// Package fewshot implements dynamic few-shot example curation.
//
// Dynamically selects the best few-shot examples for each query type based on
// semantic similarity, improving response quality without fine-tuning.
package fewshot

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"
)

// Example represents a few-shot example (query-response pair).
type Example struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Query     string    `json:"query"`
	Response  string    `json:"response"`
	Embedding []float64 `json:"-"` // TF-IDF vector
	Score     float64   `json:"score"`   // Quality score
	HitCount  int       `json:"hit_count"`
	CreatedAt time.Time `json:"created_at"`
}

// Config configures the few-shot engine.
type Config struct {
	MaxExamples  int     `json:"max_examples"`   // Max examples per agent
	RetrievalK   int     `json:"retrieval_k"`    // Number of examples to retrieve
	MinScore     float64 `json:"min_score"`      // Min quality score to keep
}

// Engine manages few-shot examples.
type Engine struct {
	examples map[string][]Example // agent → examples
	config   Config
	mu       sync.RWMutex
	counter  int
}

// NewEngine creates a new few-shot engine.
func NewEngine(cfg Config) *Engine {
	if cfg.MaxExamples <= 0 {
		cfg.MaxExamples = 100
	}
	if cfg.RetrievalK <= 0 {
		cfg.RetrievalK = 3
	}
	if cfg.MinScore <= 0 {
		cfg.MinScore = 0.3
	}
	return &Engine{
		examples: make(map[string][]Example),
		config:   cfg,
	}
}

// AddExample adds a new few-shot example.
func (e *Engine) AddExample(agent, query, response string, score float64) *Example {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.counter++
	ex := Example{
		ID:        fmt.Sprintf("ex_%d", e.counter),
		Agent:     agent,
		Query:     query,
		Response:  response,
		Embedding: textToVec(query),
		Score:     score,
		CreatedAt: time.Now().UTC(),
	}

	existing := e.examples[agent]

	// Evict lowest-scoring if at capacity.
	if len(existing) >= e.config.MaxExamples {
		sort.Slice(existing, func(i, j int) bool {
			return existing[i].Score > existing[j].Score
		})
		existing = existing[:len(existing)-1]
	}

	e.examples[agent] = append(existing, ex)
	return &e.examples[agent][len(e.examples[agent])-1]
}

// Retrieve finds the K most similar examples for a query.
func (e *Engine) Retrieve(agent, query string) []Example {
	e.mu.RLock()
	defer e.mu.RUnlock()

	examples, ok := e.examples[agent]
	if !ok || len(examples) == 0 {
		return nil
	}

	queryVec := textToVec(query)

	type scored struct {
		ex Example
		s  float64
	}

	var results []scored
	for _, ex := range examples {
		if len(ex.Embedding) == 0 {
			continue
		}
		sim := cosineSim(queryVec, ex.Embedding)
		if sim >= e.config.MinScore {
			results = append(results, scored{ex: ex, s: sim})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].s > results[j].s
	})

	k := e.config.RetrievalK
	if k > len(results) {
		k = len(results)
	}

	var out []Example
	for i := 0; i < k; i++ {
		out = append(out, results[i].ex)
	}
	return out
}

// GetExamples returns all examples for an agent.
func (e *Engine) GetExamples(agent string) []Example {
	e.mu.RLock()
	defer e.mu.RUnlock()
	examples := e.examples[agent]
	result := make([]Example, len(examples))
	copy(result, examples)
	return result
}

// Count returns the total number of examples across all agents.
func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, examples := range e.examples {
		total += len(examples)
	}
	return total
}

// --- TF-IDF Helpers ---

func textToVec(text string) []float64 {
	words := tokenize(text)
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

func tokenize(text string) []string {
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

func cosineSim(a, b []float64) float64 {
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

// MarshalJSON implements json.Marshaler for Example.
func (ex Example) MarshalJSON() ([]byte, error) {
	type Alias Example
	return json.Marshal(struct {
		Alias
		ScorePercent string `json:"score_percent"`
	}{
		Alias:        Alias(ex),
		ScorePercent: fmt.Sprintf("%.1f%%", ex.Score*100),
	})
}


