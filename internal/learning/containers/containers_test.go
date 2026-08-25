package containers

import (
	"encoding/json"
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.Count() != 0 {
		t.Errorf("expected 0 containers, got %d", e.Count())
	}
}

func TestCreateContainer(t *testing.T) {
	e := NewEngine()
	cfg := ContainerConfig{MinInteractions: 5, ImprovementThreshold: 0.2}

	c := e.Create("test-container", ScopeUser, "user_1", "agent_1", cfg)
	if c == nil {
		t.Fatal("expected non-nil container")
	}
	if c.ID == "" {
		t.Error("expected non-empty ID")
	}
	if c.Name != "test-container" {
		t.Errorf("expected name 'test-container', got %q", c.Name)
	}
	if c.Scope != ScopeUser {
		t.Errorf("expected scope 'user', got %q", c.Scope)
	}
	if c.ScopeID != "user_1" {
		t.Errorf("expected scope_id 'user_1', got %q", c.ScopeID)
	}
	if c.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", c.Agent)
	}
	if !c.Enabled {
		t.Error("expected container to be enabled")
	}
	if c.MinInteractions != 5 {
		t.Errorf("expected min_interactions 5, got %d", c.MinInteractions)
	}
	if c.ImprovementThreshold != 0.2 {
		t.Errorf("expected improvement_threshold 0.2, got %f", c.ImprovementThreshold)
	}
}

func TestCreateContainerDefaults(t *testing.T) {
	e := NewEngine()
	c := e.Create("default", ScopeProject, "proj_1", "agent_1", ContainerConfig{})

	if c.MinInteractions != 10 {
		t.Errorf("expected default min_interactions 10, got %d", c.MinInteractions)
	}
	if c.ImprovementThreshold != 0.1 {
		t.Errorf("expected default improvement_threshold 0.1, got %f", c.ImprovementThreshold)
	}
}

func TestGetContainer(t *testing.T) {
	e := NewEngine()
	c := e.Create("test", ScopeUser, "user_1", "agent_1", ContainerConfig{})

	got, ok := e.Get(c.ID)
	if !ok {
		t.Fatal("expected to find container")
	}
	if got.ID != c.ID {
		t.Errorf("expected ID %s, got %s", c.ID, got.ID)
	}

	_, ok = e.Get("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent ID")
	}
}

