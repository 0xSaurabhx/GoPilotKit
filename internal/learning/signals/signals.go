// Package signals extracts implicit and explicit feedback signals from user interactions.
//
// Signals drive the autonomous learning pipeline: successful patterns get reinforced,
// failures trigger prompt evolution, and user preferences shape few-shot curation.
package signals

import (
	"encoding/json"
	"strings"
	"time"
)

// SignalType represents the type of feedback signal.
type SignalType string

const (
	SignalExplicitPositive SignalType = "explicit_positive" // Thumbs up, "great answer"
	SignalExplicitNegative SignalType = "explicit_negative" // Thumbs down, "wrong answer"
	SignalImplicitPositive SignalType = "implicit_positive" // User accepted, copied, used tool output
	SignalImplicitNegative SignalType = "implicit_negative" // User rephrased, abandoned, retried
	SignalToolRetry        SignalType = "tool_retry"        // User asked to retry tool call
	SignalClarification    SignalType = "clarification"     // User asked follow-up question
	SignalCorrection       SignalType = "correction"        // User corrected the response
	SignalOverride         SignalType = "override"          // Human-in-the-loop override
)

// Signal represents a single extracted feedback signal.
type Signal struct {
	Type      SignalType      `json:"type"`
	RunID     string          `json:"run_id"`
	ThreadID  string          `json:"thread_id"`
	Agent     string          `json:"agent"`
	Value     float64         `json:"value"`      // -1.0 (very negative) to 1.0 (very positive)
	Confidence float64        `json:"confidence"`  // 0.0 to 1.0
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Extractor analyzes interactions and extracts signals.
type Extractor struct{}

// NewExtractor creates a new signal extractor.
func NewExtractor() *Extractor {
	return &Extractor{}
}

// ExtractFromFeedback extracts signals from explicit user feedback.
func (e *Extractor) ExtractFromFeedback(runID, threadID, agent string, feedback string) *Signal {
	lower := strings.ToLower(feedback)

	// Positive signals.
	positiveWords := []string{"good", "great", "excellent", "perfect", "thanks", "helpful", "correct", "right", "yes"}
	for _, word := range positiveWords {
		if strings.Contains(lower, word) {
			return &Signal{
				Type:       SignalExplicitPositive,
				RunID:      runID,
				ThreadID:   threadID,
				Agent:      agent,
				Value:      0.8,
				Confidence: 0.9,
				Timestamp:  time.Now().UTC(),
			}
		}
	}

	// Negative signals.
	negativeWords := []string{"wrong", "bad", "incorrect", "error", "no", "terrible", "useless", "fix"}
	for _, word := range negativeWords {
		if strings.Contains(lower, word) {
			return &Signal{
				Type:       SignalExplicitNegative,
				RunID:      runID,
				ThreadID:   threadID,
				Agent:      agent,
				Value:      -0.8,
				Confidence: 0.9,
				Timestamp:  time.Now().UTC(),
			}
		}
	}

	// Correction signal.
	correctionWords := []string{"actually", "i meant", "not what i", "should be", "change to"}
	for _, word := range correctionWords {
		if strings.Contains(lower, word) {
			return &Signal{
				Type:       SignalCorrection,
				RunID:      runID,
				ThreadID:   threadID,
				Agent:      agent,
				Value:      -0.5,
				Confidence: 0.7,
				Timestamp:  time.Now().UTC(),
			}
		}
	}

	return nil
}

// ExtractFromInteraction extracts implicit signals from interaction patterns.
func (e *Extractor) ExtractFromInteraction(runID, threadID, agent string, action InteractionAction) *Signal {
	switch action.Type {
	case "message_edited":
		return &Signal{
			Type:       SignalImplicitNegative,
			RunID:      runID,
			ThreadID:   threadID,
			Agent:      agent,
			Value:      -0.3,
			Confidence: 0.5,
			Timestamp:  time.Now().UTC(),
		}
	case "response_copied":
		return &Signal{
			Type:       SignalImplicitPositive,
			RunID:      runID,
			ThreadID:   threadID,
			Agent:      agent,
			Value:      0.6,
			Confidence: 0.7,
			Timestamp:  time.Now().UTC(),
		}
	case "tool_output_used":
		return &Signal{
			Type:       SignalImplicitPositive,
			RunID:      runID,
			ThreadID:   threadID,
			Agent:      agent,
			Value:      0.5,
			Confidence: 0.6,
			Timestamp:  time.Now().UTC(),
		}
	case "followup_question":
		return &Signal{
			Type:       SignalClarification,
			RunID:      runID,
			ThreadID:   threadID,
			Agent:      agent,
			Value:      -0.1,
			Confidence: 0.4,
			Timestamp:  time.Now().UTC(),
		}
	}
	return nil
}

// InteractionAction represents a user interaction that can be used to extract signals.
type InteractionAction struct {
	Type      string            `json:"type"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
}

// AggregateSignals combines multiple signals into a summary score.
func (e *Extractor) AggregateSignals(signals []Signal) float64 {
	if len(signals) == 0 {
		return 0
	}

	var totalWeighted float64
	var totalWeight float64
	for _, s := range signals {
		totalWeighted += s.Value * s.Confidence
		totalWeight += s.Confidence
	}
	if totalWeight == 0 {
		return 0
	}
	return totalWeighted / totalWeight
}
