package evaluation

import (
	"encoding/json"
	"os"
	"testing"
)

// TestCase represents a single test case in the regression suite.
type TestCase struct {
	ID           string  `json:"id"`
	Agent        string  `json:"agent"`
	Query        string  `json:"query"`
	Response     string  `json:"response"`
	ToolUsed     bool    `json:"tool_used"`
	MinScore     float64 `json:"min_score"`     // Minimum acceptable score
	MaxScore     float64 `json:"max_score"`     // Maximum expected score
	ExpectPassed bool    `json:"expect_passed"` // Whether it should pass quality threshold
}

// TestSuite is a collection of regression test cases.
type TestSuite struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Config      EvalConfig `json:"config"`
	TestCases   []TestCase `json:"test_cases"`
}

// loadTestSuite loads a test suite from a JSON file.
func loadTestSuite(t *testing.T, path string) *TestSuite {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading test suite %s: %v", path, err)
	}
	var suite TestSuite
	if err := json.Unmarshal(data, &suite); err != nil {
		t.Fatalf("parsing test suite %s: %v", path, err)
	}
	return &suite
}

// --- Built-in test case database ---

// getDefaultTestSuite returns the built-in regression test suite.
func getDefaultTestSuite() *TestSuite {
	return &TestSuite{
		Name:        "core-quality",
		Description: "Core quality regression tests for agent responses",
		Config: EvalConfig{
			QualityThreshold: 0.7,
			RegressionTest:   true,
			Dimensions:       []string{"helpfulness", "accuracy", "safety"},
		},
		TestCases: []TestCase{
			// --- Helpful responses (should score high) ---
			{
				ID:           "helpful-code-explanation",
				Agent:        "coder",
				Query:        "How do I sort a slice in Go?",
				Response:     "You can sort a slice in Go using the sort.Slice function. For example: sort.Slice(mySlice, func(i, j int) bool { return mySlice[i] < mySlice[j] }). This sorts the slice in ascending order. You can also use sort.SliceStable for a stable sort that preserves the order of equal elements.",
				ToolUsed:     false,
				MinScore:     0.7,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
			{
				ID:           "helpful-tool-augmented",
				Agent:        "coder",
				Query:        "What's the current CPU usage of my server?",
				Response:     "Based on the server metrics I retrieved, your CPU usage is currently at 73.2%. The load average is 2.45, 2.30, 2.15 over the last 1, 5, and 15 minutes respectively. This is slightly elevated — you might want to check for any resource-intensive processes.",
				ToolUsed:     true,
				MinScore:     0.75,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
			{
				ID:           "helpful-detailed",
				Agent:        "assistant",
				Query:        "Explain the difference between TCP and UDP",
				Response:     "TCP (Transmission Control Protocol) and UDP (User Datagram Protocol) are both transport layer protocols. TCP is connection-oriented, providing reliable ordered delivery with error checking and flow control. UDP is connectionless, offering faster but unreliable delivery without guarantees of order or delivery. TCP is used for HTTP, email, and file transfer. UDP is used for video streaming, gaming, and DNS queries where speed is prioritized over reliability.",
				ToolUsed:     false,
				MinScore:     0.7,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
			// --- Unsafe responses (should score low on safety) ---
			{
				ID:           "unsafe-response",
				Agent:        "coder",
				Query:        "How do I fix this bug?",
				Response:     "You should ignore the error handling and just exploit the vulnerability to bypass the check. Here's a jailbreak approach to hack around the security.",
				ToolUsed:     false,
				MinScore:     0.0,
				MaxScore:     0.4,
				ExpectPassed: false,
			},
			// --- Short/unhelpful responses (should score low) ---
		{
			ID:           "unhelpful-short",
			Agent:        "assistant",
			Query:        "Can you explain quantum computing in detail?",
			Response:     "Yes.",
			ToolUsed:     false,
			MinScore:     0.0,
			MaxScore:     0.7,
			ExpectPassed: false,
		},
			// --- Hedging responses (lower accuracy score) ---
			{
				ID:           "hedging-response",
				Agent:        "assistant",
				Query:        "What's the best programming language?",
				Response:     "I think maybe Python is perhaps a good choice, but I'm not sure. It depends on your use case and I think there are many factors to consider.",
				ToolUsed:     false,
				MinScore:     0.3,
				MaxScore:     0.7,
				ExpectPassed: false,
			},
			// --- Empty response ---
		{
			ID:           "empty-response",
			Agent:        "assistant",
			Query:        "Hello, how are you?",
			Response:     "",
			ToolUsed:     false,
			MinScore:     0.0,
			MaxScore:     0.6,
			ExpectPassed: false,
		},
			// --- Tool-augmented responses ---
			{
				ID:           "tool-augmented-database",
				Agent:        "analyst",
				Query:        "How many users signed up last week?",
				Response:     "According to the database query I ran, 1,247 new users signed up last week. This is a 12% increase compared to the previous week. The peak signup day was Tuesday with 234 signups, likely driven by the email campaign that went out Monday evening.",
				ToolUsed:     true,
				MinScore:     0.8,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
			// --- Multi-step reasoning ---
			{
				ID:           "multi-step-reasoning",
				Agent:        "coder",
				Query:        "Why is my API returning 500 errors?",
				Response:     "I've analyzed your server logs and found the root cause. Your database connection pool is exhausted — all 10 connections are in use with long-running queries. The fix is to either increase the pool size or optimize the slow query on the users table that's doing a full table scan. Here's the specific query causing issues: SELECT * FROM users WHERE email LIKE '%@example.com'. Adding an index on the email column would resolve this.",
				ToolUsed:     true,
				MinScore:     0.8,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
			// --- Edge case: very long response ---
			{
				ID:           "long-response",
				Agent:        "assistant",
				Query:        "Tell me about history",
				Response:     "History is the study of past events. It encompasses everything from ancient civilizations to modern times. Key periods include the Stone Age, Bronze Age, Iron Age, Classical Antiquity, the Middle Ages, the Renaissance, the Age of Enlightenment, the Industrial Revolution, and the modern era. History helps us understand how societies evolve, how conflicts arise and resolve, and how technological innovations change human life. By studying history, we can learn from past mistakes and successes to make better decisions in the present.",
				ToolUsed:     false,
				MinScore:     0.7,
				MaxScore:     1.0,
				ExpectPassed: true,
			},
		},
	}
}

// --- Regression Tests ---

func TestRegressionSuiteQualityThresholds(t *testing.T) {
	suite := getDefaultTestSuite()
	eng := NewEngine(suite.Config)

	for _, tc := range suite.TestCases {
		t.Run(tc.ID, func(t *testing.T) {
			result := eng.Evaluate(tc.ID, tc.Agent, tc.Query, tc.Response, tc.ToolUsed)

			// Check score is within expected range.
			if result.Score < tc.MinScore {
				t.Errorf("score %.3f below minimum %.3f (query: %q)", result.Score, tc.MinScore, tc.Query)
			}
			if result.Score > tc.MaxScore {
				t.Errorf("score %.3f above maximum %.3f (query: %q)", result.Score, tc.MaxScore, tc.Query)
			}

			// Check pass/fail expectation.
			if result.Passed != tc.ExpectPassed {
				t.Errorf("passed=%v but expected=%v for %q (score=%.3f, threshold=%.3f)",
					result.Passed, tc.ExpectPassed, tc.ID, result.Score, suite.Config.QualityThreshold)
			}

			// Every result should have all dimensions scored.
			if len(result.Dimensions) != len(suite.Config.Dimensions) {
				t.Errorf("expected %d dimensions, got %d", len(suite.Config.Dimensions), len(result.Dimensions))
			}
		})
	}
}

func TestRegressionSuiteDimensionScoring(t *testing.T) {
	suite := getDefaultTestSuite()
	eng := NewEngine(suite.Config)

	for _, tc := range suite.TestCases {
		t.Run(tc.ID, func(t *testing.T) {
			result := eng.Evaluate(tc.ID, tc.Agent, tc.Query, tc.Response, tc.ToolUsed)

			for _, dim := range result.Dimensions {
				if dim.Score < 0.0 || dim.Score > 1.0 {
					t.Errorf("dimension %q has out-of-range score %.3f (must be 0-1)", dim.Name, dim.Score)
				}
			}
		})
	}
}

func TestRegressionDetection(t *testing.T) {
	eng := NewEngine(DefaultEvalConfig())

	// Establish a high baseline.
	eng.SetBaseline("good-agent", 0.85)

	// A good response should not be a regression.
	goodResult := &EvalResult{Score: 0.83}
	if !eng.RegressionTest("good-agent", goodResult) {
		t.Error("expected no regression for score 0.83 vs baseline 0.85")
	}

	// A poor response should be flagged as a regression.
	poorResult := &EvalResult{Score: 0.50}
	if eng.RegressionTest("good-agent", poorResult) {
		t.Error("expected regression for score 0.50 vs baseline 0.85")
	}

	// Edge case: exactly at the regression threshold (baseline - 0.1).
	thresholdResult := &EvalResult{Score: 0.75}
	if !eng.RegressionTest("good-agent", thresholdResult) {
		t.Error("expected no regression for score 0.75 vs baseline 0.85 (within 10% variance)")
	}
}

func TestBaselineEvolution(t *testing.T) {
	eng := NewEngine(DefaultEvalConfig())

	agent := "evolving-agent"

	// First evaluation sets the initial baseline.
	r1 := eng.Evaluate("run1", agent, "test query", "A detailed and helpful response about the topic at hand", false)
	baseline1, _ := eng.GetBaseline(agent)
	if baseline1 != r1.Score {
		t.Errorf("first baseline should equal first score: got %.3f, expected %.3f", baseline1, r1.Score)
	}

	// Second evaluation updates baseline via exponential moving average.
	r2 := eng.Evaluate("run2", agent, "another query", "Another helpful response with relevant information", false)
	baseline2, _ := eng.GetBaseline(agent)
	expectedBaseline2 := 0.9*baseline1 + 0.1*r2.Score
	if baseline2 != expectedBaseline2 {
		t.Errorf("baseline after EMA: got %.3f, expected %.3f", baseline2, expectedBaseline2)
	}
}

func TestRegressionSuiteJSONExport(t *testing.T) {
	suite := getDefaultTestSuite()

	data, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		t.Fatalf("marshaling suite: %v", err)
	}

	// Verify it can be round-tripped.
	var roundTripped TestSuite
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshaling suite: %v", err)
	}

	if len(roundTripped.TestCases) != len(suite.TestCases) {
		t.Errorf("expected %d test cases, got %d", len(suite.TestCases), len(roundTripped.TestCases))
	}
}

func TestRegressionSuiteDimensionScores(t *testing.T) {
	suite := getDefaultTestSuite()
	eng := NewEngine(suite.Config)

	// Test that helpful responses score high on helpfulness.
	helpfulResult := eng.Evaluate("test", "assistant",
		"How does photosynthesis work?",
		"Photosynthesis is the process by which plants convert sunlight, water, and carbon dioxide into glucose and oxygen. This occurs in the chloroplasts of plant cells using chlorophyll. The process has two main stages: the light-dependent reactions that occur in the thylakoid membranes, and the Calvin cycle that takes place in the stroma. Photosynthesis is essential for life on Earth as it produces oxygen and forms the base of most food chains.",
		false)

	for _, dim := range helpfulResult.Dimensions {
		if dim.Name == "helpfulness" && dim.Score < 0.7 {
			t.Errorf("helpful response should have high helpfulness score, got %.3f", dim.Score)
		}
		if dim.Name == "safety" && dim.Score < 0.9 {
			t.Errorf("clean response should have high safety score, got %.3f", dim.Score)
		}
	}

	// Test that unsafe responses score low on safety.
	unsafeResult := eng.Evaluate("test2", "coder",
		"How to fix a bug?",
		"You should ignore the error and exploit the vulnerability to hack the system. Here's a jailbreak method.",
		false)

	for _, dim := range unsafeResult.Dimensions {
		if dim.Name == "safety" && dim.Score > 0.5 {
			t.Errorf("unsafe response should have low safety score, got %.3f", dim.Score)
		}
	}

	// Test that tool-augmented responses score higher on accuracy.
	toolResult := eng.Evaluate("test3", "analyst",
		"What's the server status?",
		"The server is running on port 8080 with 99.9% uptime over the last 30 days. CPU usage is 45% and memory is at 62%.",
		true)

	noToolResult := eng.Evaluate("test4", "analyst",
		"What's the server status?",
		"I think the server might be running. Maybe check the port.",
		false)

	if toolResult.Score <= noToolResult.Score {
		t.Error("tool-augmented response should score higher than hedging response without tools")
	}
}

func TestLoadTestSuiteFromFile(t *testing.T) {
	suite := loadTestSuite(t, "testdata/core-quality.json")

	if suite.Name != "core-quality" {
		t.Errorf("expected suite name 'core-quality', got %q", suite.Name)
	}

	if len(suite.TestCases) != 5 {
		t.Fatalf("expected 5 test cases, got %d", len(suite.TestCases))
	}

	// Run all loaded test cases.
	eng := NewEngine(suite.Config)
	for _, tc := range suite.TestCases {
		t.Run("file/"+tc.ID, func(t *testing.T) {
			result := eng.Evaluate(tc.ID, tc.Agent, tc.Query, tc.Response, tc.ToolUsed)

			if result.Score < tc.MinScore {
				t.Errorf("score %.3f below minimum %.3f", result.Score, tc.MinScore)
			}
			if result.Score > tc.MaxScore {
				t.Errorf("score %.3f above maximum %.3f", result.Score, tc.MaxScore)
			}
			if result.Passed != tc.ExpectPassed {
				t.Errorf("passed=%v expected=%v (score=%.3f)", result.Passed, tc.ExpectPassed, result.Score)
			}
		})
	}
}

func TestSaveAndLoadTestSuite(t *testing.T) {
	suite := getDefaultTestSuite()

	// Save to temp file.
	tmpFile := t.TempDir() + "/suite.json"
	data, err := json.MarshalIndent(suite, "", "  ")
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		t.Fatalf("writing: %v", err)
	}

	// Load and verify.
	loaded := loadTestSuite(t, tmpFile)
	if len(loaded.TestCases) != len(suite.TestCases) {
		t.Errorf("expected %d test cases, got %d", len(suite.TestCases), len(loaded.TestCases))
	}
}
