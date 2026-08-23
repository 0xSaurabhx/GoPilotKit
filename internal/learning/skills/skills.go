// Package skills implements skill recording.
//
// Learns new tasks from user demonstrations, building a library of
// repeatable skills that the agent can reuse in future interactions.
package skills

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// Skill represents a learned task.
type Skill struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Agent       string          `json:"agent"`
	Steps       []SkillStep     `json:"steps"`
	Trigger     string          `json:"trigger"`     // Pattern that triggers this skill
	Category    string          `json:"category"`    // "data_entry", "research", "coding", etc.
	SuccessRate float64         `json:"success_rate"` // 0.0 to 1.0
	UsageCount  int             `json:"usage_count"`
	LastUsedAt  *time.Time      `json:"last_used_at,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// SkillStep is a single step in a skill.
type SkillStep struct {
	Action    string          `json:"action"`    // "query", "tool_call", "response", "condition"
	Input     string          `json:"input"`
	Output    string          `json:"output"`
	ToolName  string          `json:"tool_name,omitempty"`
	ToolArgs  json.RawMessage `json:"tool_args,omitempty"`
	Condition string          `json:"condition,omitempty"` // For conditional steps
}

// Engine manages skill recording and retrieval.
type Engine struct {
	skills   map[string][]Skill // agent → skills
	counter  int
	mu       sync.RWMutex
}

// NewEngine creates a new skill recording engine.
func NewEngine() *Engine {
	return &Engine{
		skills: make(map[string][]Skill),
	}
}

// RecordSkill records a new skill from a demonstration.
func (e *Engine) RecordSkill(agent, name, description, category string, steps []SkillStep) *Skill {
	e.counter++
	skill := Skill{
		ID:          fmt.Sprintf("skill_%d", e.counter),
		Name:        name,
		Description: description,
		Agent:       agent,
		Steps:       steps,
		Category:    category,
		SuccessRate: 1.0, // Start with perfect rate
		CreatedAt:   time.Now().UTC(),
	}
	e.skills[agent] = append(e.skills[agent], skill)
	return &e.skills[agent][len(e.skills[agent])-1]
}

// FindSkills finds skills matching a query pattern.
func (e *Engine) FindSkills(agent, query string) []Skill {
	e.mu.RLock()
	defer e.mu.RUnlock()

	skills, ok := e.skills[agent]
	if !ok {
		return nil
	}

	var matches []Skill
	lower := query
	for _, s := range skills {
		if containsIgnoreCase(s.Name, lower) || containsIgnoreCase(s.Description, lower) {
			matches = append(matches, s)
		}
	}
	return matches
}

// GetSkills returns all skills for an agent.
func (e *Engine) GetSkills(agent string) []Skill {
	e.mu.RLock()
	defer e.mu.RUnlock()
	skills := e.skills[agent]
	result := make([]Skill, len(skills))
	copy(result, skills)
	return result
}

// UpdateSuccessRate updates the success rate for a skill.
func (e *Engine) UpdateSuccessRate(agent, skillID string, success bool) {
	e.mu.Lock()
	defer e.mu.Unlock()

	skills, ok := e.skills[agent]
	if !ok {
		return
	}
	for i := range skills {
		if skills[i].ID == skillID {
			skills[i].UsageCount++
			now := time.Now().UTC()
			skills[i].LastUsedAt = &now
			// Exponential moving average.
			if success {
				skills[i].SuccessRate = skills[i].SuccessRate*0.9 + 0.1
			} else {
				skills[i].SuccessRate = skills[i].SuccessRate * 0.9
			}
			return
		}
	}
}

// Count returns total skills across all agents.
func (e *Engine) Count() int {
	e.mu.RLock()
	defer e.mu.RUnlock()
	total := 0
	for _, skills := range e.skills {
		total += len(skills)
	}
	return total
}

func containsIgnoreCase(s, substr string) bool {
	s = toLower(s)
	substr = toLower(substr)
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
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
