package runtime

import (
	"encoding/json"
	"log"

	"github.com/0xsaurabhx/GoPilotKit/internal/learning/containers"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/evaluation"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/fewshot"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/memory"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/prompts"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/signals"
	"github.com/0xsaurabhx/GoPilotKit/internal/learning/skills"
	"github.com/0xsaurabhx/GoPilotKit/internal/store/analytics"
)

// LearningPipeline orchestrates the autonomous self-improvement subsystems.
// It hooks into the run lifecycle to extract signals, evaluate quality,
// evolve prompts, curate few-shot examples, and manage memory.
type LearningPipeline struct {
	Enabled      bool
	Signals      *signals.Extractor
	Evaluation   *evaluation.Engine
	Prompts      *prompts.Engine
	FewShot      *fewshot.Engine
	Memory       *memory.Engine
	Skills       *skills.Engine
	Containers   *containers.Engine
	Analytics    *analytics.Store
}

// NewLearningPipeline creates a learning pipeline from the engine's configuration.
func NewLearningPipeline(analyticsStore *analytics.Store) *LearningPipeline {
	return &LearningPipeline{
		Enabled:    analyticsStore != nil,
		Signals:    signals.NewExtractor(),
		Evaluation: evaluation.NewEngine(evaluation.DefaultEvalConfig()),
		Prompts:    prompts.NewEngine(),
		FewShot:    fewshot.NewEngine(fewshot.Config{MaxExamples: 100, RetrievalK: 3, MinScore: 0.3}),
		Memory:     memory.NewEngine(memory.DefaultMemoryConfig()),
		Skills:     skills.NewEngine(),
		Containers: containers.NewEngine(),
		Analytics:  analyticsStore,
	}
}

// RunStarted records a run_start analytics event.
func (lp *LearningPipeline) RunStarted(runID, threadID, agent string) {
	if lp.Analytics == nil {
		return
	}
	lp.Analytics.RecordEvent(analytics.Event{
		Type:     "run_start",
		RunID:    runID,
		ThreadID: threadID,
		Agent:    agent,
	})
}

// RunCompleted records a run_end event, evaluates quality, and learns from success.
func (lp *LearningPipeline) RunCompleted(runID, threadID, agent, query, response string, toolUsed bool, tokensUsed int, costUSD float64, durationMs int64) {
	if lp.Analytics == nil {
		return
	}

	// Record analytics event.
	lp.Analytics.RecordEvent(analytics.Event{
		Type:       "run_end",
		RunID:      runID,
		ThreadID:   threadID,
		Agent:      agent,
		TokensUsed: tokensUsed,
		CostUSD:    costUSD,
		DurationMs: durationMs,
	})

	// Evaluate response quality.
	evalResult := lp.Evaluation.Evaluate(runID, agent, query, response, toolUsed)

	if evalResult.Passed {
		// Success — curate as few-shot example and store memory.
		lp.FewShot.AddExample(agent, query, response, evalResult.Score)
		lp.Memory.Store(agent, response, "interaction", runID, []string{agent})
		log.Printf("learning: run %s passed evaluation (score=%.2f), stored as few-shot example", runID, evalResult.Score)
	} else {
		// Failure — evolve prompt if regression detected.
		if evalResult.Regression {
			failures := []prompts.FailurePattern{
				{Type: "incomplete_response", Count: 1, RunIDs: []string{runID}},
			}
			newVersion, changeLog := lp.Prompts.Evolve(agent, failures)
			if newVersion != nil {
				log.Printf("learning: prompt evolved for agent %q: %s", agent, changeLog.Reason)
			}
		}
		log.Printf("learning: run %s failed evaluation (score=%.2f, regression=%v)", runID, evalResult.Score, evalResult.Regression)
	}
}

// RunFailed records a run_error event and extracts negative signals.
func (lp *LearningPipeline) RunFailed(runID, threadID, agent, errMsg string) {
	if lp.Analytics == nil {
		return
	}

	lp.Analytics.RecordEvent(analytics.Event{
		Type:     "run_error",
		RunID:    runID,
		ThreadID: threadID,
		Agent:    agent,
		Payload:  json.RawMessage(`{"error":"` + errMsg + `"}`),
	})

	// Extract negative signal from error.
	lp.Signals.ExtractFromFeedback(runID, threadID, agent, "error: "+errMsg)
}

// ToolCall records a tool_call analytics event.
func (lp *LearningPipeline) ToolCall(runID, threadID, agent, toolName string) {
	if lp.Analytics == nil {
		return
	}

	lp.Analytics.RecordEvent(analytics.Event{
		Type:     "tool_call",
		RunID:    runID,
		ThreadID: threadID,
		Agent:    agent,
		Payload:  json.RawMessage(`{"tool":"` + toolName + `"}`),
	})
}

// ProcessFeedback extracts signals from user feedback and learns from it.
func (lp *LearningPipeline) ProcessFeedback(runID, threadID, agent, feedback string) {
	signal := lp.Signals.ExtractFromFeedback(runID, threadID, agent, feedback)
	if signal != nil {
		log.Printf("learning: extracted signal type=%s value=%.2f from feedback", signal.Type, signal.Value)

		// Store feedback as memory.
		lp.Memory.Store(agent, feedback, "preference", "user_feedback:"+string(signal.Type), []string{string(signal.Type)})
	}
}

// GetPrompt returns the current evolved prompt for an agent, or empty if none.
func (lp *LearningPipeline) GetPrompt(agent string) string {
	if lp.Prompts == nil {
		return ""
	}
	prompt, ok := lp.Prompts.GetCurrentPrompt(agent)
	if !ok {
		return ""
	}
	return prompt
}

// InitAgentPrompt registers an agent's initial prompt for evolution tracking.
func (lp *LearningPipeline) InitAgentPrompt(agent, systemPrompt string) {
	if lp.Prompts != nil {
		lp.Prompts.SetBasePrompt(agent, systemPrompt)
	}
}

// DecayMemory runs memory decay and consolidation. Call periodically.
func (lp *LearningPipeline) DecayMemory() {
	if lp.Memory != nil {
		pruned := lp.Memory.Decay()
		if pruned > 0 {
			log.Printf("learning: memory decay pruned %d entries", pruned)
		}
		consolidated := lp.Memory.Consolidate("*")
		if consolidated > 0 {
			log.Printf("learning: memory consolidation merged %d entries", consolidated)
		}
	}
}

// PurgeOldEvents runs analytics event retention cleanup.
func (lp *LearningPipeline) PurgeOldEvents() {
	if lp.Analytics != nil {
		purged := lp.Analytics.PurgeOld()
		if purged > 0 {
			log.Printf("analytics: purged %d old events", purged)
		}
	}
}