func TestFindByScope(t *testing.T) {
	e := NewEngine()
	e.Create("c1", ScopeUser, "user_1", "agent_1", ContainerConfig{})
	e.Create("c2", ScopeUser, "user_2", "agent_1", ContainerConfig{})
	e.Create("c3", ScopeOrganization, "org_1", "agent_1", ContainerConfig{})

	results := e.FindByScope(ScopeUser, "user_1")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "c1" {
		t.Errorf("expected c1, got %s", results[0].Name)
	}

	results = e.FindByScope(ScopeUser, "user_1")
	if len(results) != 1 {
		t.Errorf("expected 1 user_1 container, got %d", len(results))
	}

	results = e.FindByScope(ScopeProject, "nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFindByAgent(t *testing.T) {
	e := NewEngine()
	e.Create("c1", ScopeUser, "user_1", "agent_a", ContainerConfig{})
	e.Create("c2", ScopeUser, "user_2", "agent_a", ContainerConfig{})
	e.Create("c3", ScopeUser, "user_3", "agent_b", ContainerConfig{})

	results := e.FindByAgent("agent_a")
	if len(results) != 2 {
		t.Fatalf("expected 2 results for agent_a, got %d", len(results))
	}

	results = e.FindByAgent("nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestRecordInteraction(t *testing.T) {
	e := NewEngine()
	c := e.Create("test", ScopeUser, "user_1", "agent_1", ContainerConfig{})

	e.RecordInteraction(c.ID)
	got, _ := e.Get(c.ID)
	if got.TotalInteractions != 1 {
		t.Errorf("expected 1 interaction, got %d", got.TotalInteractions)
	}

	e.RecordInteraction(c.ID)
	got, _ = e.Get(c.ID)
	if got.TotalInteractions != 2 {
		t.Errorf("expected 2 interactions, got %d", got.TotalInteractions)
	}

	// Recording for nonexistent container should not panic.
	e.RecordInteraction("nonexistent")
}

func TestShouldImprove(t *testing.T) {
	e := NewEngine()
	c := e.Create("test", ScopeUser, "user_1", "agent_1", ContainerConfig{MinInteractions: 3})

	if e.ShouldImprove(c.ID) {
		t.Error("should not improve with 0 interactions")
	}

	e.RecordInteraction(c.ID)
	e.RecordInteraction(c.ID)
	if e.ShouldImprove(c.ID) {
		t.Error("should not improve with 2 interactions (need 3)")
	}

	e.RecordInteraction(c.ID)
	if !e.ShouldImprove(c.ID) {
		t.Error("should improve with 3 interactions")
	}

	// Disabled container should never improve.
	e2 := NewEngine()
	c2 := e2.Create("disabled", ScopeUser, "user_1", "agent_1", ContainerConfig{MinInteractions: 1})
	c2.Enabled = false
	e2.RecordInteraction(c2.ID)
	if e2.ShouldImprove(c2.ID) {
		t.Error("disabled container should not improve")
	}

	// Nonexistent container.
	if e.ShouldImprove("nonexistent") {
		t.Error("nonexistent container should not improve")
	}
}

func TestRecordImprovement(t *testing.T) {
	e := NewEngine()
	c := e.Create("test", ScopeUser, "user_1", "agent_1", ContainerConfig{})

	e.RecordImprovement(c.ID)
	got, _ := e.Get(c.ID)
	if got.ImprovementsMade != 1 {
		t.Errorf("expected 1 improvement, got %d", got.ImprovementsMade)
	}
	if got.LastImprovementAt == nil {
		t.Error("expected last_improvement_at to be set")
	}

	e.RecordImprovement(c.ID)
	got, _ = e.Get(c.ID)
	if got.ImprovementsMade != 2 {
		t.Errorf("expected 2 improvements, got %d", got.ImprovementsMade)
	}

	// Nonexistent should not panic.
	e.RecordImprovement("nonexistent")
}

func TestList(t *testing.T) {
	e := NewEngine()
	e.Create("c1", ScopeUser, "user_1", "agent_1", ContainerConfig{})
	e.Create("c2", ScopeUser, "user_2", "agent_1", ContainerConfig{})

	list := e.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 containers, got %d", len(list))
	}
}

func TestCount(t *testing.T) {
	e := NewEngine()
	if e.Count() != 0 {
		t.Errorf("expected 0, got %d", e.Count())
	}
	e.Create("c1", ScopeUser, "user_1", "agent_1", ContainerConfig{})
	e.Create("c2", ScopeUser, "user_2", "agent_1", ContainerConfig{})
	if e.Count() != 2 {
		t.Errorf("expected 2, got %d", e.Count())
	}
}

func TestMarshalJSON(t *testing.T) {
	e := NewEngine()
	c := e.Create("test", ScopeUser, "user_1", "agent_1", ContainerConfig{MinInteractions: 5})

	// Before interactions, ready_to_improve should be false.
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if m["ready_to_improve"] != false {
		t.Errorf("expected ready_to_improve=false, got %v", m["ready_to_improve"])
	}

	// After enough interactions, ready_to_improve should be true.
	for i := 0; i < 5; i++ {
		e.RecordInteraction(c.ID)
	}
	data, _ = json.Marshal(c)
	json.Unmarshal(data, &m)
	if m["ready_to_improve"] != true {
		t.Errorf("expected ready_to_improve=true, got %v", m["ready_to_improve"])
	}
}

func TestContainerScopeConstants(t *testing.T) {
	if ScopeUser != "user" {
		t.Errorf("expected 'user', got %q", ScopeUser)
	}
	if ScopeOrganization != "organization" {
		t.Errorf("expected 'organization', got %q", ScopeOrganization)
	}
	if ScopeProject != "project" {
		t.Errorf("expected 'project', got %q", ScopeProject)
	}
	if ScopeAgent != "agent" {
		t.Errorf("expected 'agent', got %q", ScopeAgent)
	}
}
