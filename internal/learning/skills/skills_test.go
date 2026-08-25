package skills

import (
	"testing"
)

func TestNewEngine(t *testing.T) {
	e := NewEngine()
	if e == nil {
		t.Fatal("expected non-nil engine")
	}
	if e.Count() != 0 {
		t.Errorf("expected 0 skills, got %d", e.Count())
	}
}

func TestRecordSkill(t *testing.T) {
	e := NewEngine()
	steps := []SkillStep{
		{Action: "query", Input: "user question", Output: "answer"},
		{Action: "tool_call", Input: "search", ToolName: "web_search", Output: "results"},
	}

	skill := e.RecordSkill("agent_1", "answer questions", "Answer user questions", "research", steps)
	if skill == nil {
		t.Fatal("expected non-nil skill")
	}
	if skill.ID == "" {
		t.Error("expected non-empty ID")
	}
	if skill.Name != "answer questions" {
		t.Errorf("expected name 'answer questions', got %q", skill.Name)
	}
	if skill.Description != "Answer user questions" {
		t.Errorf("unexpected description")
	}
	if skill.Agent != "agent_1" {
		t.Errorf("expected agent 'agent_1', got %q", skill.Agent)
	}
	if skill.Category != "research" {
		t.Errorf("expected category 'research', got %q", skill.Category)
	}
	if len(skill.Steps) != 2 {
		t.Errorf("expected 2 steps, got %d", len(skill.Steps))
	}
	if skill.SuccessRate != 1.0 {
		t.Errorf("expected initial success rate 1.0, got %f", skill.SuccessRate)
	}
	if e.Count() != 1 {
		t.Errorf("expected count 1, got %d", e.Count())
	}
}

func TestFindSkills(t *testing.T) {
	e := NewEngine()
	e.RecordSkill("agent_1", "answer questions", "Answer user questions", "research", nil)
	e.RecordSkill("agent_1", "write code", "Write Go programs", "coding", nil)
	e.RecordSkill("agent_1", "debug code", "Find and fix bugs", "coding", nil)
	e.RecordSkill("agent_2", "answer questions", "Answer user questions", "research", nil)

	// Search by name.
	results := e.FindSkills("agent_1", "answer")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "answer questions" {
		t.Errorf("expected 'answer questions', got %q", results[0].Name)
	}

	// Search by description.
	results = e.FindSkills("agent_1", "Go")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "write code" {
		t.Errorf("expected 'write code', got %q", results[0].Name)
	}

	// Search for code-related skills.
	results = e.FindSkills("agent_1", "code")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Nonexistent agent.
	results = e.FindSkills("nonexistent", "query")
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}

	// No matches.
	results = e.FindSkills("agent_1", "nonexistent")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestGetSkills(t *testing.T) {
	e := NewEngine()
	e.RecordSkill("agent_1", "skill1", "desc1", "cat", nil)
	e.RecordSkill("agent_1", "skill2", "desc2", "cat", nil)
	e.RecordSkill("agent_2", "skill3", "desc3", "cat", nil)

	skills := e.GetSkills("agent_1")
	if len(skills) != 2 {
		t.Errorf("expected 2 skills, got %d", len(skills))
	}

	// Verify copy semantics.
	skills[0].Name = "modified"
	original := e.GetSkills("agent_1")
	if original[0].Name == "modified" {
		t.Error("GetSkills should return a copy")
	}
}

func TestUpdateSuccessRate(t *testing.T) {
	e := NewEngine()
	skill := e.RecordSkill("agent_1", "test", "test skill", "cat", nil)

	// Force a lower starting rate so we can observe changes.
	e.mu.Lock()
	e.skills["agent_1"][0].SuccessRate = 0.5
	e.mu.Unlock()

	// Successful use: 0.5*0.9 + 0.1 = 0.55
	e.UpdateSuccessRate("agent_1", skill.ID, true)
	skills := e.GetSkills("agent_1")
	if skills[0].UsageCount != 1 {
		t.Errorf("expected usage_count 1, got %d", skills[0].UsageCount)
	}
	if skills[0].SuccessRate <= 0.5 {
		t.Errorf("expected success rate to increase, got %f", skills[0].SuccessRate)
	}
	if skills[0].LastUsedAt == nil {
		t.Error("expected last_used_at to be set")
	}

	// Failed use: 0.55*0.9 = 0.495
	e.UpdateSuccessRate("agent_1", skill.ID, false)
	skills = e.GetSkills("agent_1")
	if skills[0].UsageCount != 2 {
		t.Errorf("expected usage_count 2, got %d", skills[0].UsageCount)
	}
	if skills[0].SuccessRate >= 0.55 {
		t.Errorf("expected success rate to decrease, got %f", skills[0].SuccessRate)
	}
}

func TestUpdateSuccessRateNonexistent(t *testing.T) {
	e := NewEngine()
	// Should not panic.
	e.UpdateSuccessRate("nonexistent", "skill_999", true)
	e.UpdateSuccessRate("agent_1", "nonexistent_id", true)
}

func TestCount(t *testing.T) {
	e := NewEngine()
	if e.Count() != 0 {
		t.Errorf("expected 0, got %d", e.Count())
	}
	e.RecordSkill("agent_1", "s1", "d1", "c1", nil)
	e.RecordSkill("agent_2", "s2", "d2", "c2", nil)
	if e.Count() != 2 {
		t.Errorf("expected 2, got %d", e.Count())
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s, sub string
		want   bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "xyz", false},
		{"", "", true},
		{"Go is great", "GO", true},
	}

	for _, tt := range tests {
		got := containsIgnoreCase(tt.s, tt.sub)
		if got != tt.want {
			t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.sub, got, tt.want)
		}
	}
}

func TestToLower(t *testing.T) {
	result := toLower("Hello WORLD 123")
	if result != "hello world 123" {
		t.Errorf("expected 'hello world 123', got %q", result)
	}
}

func TestConcurrentAccess(t *testing.T) {
	e := NewEngine()

	done := make(chan bool, 3)

	go func() {
		for i := 0; i < 20; i++ {
			e.RecordSkill("agent_1", "skill", "desc", "cat", nil)
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 20; i++ {
			e.FindSkills("agent_1", "skill")
		}
		done <- true
	}()

	go func() {
		for i := 0; i < 20; i++ {
			e.Count()
		}
		done <- true
	}()

	<-done
	<-done
	<-done
}
