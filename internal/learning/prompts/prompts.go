// Package prompts implements automatic prompt evolution.
//
// Analyzes failure patterns and success patterns to automatically improve
// system prompts. All changes are auditable and traceable.
package prompts

import (
	"fmt"
	"strings"
	"time"
)

// PromptVersion represents a versioned system prompt.
type PromptVersion struct {
	ID        string    `json:"id"`
	Agent     string    `json:"agent"`
	Prompt    string    `json:"prompt"`
	Version   int       `json:"version"`
	ParentID  string    `json:"parent_id,omitempty"` // Previous version
	Reason    string    `json:"reason"`              // Why this version was created
	Score     float64   `json:"score"`              // Quality score of this version
	CreatedAt time.Time `json:"created_at"`
}

// Evolution tracks how prompts evolve over time.
type Evolution struct {
	Agent    string          `json:"agent"`
	Versions []PromptVersion `json:"versions"`
	Current  int             `json:"current"` // Index of current version
}

// ChangeLog records what changed between prompt versions.
type ChangeLog struct {
	FromVersion string `json:"from_version"`
	ToVersion   string `json:"to_version"`
	Added       string `json:"added,omitempty"`
	Removed     string `json:"removed,omitempty"`
	Reason      string `json:"reason"`
}

// Engine manages prompt evolution for agents.
type Engine struct {
	evolutions map[string]*Evolution // agent → evolution
	counter    int
}

// NewEngine creates a new prompt evolution engine.
func NewEngine() *Engine {
	return &Engine{
		evolutions: make(map[string]*Evolution),
	}
}

// SetBasePrompt sets the initial prompt for an agent.
func (e *Engine) SetBasePrompt(agent, prompt string) *PromptVersion {
	e.counter++
	version := PromptVersion{
		ID:        fmt.Sprintf("prompt_%d", e.counter),
		Agent:     agent,
		Prompt:    prompt,
		Version:   1,
		Reason:    "Initial prompt",
		Score:     0.5, // Neutral starting score
		CreatedAt: time.Now().UTC(),
	}

	e.evolutions[agent] = &Evolution{
		Agent:    agent,
		Versions: []PromptVersion{version},
		Current:  0,
	}

	return &version
}

// GetCurrentPrompt returns the current prompt for an agent.
func (e *Engine) GetCurrentPrompt(agent string) (string, bool) {
	evo, ok := e.evolutions[agent]
	if !ok || len(evo.Versions) == 0 {
		return "", false
	}
	return evo.Versions[evo.Current].Prompt, true
}

// Evolve analyzes failure patterns and suggests prompt improvements.
// Returns the new prompt version if an evolution was made.
func (e *Engine) Evolve(agent string, failures []FailurePattern) (*PromptVersion, ChangeLog) {
	evo, ok := e.evolutions[agent]
	if !ok || len(evo.Versions) == 0 {
		return nil, ChangeLog{}
	}

	current := evo.Versions[evo.Current]
	newPrompt := current.Prompt

	// Analyze failures and suggest improvements.
	var changes []string
	for _, f := range failures {
		switch f.Type {
		case "wrong_tool_usage":
			if !strings.Contains(newPrompt, "When using tools") {
				newPrompt += "\n\nWhen using tools, always verify the tool output before presenting results to the user."
				changes = append(changes, "Added tool usage guidance")
			}
		case "incomplete_response":
			if !strings.Contains(newPrompt, "Provide complete") {
				newPrompt += "\n\nProvide complete, thorough responses. Address all parts of the user's question."
				changes = append(changes, "Added completeness guidance")
			}
		case "safety_violation":
			newPrompt += "\n\nNever assist with harmful, illegal, or unethical activities."
			changes = append(changes, "Added safety guardrail")
		case "hallucination":
			newPrompt += "\n\nOnly state facts you are confident about. If uncertain, say so explicitly."
			changes = append(changes, "Added uncertainty guidance")
		}
	}

	if len(changes) == 0 {
		return nil, ChangeLog{}
	}

	// Create new version.
	e.counter++
	newVersion := PromptVersion{
		ID:        fmt.Sprintf("prompt_%d", e.counter),
		Agent:     agent,
		Prompt:    newPrompt,
		Version:   current.Version + 1,
		ParentID:  current.ID,
		Reason:    strings.Join(changes, "; "),
		Score:     current.Score, // Inherit score until re-evaluated
		CreatedAt: time.Now().UTC(),
	}

	evo.Versions = append(evo.Versions, newVersion)
	evo.Current = len(evo.Versions) - 1

	log := ChangeLog{
		FromVersion: current.ID,
		ToVersion:   newVersion.ID,
		Reason:      newVersion.Reason,
	}

	return &newVersion, log
}

// GetEvolution returns the full evolution history for an agent.
func (e *Engine) GetEvolution(agent string) (*Evolution, bool) {
	evo, ok := e.evolutions[agent]
	return evo, ok
}

// Rollback reverts to a previous prompt version.
func (e *Engine) Rollback(agent string, versionID string) bool {
	evo, ok := e.evolutions[agent]
	if !ok {
		return false
	}
	for i, v := range evo.Versions {
		if v.ID == versionID {
			evo.Current = i
			return true
		}
	}
	return false
}

// FailurePattern describes a failure that triggered prompt evolution.
type FailurePattern struct {
	Type    string  `json:"type"`    // "wrong_tool_usage", "incomplete_response", etc.
	Count   int     `json:"count"`   // How many times this failure occurred
	RunIDs  []string `json:"run_ids"` // Specific runs where this occurred
}
