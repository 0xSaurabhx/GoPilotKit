package memory

import (
	"testing"
	"time"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine(Config{})
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.config.MaxMemories != 1000 {
		t.Errorf("expected default MaxMemories 1000, got %d", e.config.MaxMemories)
	}
	if e.config.DecayRate != 0.01 {
		t.Errorf("expected default DecayRate 0.01, got %f", e.config.DecayRate)
	}
	if e.config.MinStrength != 0.1 {
		t.Errorf("expected default MinStrength 0.1, got %f", e.config.MinStrength)
	}
}

func TestNewEngineCustomConfig(t *testing.T) {
	cfg := Config{MaxMemories: 500, DecayRate: 0.05, MinStrength: 0.2, ConsolidateThreshold: 5}
	e := NewEngine(cfg)
	if e.config.MaxMemories != 500 {
		t.Errorf("expected MaxMemories 500, got %d", e.config.MaxMemories)
	}
	if e.config.DecayRate != 0.05 {
		t.Errorf("expected DecayRate 0.05, got %f", e.config.DecayRate)
	}
}

func TestDefaultMemoryConfig(t *testing.T) {
	cfg := DefaultMemoryConfig()
	if cfg.MaxMemories != 1000 {
		t.Errorf("expected 1000, got %d", cfg.MaxMemories)
	}
	if cfg.DecayRate != 0.01 {
		t.Errorf("expected 0.01, got %f", cfg.DecayRate)
	}
	if cfg.MinStrength != 0.1 {
		t.Errorf("expected 0.1, got %f", cfg.MinStrength)
	}
	if cfg.ConsolidateThreshold != 3 {
		t.Errorf("expected 3, got %d", cfg.ConsolidateThreshold)
	}
}

func TestStore(t *testing.T) {
	e := NewEngine(Config{MaxMemories: 100})
	mem := e.Store("agent_1", "Go is a programming language", "fact", "conversation", []string{"go", "programming"})

	if mem.ID == "" {
		t.Error("expected non-empty ID")
	}
	if mem.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", mem.Agent)
	}
	if mem.Content != "Go is a programming language" {
		t.Errorf("unexpected content")
	}
	if mem.Category != "fact" {
		t.Errorf("expected category 'fact', got %q", mem.Category)
	}
	if mem.Source != "conversation" {
		t.Errorf("expected source 'conversation', got %q", mem.Source)
	}
	if mem.Strength != 1.0 {
		t.Errorf("expected initial strength 1.0, got %f", mem.Strength)
	}
	if len(mem.Tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(mem.Tags))
	}
	if e.Count() != 1 {
		t.Errorf("expected count 1, got %d", e.Count())
	}
}

func TestStoreEviction(t *testing.T) {
	e := NewEngine(Config{MaxMemories: 3})

	e.Store("agent_1", "memory 1", "fact", "src", nil)
	e.Store("agent_1", "memory 2", "fact", "src", nil)
	e.Store("agent_1", "memory 3", "fact", "src", nil)

	// Store a 4th — should evict the weakest.
	e.Store("agent_1", "memory 4", "fact", "src", nil)

	memories := e.GetMemories("agent_1")
	if len(memories) != 3 {
		t.Fatalf("expected 3 memories after eviction, got %d", len(memories))
	}
}

func TestRecall(t *testing.T) {
	e := NewEngine(Config{MinStrength: 0.1})
	e.Store("agent_1", "Go is a compiled programming language", "fact", "src", nil)
	e.Store("agent_1", "Python is an interpreted language", "fact", "src", nil)
	e.Store("agent_1", "The weather is sunny today", "context", "src", nil)

	results := e.Recall("agent_1", "Go programming", 10)
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	// The Go-related memory should be ranked first.
	if results[0].Content != "Go is a compiled programming language" {
		t.Errorf("expected Go memory first, got %q", results[0].Content)
	}
}

