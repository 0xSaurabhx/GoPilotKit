package fewshot

import (
	"encoding/json"
	"math"
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine(Config{})
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.config.MaxExamples != 100 {
		t.Errorf("expected default MaxExamples 100, got %d", e.config.MaxExamples)
	}
	if e.config.RetrievalK != 3 {
		t.Errorf("expected default RetrievalK 3, got %d", e.config.RetrievalK)
	}
	if e.config.MinScore != 0.3 {
		t.Errorf("expected default MinScore 0.3, got %f", e.config.MinScore)
	}
}

func TestNewEngineCustomConfig(t *testing.T) {
	cfg := Config{MaxExamples: 50, RetrievalK: 5, MinScore: 0.5}
	e := NewEngine(cfg)
	if e.config.MaxExamples != 50 {
		t.Errorf("expected MaxExamples 50, got %d", e.config.MaxExamples)
	}
	if e.config.RetrievalK != 5 {
		t.Errorf("expected RetrievalK 5, got %d", e.config.RetrievalK)
	}
	if e.config.MinScore != 0.5 {
		t.Errorf("expected MinScore 0.5, got %f", e.config.MinScore)
	}
}

func TestAddExample(t *testing.T) {
	e := NewEngine(Config{MaxExamples: 10})
	ex := e.AddExample("agent_1", "what is Go?", "Go is a programming language.", 0.9)

	if ex.ID == "" {
		t.Error("expected non-empty ID")
	}
	if ex.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", ex.Agent)
	}
	if ex.Query != "what is Go?" {
		t.Errorf("expected query 'what is Go?', got %q", ex.Query)
	}
	if ex.Response != "Go is a programming language." {
		t.Errorf("unexpected response")
	}
	if ex.Score != 0.9 {
		t.Errorf("expected score 0.9, got %f", ex.Score)
	}
	if len(ex.Embedding) == 0 {
		t.Error("expected non-empty embedding")
	}
	if e.Count() != 1 {
		t.Errorf("expected count 1, got %d", e.Count())
	}
}

func TestAddExampleEviction(t *testing.T) {
	e := NewEngine(Config{MaxExamples: 3})

	e.AddExample("agent_1", "query 1", "response 1", 0.5)
	e.AddExample("agent_1", "query 2", "response 2", 0.8)
	e.AddExample("agent_1", "query 3", "response 3", 0.3)

	// Adding a 4th should evict the lowest-scoring (query 3, score 0.3).
	e.AddExample("agent_1", "query 4", "response 4", 0.9)

	examples := e.GetExamples("agent_1")
	if len(examples) != 3 {
		t.Fatalf("expected 3 examples after eviction, got %d", len(examples))
	}

	// The evicted example should be the one with score 0.3.
	for _, ex := range examples {
		if ex.Score == 0.3 {
			t.Error("expected score 0.3 to be evicted")
		}
	}
}

func TestRetrieve(t *testing.T) {
	e := NewEngine(Config{RetrievalK: 3, MinScore: 0.1})

	e.AddExample("agent_1", "how to sort a slice in Go", "Use sort.Slice()", 0.9)
	e.AddExample("agent_1", "how to read a file in Go", "Use os.ReadFile()", 0.8)
	e.AddExample("agent_1", "what is the capital of France", "Paris", 0.7)

	// Query similar to the Go questions should retrieve them.
	results := e.Retrieve("agent_1", "sorting in Go")
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}

	// First result should be the most similar.
	found := false
	for _, r := range results {
		if r.Query == "how to sort a slice in Go" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'how to sort a slice in Go' in results")
	}
}

func TestRetrieveKLimit(t *testing.T) {
	e := NewEngine(Config{RetrievalK: 2, MinScore: 0.0})

	for i := 0; i < 5; i++ {
		e.AddExample("agent_1", "test query about Go programming language", "response", 0.9)
	}

	results := e.Retrieve("agent_1", "Go programming")
	if len(results) > 2 {
		t.Errorf("expected at most 2 results (k=2), got %d", len(results))
	}
}

