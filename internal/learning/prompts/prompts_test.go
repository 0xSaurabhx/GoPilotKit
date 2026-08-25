package prompts

import (
	"strings"
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestSetBasePrompt(t *testing.T) {
	e := NewEngine()
	v := e.SetBasePrompt("agent_1", "You are a helpful assistant.")

	if v == nil {
		t.Fatal("expected non-nil version")
	}
	if v.ID == "" {
		t.Error("expected non-empty ID")
	}
	if v.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", v.Agent)
	}
	if v.Prompt != "You are a helpful assistant." {
		t.Errorf("unexpected prompt content")
	}
	if v.Version != 1 {
		t.Errorf("expected version 1, got %d", v.Version)
	}
	if v.Reason != "Initial prompt" {
		t.Errorf("expected reason 'Initial prompt', got %q", v.Reason)
	}
	if v.Score != 0.5 {
		t.Errorf("expected score 0.5, got %f", v.Score)
	}
}

func TestGetCurrentPrompt(t *testing.T) {
	e := NewEngine()

	// No agent yet.
	prompt, ok := e.GetCurrentPrompt("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent agent")
	}
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}

	e.SetBasePrompt("agent_1", "You are helpful.")
	prompt, ok = e.GetCurrentPrompt("agent_1")
	if !ok {
		t.Fatal("expected to find prompt")
	}
	if prompt != "You are helpful." {
		t.Errorf("expected 'You are helpful.', got %q", prompt)
	}
}

func TestEvolveWrongToolUsage(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "You are a helpful assistant.")

	failures := []FailurePattern{
		{Type: "wrong_tool_usage", Count: 3, RunIDs: []string{"run_1"}},
	}

	newVersion, log := e.Evolve("agent_1", failures)
	if newVersion == nil {
		t.Fatal("expected new version")
	}
	if newVersion.Version != 2 {
		t.Errorf("expected version 2, got %d", newVersion.Version)
	}
	if newVersion.ParentID == "" {
		t.Error("expected non-empty parent ID")
	}
	if !strings.Contains(newVersion.Prompt, "When using tools") {
		t.Error("expected tool usage guidance in evolved prompt")
	}
	if log.Reason == "" {
		t.Error("expected non-empty reason in changelog")
	}

	// Current prompt should now be the evolved one.
	prompt, _ := e.GetCurrentPrompt("agent_1")
	if !strings.Contains(prompt, "When using tools") {
		t.Error("current prompt should contain tool usage guidance")
	}
}

func TestEvolveIncompleteResponse(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "Be helpful.")

	failures := []FailurePattern{
		{Type: "incomplete_response", Count: 2},
	}

	newVersion, _ := e.Evolve("agent_1", failures)
	if newVersion == nil {
		t.Fatal("expected new version")
	}
	if !strings.Contains(newVersion.Prompt, "Provide complete") {
		t.Error("expected completeness guidance")
	}
}

func TestEvolveSafetyViolation(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "Be helpful.")

	failures := []FailurePattern{
		{Type: "safety_violation", Count: 1},
	}

	newVersion, _ := e.Evolve("agent_1", failures)
	if newVersion == nil {
		t.Fatal("expected new version")
	}
	if !strings.Contains(newVersion.Prompt, "Never assist with harmful") {
		t.Error("expected safety guardrail")
	}
}

func TestEvolveHallucination(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "Be helpful.")

	failures := []FailurePattern{
		{Type: "hallucination", Count: 5},
	}

	newVersion, _ := e.Evolve("agent_1", failures)
	if newVersion == nil {
		t.Fatal("expected new version")
	}
	if !strings.Contains(newVersion.Prompt, "Only state facts") {
		t.Error("expected uncertainty guidance")
	}
}

func TestEvolveNoMatch(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "You are helpful.")

	failures := []FailurePattern{
		{Type: "unknown_type", Count: 1},
	}

	newVersion, _ := e.Evolve("agent_1", failures)
	if newVersion != nil {
		t.Error("expected nil for unrecognized failure type")
	}
}

func TestEvolveNonexistentAgent(t *testing.T) {
	e := NewEngine()
	failures := []FailurePattern{{Type: "wrong_tool_usage", Count: 1}}

	newVersion, _ := e.Evolve("nonexistent", failures)
	if newVersion != nil {
		t.Error("expected nil for nonexistent agent")
	}
}

func TestEvolveMultipleVersions(t *testing.T) {
	e := NewEngine()
	e.SetBasePrompt("agent_1", "Base prompt.")

	// First evolution.
	failures := []FailurePattern{{Type: "wrong_tool_usage", Count: 1}}
	v2, _ := e.Evolve("agent_1", failures)
	if v2 == nil || v2.Version != 2 {
		t.Fatalf("expected version 2, got %v", v2)
	}

	// Second evolution.
	failures = []FailurePattern{{Type: "hallucination", Count: 1}}
	v3, _ := e.Evolve("agent_1", failures)
	if v3 == nil || v3.Version != 3 {
		t.Fatalf("expected version 3, got %v", v3)
	}
	if v3.ParentID != v2.ID {
		t.Error("expected parent to be v2")
	}
}

func TestGetEvolution(t *testing.T) {
	e := NewEngine()

	evo, ok := e.GetEvolution("nonexistent")
	if ok {
		t.Error("expected not found")
	}
	if evo != nil {
		t.Error("expected nil evolution")
	}

	e.SetBasePrompt("agent_1", "prompt")
	evo, ok = e.GetEvolution("agent_1")
	if !ok {
		t.Fatal("expected found")
	}
	if evo.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", evo.Agent)
	}
	if len(evo.Versions) != 1 {
		t.Errorf("expected 1 version, got %d", len(evo.Versions))
	}
}

func TestRollback(t *testing.T) {
	e := NewEngine()
	v1 := e.SetBasePrompt("agent_1", "Version 1 prompt.")

	failures := []FailurePattern{{Type: "wrong_tool_usage", Count: 1}}
	v2, _ := e.Evolve("agent_1", failures)

	// Current should be v2.
	prompt, _ := e.GetCurrentPrompt("agent_1")
	if prompt != v2.Prompt {
		t.Error("expected current to be v2")
	}

	// Rollback to v1.
	ok := e.Rollback("agent_1", v1.ID)
	if !ok {
		t.Fatal("expected rollback success")
	}

	prompt, _ = e.GetCurrentPrompt("agent_1")
	if prompt != v1.Prompt {
		t.Errorf("expected rolled back to v1 prompt, got %q", prompt)
	}

	// Rollback to nonexistent version.
	ok = e.Rollback("agent_1", "nonexistent")
	if ok {
		t.Error("expected rollback failure for nonexistent version")
	}

	// Rollback for nonexistent agent.
	ok = e.Rollback("nonexistent", v1.ID)
	if ok {
		t.Error("expected rollback failure for nonexistent agent")
	}
}
