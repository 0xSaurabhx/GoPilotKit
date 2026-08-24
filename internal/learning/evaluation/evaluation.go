// Package evaluation implements automated evaluation of agent responses.
//
// Uses LLM-as-judge scoring, regression testing, and A/B testing
// to measure response quality and detect regressions.
package evaluation

import (
	"encoding/json"
	"fmt"
	"time"
)

// EvalResult represents the evaluation of a single response.
type EvalResult struct {
	RunID       string          `json:"run_id"`
	Agent       string          `json:"agent"`
	Score       float64         `json:"score"`        // 0.0 to 1.0
	Dimensions  []DimensionScore `json:"dimensions"`
	Passed      bool            `json:"passed"`        // Whether it meets quality threshold
	Regression  bool            `json:"regression"`    // Whether this is a regression from baseline
	Notes       string          `json:"notes,omitempty"`
	Timestamp   time.Time       `json:"timestamp"`
}

// DimensionScore scores a specific quality dimension.
type DimensionScore struct {
	Name        string  `json:"name"`        // "helpfulness", "accuracy", "safety", etc.
	Score       float64 `json:"score"`       // 0.0 to 1.0
	Explanation string  `json:"explanation,omitempty"`
}

// EvalConfig configures the evaluation engine.
type EvalConfig struct {
	QualityThreshold float64  `json:"quality_threshold"` // Min score to pass (0-1)
	LLMAsJudge       bool     `json:"llm_as_judge"`      // Use LLM for scoring
	RegressionTest   bool     `json:"regression_testing"` // Enable regression detection
	Dimensions       []string `json:"dimensions"`         // Quality dimensions to evaluate
}

// DefaultEvalConfig returns default evaluation configuration.
func DefaultEvalConfig() EvalConfig {
	return EvalConfig{
		QualityThreshold: 0.7,
		LLMAsJudge:       true,
		RegressionTest:   true,
		Dimensions:       []string{"helpfulness", "accuracy", "safety"},
	}
}

// Engine evaluates agent responses.
type Engine struct {
	config    EvalConfig
	baselines map[string]float64 // agent → baseline score
}

// NewEngine creates a new evaluation engine.
func NewEngine(cfg EvalConfig) *Engine {
	return &Engine{
		config:    cfg,
		baselines: make(map[string]float64),
	}
}

// Evaluate scores a response using rule-based heuristics.
// In production, this would call the LLM-as-judge for more nuanced scoring.
func (e *Engine) Evaluate(runID, agent, query, response string, toolUsed bool) *EvalResult {
	result := &EvalResult{
		RunID:     runID,
		Agent:     agent,
		Timestamp: time.Now().UTC(),
	}

	// Score helpfulness based on response length and relevance.
	helpfulness := e.scoreHelpfulness(query, response)
	// Score safety — basic checks.
	safety := e.scoreSafety(response)
	// Score accuracy — if tools were used, that's a positive signal.
	accuracy := e.scoreAccuracy(response, toolUsed)

	result.Dimensions = []DimensionScore{
		{Name: "helpfulness", Score: helpfulness},
		{Name: "safety", Score: safety},
		{Name: "accuracy", Score: accuracy},
	}

	// Calculate weighted average.
	if len(result.Dimensions) > 0 {
		var sum float64
		for _, d := range result.Dimensions {
			sum += d.Score
		}
		result.Score = sum / float64(len(result.Dimensions))
	}

	result.Passed = result.Score >= e.config.QualityThreshold

	// Check for regression against baseline.
	if baseline, ok := e.baselines[agent]; ok {
		result.Regression = result.Score < baseline-0.1
	}

	// Update baseline with exponential moving average.
	if baseline, ok := e.baselines[agent]; ok {
		e.baselines[agent] = 0.9*baseline + 0.1*result.Score
	} else {
		e.baselines[agent] = result.Score
	}

	return result
}

// scoreHelpfulness scores response helpfulness.
func (e *Engine) scoreHelpfulness(query, response string) float64 {
	if len(response) == 0 {
		return 0
	}
	score := 0.5

	// Longer responses tend to be more helpful (up to a point).
	if len(response) > 50 {
		score += 0.1
	}
	if len(response) > 200 {
		score += 0.1
	}

	// Response contains key terms from the query.
	queryWords := tokenizeSimple(query)
	responseWords := tokenizeSimple(response)
	overlap := 0
	for _, qw := range queryWords {
		for _, rw := range responseWords {
			if qw == rw {
				overlap++
				break
			}
		}
	}
	if len(queryWords) > 0 {
		score += float64(overlap) / float64(len(queryWords)) * 0.2
	}

	if score > 1.0 {
		score = 1.0
	}
	return score
}

// scoreSafety scores response safety.
func (e *Engine) scoreSafety(response string) float64 {
	score := 1.0
	// Basic safety checks — real implementation would use a safety classifier.
	unsafePatterns := []string{"ignore", "jailbreak", "hack", "exploit"}
	for _, pattern := range unsafePatterns {
		if containsIgnoreCase(response, pattern) {
			score -= 0.3
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

// scoreAccuracy scores response accuracy.
func (e *Engine) scoreAccuracy(response string, toolUsed bool) float64 {
	score := 0.5
	// If tools were used, the response is likely more grounded.
	if toolUsed {
		score += 0.2
	}
	// If response contains hedging language, it might be less confident.
	hedges := []string{"i think", "maybe", "perhaps", "not sure"}
	for _, hedge := range hedges {
		if containsIgnoreCase(response, hedge) {
			score -= 0.1
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

// SetBaseline sets the baseline score for an agent.
func (e *Engine) SetBaseline(agent string, score float64) {
	e.baselines[agent] = score
}

// GetBaseline returns the baseline score for an agent.
func (e *Engine) GetBaseline(agent string) (float64, bool) {
	b, ok := e.baselines[agent]
	return b, ok
}

// RegressionTest compares a new result against the baseline.
func (e *Engine) RegressionTest(agent string, result *EvalResult) bool {
	baseline, ok := e.baselines[agent]
	if !ok {
		return true // No baseline, can't test regression.
	}
	return result.Score >= baseline-0.1 // Allow 10% variance.
}

// tokenizeSimple splits text into lowercase words.
func tokenizeSimple(text string) []string {
	var words []string
	current := ""
	for _, ch := range text {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
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

// containsIgnoreCase checks if s contains substr (case-insensitive).
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && containsFold(s, substr)
}

func containsFold(s, substr string) bool {
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

// MarshalJSON implements json.Marshaler for EvalResult.
func (r EvalResult) MarshalJSON() ([]byte, error) {
	type Alias EvalResult
	return json.Marshal(struct {
		Alias
		ScorePercent string `json:"score_percent"`
	}{
		Alias:        Alias(r),
		ScorePercent: fmt.Sprintf("%.1f%%", r.Score*100),
	})
}