func TestRetrieveEmptyAgent(t *testing.T) {
	e := NewEngine(Config{})
	results := e.Retrieve("nonexistent", "query")
	if results != nil {
		t.Errorf("expected nil for nonexistent agent, got %d results", len(results))
	}
}

func TestGetExamples(t *testing.T) {
	e := NewEngine(Config{})
	e.AddExample("agent_1", "q1", "r1", 0.5)
	e.AddExample("agent_1", "q2", "r2", 0.6)
	e.AddExample("agent_2", "q3", "r3", 0.7)

	examples := e.GetExamples("agent_1")
	if len(examples) != 2 {
		t.Errorf("expected 2 examples for agent_1, got %d", len(examples))
	}

	// Verify returned slice is a copy.
	examples[0].Score = 999
	original := e.GetExamples("agent_1")
	if original[0].Score == 999 {
		t.Error("GetExamples should return a copy, not a reference")
	}
}

func TestCount(t *testing.T) {
	e := NewEngine(Config{})
	if e.Count() != 0 {
		t.Errorf("expected 0, got %d", e.Count())
	}

	e.AddExample("agent_1", "q1", "r1", 0.5)
	e.AddExample("agent_1", "q2", "r2", 0.6)
	e.AddExample("agent_2", "q3", "r3", 0.7)
	if e.Count() != 3 {
		t.Errorf("expected 3, got %d", e.Count())
	}
}

func TestMarshalJSON(t *testing.T) {
	e := NewEngine(Config{})
	ex := e.AddExample("agent_1", "test query", "test response", 0.85)

	data, err := json.Marshal(ex)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if m["score_percent"] != "85.0%" {
		t.Errorf("expected score_percent '85.0%%', got %v", m["score_percent"])
	}
}

func TestCosineSim(t *testing.T) {
	tests := []struct {
		name     string
		a, b     []float64
		expected float64
	}{
		{"identical", []float64{1, 0, 0}, []float64{1, 0, 0}, 1.0},
		{"orthogonal", []float64{1, 0}, []float64{0, 1}, 0.0},
		{"empty", nil, nil, 0.0},
		{"single element", []float64{1}, []float64{-1}, -1.0},
		{"one empty", []float64{1, 2}, nil, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cosineSim(tt.a, tt.b)
			if math.Abs(got-tt.expected) > 1e-9 {
				t.Errorf("expected %f, got %f", tt.expected, got)
			}
		})
	}
}

func TestTextToVec(t *testing.T) {
	vec := textToVec("hello world")
	if len(vec) == 0 {
		t.Error("expected non-empty vector")
	}

	// Empty input.
	vec = textToVec("")
	if vec != nil {
		t.Errorf("expected nil for empty text, got %v", vec)
	}

	// Punctuation should be stripped.
	vec2 := textToVec("hello, world!")
	if len(vec2) == 0 {
		t.Error("expected non-empty vector for punctuated text")
	}
}

func TestTokenize(t *testing.T) {
	tokens := tokenize("Hello, World! 123")
	if len(tokens) != 3 {
		t.Errorf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	if tokens[0] != "hello" {
		t.Errorf("expected 'hello', got %q", tokens[0])
	}
	if tokens[1] != "world" {
		t.Errorf("expected 'world', got %q", tokens[1])
	}
	if tokens[2] != "123" {
		t.Errorf("expected '123', got %q", tokens[2])
	}

	tokens = tokenize("")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens, got %d", len(tokens))
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := NewEngine(Config{MaxExamples: 100})

	done := make(chan bool, 2)

	go func() {
		for i := 0; i < 50; i++ {
			e.AddExample("agent_1", "query", "response", 0.5)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 50; i++ {
			e.Retrieve("agent_1", "query")
		}
		done <- true
	}()

	<-done
	<-done
}
