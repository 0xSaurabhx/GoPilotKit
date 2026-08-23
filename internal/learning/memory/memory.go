// Package memory implements memory optimization.
//
// Prunes and consolidates semantic memory to keep the agent's knowledge
// base efficient and relevant. Removes stale, redundant, or low-value memories.
package memory

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Memory represents a single memory entry.
type Memory struct {
	ID        string          `json:"id"`
	Agent     string          `json:"agent"`
	ThreadID  string          `json:"thread_id,omitempty"`
	Content   string          `json:"content"`
	Category  string          `json:"category"`  // "fact", "preference", "context", "skill"
	Source    string          `json:"source"`    // Where this memory was learned
	Strength  float64         `json:"strength"`  // 0.0 to 1.0, decays over time
	AccessCount int           `json:"access_count"`
	CreatedAt time.Time       `json:"created_at"`
	LastAccessed *time.Time    `json:"last_accessed,omitempty"`
	ExpiresAt  *time.Time     `json:"expires_at,omitempty"`
	Tags       []string       `json:"tags,omitempty"`
	Metadata   json.RawMessage `json:"metadata,omitempty"`
}

// Config configures the memory engine.
type Config struct {
	MaxMemories      int           `json:"max_memories"`
	DecayRate        float64       `json:"decay_rate"`        // Per-day strength decay
	MinStrength      float64       `json:"min_strength"`      // Below this, memory is pruned
	ConsolidateThreshold int       `json:"consolidate_threshold"` // Similar memories to consolidate
}

// DefaultMemoryConfig returns default memory configuration.
func DefaultMemoryConfig() Config {
	return Config{
		MaxMemories:          1000,
		DecayRate:            0.01,
		MinStrength:          0.1,
		ConsolidateThreshold: 3,
	}
}

// Engine manages semantic memory for agents.
type Engine struct {
	memories map[string][]Memory // agent → memories
	config   Config
	mu       sync.RWMutex
	counter  int
}

// NewEngine creates a new memory engine.
func NewEngine(cfg Config) *Engine {
	if cfg.MaxMemories <= 0 {
		cfg.MaxMemories = 1000
	}
	if cfg.DecayRate <= 0 {
		cfg.DecayRate = 0.01
	}
	if cfg.MinStrength <= 0 {
		cfg.MinStrength = 0.1
	}
	return &Engine{
		memories: make(map[string][]Memory),
		config:   cfg,
	}
}

// Store adds a new memory.
func (e *Engine) Store(agent, content, category, source string, tags []string) *Memory {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.counter++
	mem := Memory{
		ID:         fmt.Sprintf("mem_%d", e.counter),
		Agent:      agent,
		Content:    content,
		Category:   category,
		Source:     source,
		Strength:   1.0, // Start at full strength
		CreatedAt:  time.Now().UTC(),
		Tags:       tags,
	}

	// Evict weakest if at capacity.
	existing := e.memories[agent]
	if len(existing) >= e.config.MaxMemories {
		e.evictWeakest(agent)
	}

	e.memories[agent] = append(e.memories[agent], mem)
	return &e.memories[agent][len(e.memories[agent])-1]
}

// Recall retrieves memories relevant to a query.
func (e *Engine) Recall(agent, query string, limit int) []Memory {
	e.mu.RLock()
	defer e.mu.RUnlock()

	memories, ok := e.memories[agent]
	if !ok {
		return nil
	}

	type scored struct {
		mem Memory
		s   float64
	}

	var results []scored
	lower := query
	for _, mem := range memories {
		if mem.Strength < e.config.MinStrength {
			continue
		}
		// Simple relevance: check if query words appear in memory content.
		score := relevanceScore(lower, mem.Content) * mem.Strength
		if score > 0 {
			results = append(results, scored{mem: mem, s: score})
		}
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].s > results[j].s
	})

	if limit <= 0 || limit > len(results) {
		limit = len(results)
	}

	now := time.Now().UTC()
	var out []Memory
	for i := 0; i < limit; i++ {
		mem := results[i].mem
		mem.AccessCount++
		mem.LastAccessed = &now
		mem.Strength = min(1.0, mem.Strength+0.05) // Reinforce on access
		out = append(out, mem)
	}
	return out
}

// Decay applies time-based decay to all memories.
func (e *Engine) Decay() int {
	e.mu.Lock()
	defer e.mu.Unlock()

	pruned := 0
	for agent := range e.memories {
		memories := e.memories[agent]
		filtered := memories[:0]
		for _, mem := range memories {
			// Calculate days since creation.
			days := time.Since(mem.CreatedAt).Hours() / 24
			mem.Strength *= (1 - e.config.DecayRate*days)
			if mem.Strength < e.config.MinStrength {
				pruned++
				continue
			}
			filtered = append(filtered, mem)
		}
		e.memories[agent] = filtered
	}
	return pruned
}

// Consolidate merges similar memories into a single stronger memory.
func (e *Engine) Consolidate(agent string) int {
	e.mu.Lock()
	defer e.mu.Unlock()

	memories := e.memories[agent]
	if len(memories) == 0 {
		return 0
	}

	// Group by category and find duplicates.
	type cluster struct {
		indices []int
	}
	clusters := make(map[string]*cluster)
	for i, mem := range memories {
		if clusters[mem.Category] == nil {
			clusters[mem.Category] = &cluster{}
		}
		clusters[mem.Category].indices = append(clusters[mem.Category].indices, i)
	}

	consolidated := 0
	for _, cl := range clusters {
		if len(cl.indices) < e.config.ConsolidateThreshold {
			continue
		}
		// Merge the first N similar memories.
		// In a real implementation, this would use semantic similarity.
		// For now, just keep the strongest.
		best := cl.indices[0]
		for _, idx := range cl.indices[1:] {
			if memories[idx].Strength > memories[best].Strength {
				best = idx
			}
		}
		memory := memories[best]
		memory.Strength = min(1.0, memory.Strength+0.2)
		memories[best] = memory
		consolidated++
	}

	return consolidated
}

// GetMemories returns all memories for an agent.
func (e *Engine) GetMemories(agent string) []Memory {
	e.mu.RLock()
	defer e.mu.RUnlock()
	memories := e.memories[agent]
	result := make([]Memory, len(memories))
	copy(result, memories)
	return result
}

// Count returns total memories across all agents.
func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, memories := range e.memories {
		total += len(memories)
	}
	return total
}

// evictWeakest removes the weakest memory. Caller must hold lock.
func (e *Engine) evictWeakest(agent string) {
	memories := e.memories[agent]
	if len(memories) == 0 {
		return
	}
	weakest := 0
	for i, m := range memories {
		if m.Strength < memories[weakest].Strength {
			weakest = i
		}
	}
	e.memories[agent] = append(memories[:weakest], memories[weakest+1:]...)
}

// relevanceScore calculates simple word overlap between query and content.
func relevanceScore(query, content string) float64 {
	qWords := toWordSet(query)
	cWords := toWordSet(content)
	if len(qWords) == 0 {
		return 0
	}
	overlap := 0
	for w := range qWords {
		if cWords[w] {
			overlap++
		}
	}
	return float64(overlap) / float64(len(qWords))
}

func toWordSet(s string) map[string]bool {
	set := make(map[string]bool)
	s = toLower(s)
	current := ""
	for _, ch := range s {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') {
			current += string(ch)
		} else if current != "" {
			set[current] = true
			current = ""
		}
	}
	if current != "" {
		set[current] = true
	}
	return set
}

func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
