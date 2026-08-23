package cost

import (
	"testing"
	"time"
)

func TestCalculateCost(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	// OpenAI GPT-4o pricing
	cost := tracker.CalculateCost("openai", "gpt-4o", 1000, 500, 0)
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
	// 1000 prompt * $0.0000025 + 500 completion * $0.00001 = $0.0075
	expected := 0.0075
	if cost != expected {
		t.Errorf("expected cost $%.6f, got $%.6f", expected, cost)
	}
}

func TestCalculateCostUnknownProvider(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	cost := tracker.CalculateCost("unknown", "model", 1000, 500, 0)
	if cost <= 0 {
		t.Errorf("expected positive cost for unknown provider, got %f", cost)
	}
}

func TestRecordUsage(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	err := tracker.RecordUsage(UsageRecord{
		RequestID:        "req_1",
		Agent:            "test-agent",
		ProviderID:       "openai",
		Model:            "gpt-4o",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		Timestamp:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tracker.GetDailyTotal() <= 0 {
		t.Error("expected positive daily total")
	}
}

func TestBudgetDailyLimit(t *testing.T) {
	tracker := NewTracker(BudgetConfig{
		MaxDailyUSD: 0.005, // Very tight limit
	})

	// First usage with high cost.
	err := tracker.RecordUsage(UsageRecord{
		RequestID:        "req_1",
		Agent:            "test",
		ProviderID:       "openai",
		Model:            "gpt-4o",
		PromptTokens:     1000,
		CompletionTokens: 1000,
		TotalTokens:      2000,
		CostUSD:          0.004, // $0.004
		Timestamp:        time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("first usage should pass: %v", err)
	}

	// Second usage should exceed the daily limit.
	err = tracker.RecordUsage(UsageRecord{
		RequestID:   "req_2",
		Agent:       "test",
		ProviderID:  "openai",
		Model:       "gpt-4o",
		TotalTokens: 1000,
		CostUSD:     0.003, // $0.003, total = $0.007 > $0.005
		Timestamp:   time.Now().UTC(),
	})
	if err == nil {
		t.Error("expected budget exceeded error")
	}
}

func TestRateLimit(t *testing.T) {
	tracker := NewTracker(BudgetConfig{
		RateLimitRPM: 2, // 2 requests per minute
	})

	// First two should pass.
	for i := 0; i < 2; i++ {
		err := tracker.RecordUsage(UsageRecord{
			RequestID:   "req_" + string(rune('0'+i)),
			Agent:       "test",
			ProviderID:  "openai",
			Model:       "gpt-4o",
			TotalTokens: 10,
			Timestamp:   time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("request %d should pass: %v", i, err)
		}
	}

	// Third should fail.
	err := tracker.RecordUsage(UsageRecord{
		RequestID:   "req_3",
		Agent:       "test",
		ProviderID:  "openai",
		Model:       "gpt-4o",
		TotalTokens: 10,
		Timestamp:   time.Now().UTC(),
	})
	if err == nil {
		t.Error("expected rate limit error")
	}
}

func TestBudgetStatus(t *testing.T) {
	tracker := NewTracker(BudgetConfig{
		MaxDailyUSD:   10.0,
		MaxMonthlyUSD: 100.0,
	})

	_ = tracker.RecordUsage(UsageRecord{
		RequestID:   "req_1",
		Agent:       "test",
		ProviderID:  "openai",
		Model:       "gpt-4o",
		TotalTokens: 1000,
		Timestamp:   time.Now().UTC(),
	})

	status := tracker.GetBudgetStatus()
	if _, ok := status["daily_used"]; !ok {
		t.Error("expected daily_used in status")
	}
	if _, ok := status["monthly_used"]; !ok {
		t.Error("expected monthly_used in status")
	}
}

func TestSummary(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	for i := 0; i < 5; i++ {
		_ = tracker.RecordUsage(UsageRecord{
			RequestID:       "req_" + string(rune('0'+i)),
			Agent:           "test",
			ProviderID:      "openai",
			Model:           "gpt-4o",
			PromptTokens:    100,
			CompletionTokens: 50,
			TotalTokens:     150,
			Timestamp:       time.Now().UTC(),
		})
	}

	summary := tracker.GetSummary()
	if summary.TotalRequests != 5 {
		t.Errorf("expected 5 requests, got %d", summary.TotalRequests)
	}
	if summary.TotalTokens != 750 {
		t.Errorf("expected 750 tokens, got %d", summary.TotalTokens)
	}
	if summary.AvgTokensPerReq != 150 {
		t.Errorf("expected 150 avg tokens, got %f", summary.AvgTokensPerReq)
	}
}

func TestGetRecords(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	for i := 0; i < 10; i++ {
		_ = tracker.RecordUsage(UsageRecord{
			RequestID:   "req_" + string(rune('0'+i)),
			Agent:       "test",
			ProviderID:  "openai",
			Model:       "gpt-4o",
			TotalTokens: 100,
			Timestamp:   time.Now().UTC(),
		})
	}

	records := tracker.GetRecords(5)
	if len(records) != 5 {
		t.Errorf("expected 5 records, got %d", len(records))
	}

	allRecords := tracker.GetRecords(0)
	if len(allRecords) != 10 {
		t.Errorf("expected 10 records, got %d", len(allRecords))
	}
}

func TestSetPricing(t *testing.T) {
	tracker := NewTracker(BudgetConfig{})

	tracker.SetPricing(ProviderPricing{
		Provider:           "custom",
		Model:              "custom-model",
		PromptPerToken:     0.001,
		CompletionPerToken: 0.002,
	})

	cost := tracker.CalculateCost("custom", "custom-model", 100, 50, 0)
	expected := 0.001*100 + 0.002*50 // = 0.2
	if cost != expected {
		t.Errorf("expected cost $%.4f, got $%.4f", expected, cost)
	}
}