func TestRecallReinforcesStrength(t *testing.T) {
	e := NewEngine(Config{MinStrength: 0.1})
	e.Store("agent_1", "Go is great", "fact", "src", nil)

	results := e.Recall("agent_1", "Go", 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Strength starts at 1.0; recall adds 0.05 but caps at 1.0.
	if results[0].Strength != 1.0 {
		t.Errorf("expected strength 1.0 (capped), got %f", results[0].Strength)
	}
	if results[0].AccessCount != 1 {
		t.Errorf("expected access_count 1, got %d", results[0].AccessCount)
	}
	if results[0].LastAccessed == nil {
		t.Error("expected last_accessed to be set")
	}
}

func TestRecallLimit(t *testing.T) {
	e := NewEngine(Config{MinStrength: 0.0})

	for i := 0; i < 10; i++ {
		e.Store("agent_1", "test memory about Go programming", "fact", "src", nil)
	}

	results := e.Recall("agent_1", "Go", 3)
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestRecallEmptyAgent(t *testing.T) {
	e := NewEngine(Config{})
	results := e.Recall("nonexistent", "query", 10)
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
}

func TestDecay(t *testing.T) {
	e := NewEngine(Config{DecayRate: 1.0, MinStrength: 0.1})

	// Store a memory and backdate it.
	mem := e.Store("agent_1", "old memory", "fact", "src", nil)
	past := time.Now().UTC().Add(-24 * time.Hour * 100) // 100 days ago
	mem.CreatedAt = past

	// With DecayRate=1.0 and 100 days, strength should be effectively 0.
	pruned := e.Decay()
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}
	if e.Count() != 0 {
		t.Errorf("expected 0 memories after decay, got %d", e.Count())
	}
}

func TestDecayKeepsRecent(t *testing.T) {
	e := NewEngine(Config{DecayRate: 0.001, MinStrength: 0.1})

	e.Store("agent_1", "recent memory", "fact", "src", nil)

	pruned := e.Decay()
	if pruned != 0 {
		t.Errorf("expected 0 pruned for recent memory, got %d", pruned)
	}
	if e.Count() != 1 {
		t.Errorf("expected 1 memory, got %d", e.Count())
	}
}

func TestConsolidate(t *testing.T) {
	cfg := Config{ConsolidateThreshold: 3}
	e := NewEngine(cfg)

	// Store enough memories in the same category to trigger consolidation.
	for i := 0; i < 5; i++ {
		e.Store("agent_1", "fact about Go", "fact", "src", nil)
	}

	consolidated := e.Consolidate("agent_1")
	// Should have consolidated at least once (5 facts >= threshold 3).
	if consolidated < 1 {
		t.Errorf("expected at least 1 consolidation, got %d", consolidated)
	}
}

func TestConsolidateEmptyAgent(t *testing.T) {
	e := NewEngine(Config{ConsolidateThreshold: 3})
	consolidated := e.Consolidate("nonexistent")
	if consolidated != 0 {
		t.Errorf("expected 0, got %d", consolidated)
	}
}

func TestGetMemories(t *testing.T) {
	e := NewEngine(Config{})
	e.Store("agent_1", "mem1", "fact", "src", nil)
	e.Store("agent_1", "mem2", "fact", "src", nil)
	e.Store("agent_2", "mem3", "fact", "src", nil)

	memories := e.GetMemories("agent_1")
	if len(memories) != 2 {
		t.Errorf("expected 2 memories, got %d", len(memories))
	}

	// Verify copy semantics.
	memories[0].Content = "modified"
	original := e.GetMemories("agent_1")
	if original[0].Content == "modified" {
		t.Error("GetMemories should return a copy")
	}
}

func TestCount(t *testing.T) {
	e := NewEngine(Config{})
	if e.Count() != 0 {
		t.Errorf("expected 0, got %d", e.Count())
	}
	e.Store("agent_1", "m1", "fact", "src", nil)
	e.Store("agent_2", "m2", "fact", "src", nil)
	if e.Count() != 2 {
		t.Errorf("expected 2, got %d", e.Count())
	}
}

func TestRelevanceScore(t *testing.T) {
	score := relevanceScore("Go programming", "Go is a programming language")
	if score <= 0 {
		t.Errorf("expected positive score, got %f", score)
	}

	score = relevanceScore("Python", "Go is a programming language")
	if score != 0 {
		t.Errorf("expected 0 for unrelated query, got %f", score)
	}

	score = relevanceScore("", "some content")
	if score != 0 {
		t.Errorf("expected 0 for empty query, got %f", score)
	}
}

func TestToWordSet(t *testing.T) {
	set := toWordSet("Hello, World! 123")
	if !set["hello"] {
		t.Error("expected 'hello' in set")
	}
	if !set["world"] {
		t.Error("expected 'world' in set")
	}
	if !set["123"] {
		t.Error("expected '123' in set")
	}
	if len(set) != 3 {
		t.Errorf("expected 3 words, got %d", len(set))
	}
}

func TestToLower(t *testing.T) {
	result := toLower("Hello WORLD 123")
	if result != "hello world 123" {
		t.Errorf("expected 'hello world 123', got %q", result)
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := NewEngine(Config{MaxMemories: 1000})

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 50; i++ {
			e.Store("agent_1", "memory content", "fact", "src", nil)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			e.Recall("agent_1", "memory", 10)
		}
		done <- true
	}()

	<-done
	<-done
}
