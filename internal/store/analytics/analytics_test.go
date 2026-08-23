package analytics

import (
	"testing"
	"time"
)

func TestRecordEvent(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{
		Type:     "run_start",
		RunID:    "run_1",
		ThreadID: "thread_1",
		Agent:    "test-agent",
	})

	if store.Size() != 1 {
		t.Errorf("expected 1 event, got %d", store.Size())
	}
}

func TestDashboard(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	// Record some events.
	store.RecordEvent(Event{Type: "run_start", Agent: "agent-a", RunID: "r1"})
	store.RecordEvent(Event{Type: "run_end", Agent: "agent-a", RunID: "r1", TokensUsed: 100, DurationMs: 500})
	store.RecordEvent(Event{Type: "run_start", Agent: "agent-b", RunID: "r2"})
	store.RecordEvent(Event{Type: "run_end", Agent: "agent-b", RunID: "r2", TokensUsed: 200, DurationMs: 300})
	store.RecordEvent(Event{Type: "run_error", Agent: "agent-a", RunID: "r3"})

	dash := store.GetDashboard()

	if dash.TotalRuns != 2 {
		t.Errorf("expected 2 total runs, got %d", dash.TotalRuns)
	}
	if dash.TotalTokens != 300 {
		t.Errorf("expected 300 tokens, got %d", dash.TotalTokens)
	}
	if dash.RunsByAgent["agent-a"] != 1 {
		t.Errorf("expected 1 run for agent-a, got %d", dash.RunsByAgent["agent-a"])
	}
	if dash.RunsByStatus["error"] != 1 {
		t.Errorf("expected 1 error, got %d", dash.RunsByStatus["error"])
	}
	if dash.ErrorRate != 50.0 {
		t.Errorf("expected 50%% error rate, got %f", dash.ErrorRate)
	}
}

func TestQueryEvents(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{Type: "run_start", Agent: "agent-a", RunID: "r1", ThreadID: "t1"})
	store.RecordEvent(Event{Type: "run_end", Agent: "agent-a", RunID: "r1", ThreadID: "t1"})
	store.RecordEvent(Event{Type: "run_start", Agent: "agent-b", RunID: "r2", ThreadID: "t2"})
	store.RecordEvent(Event{Type: "run_end", Agent: "agent-b", RunID: "r2", ThreadID: "t2"})

	// Filter by agent.
	events := store.QueryEvents(EventQuery{Agent: "agent-a"})
	if len(events) != 2 {
		t.Errorf("expected 2 events for agent-a, got %d", len(events))
	}

	// Filter by thread.
	events = store.QueryEvents(EventQuery{ThreadID: "t2"})
	if len(events) != 2 {
		t.Errorf("expected 2 events for thread t2, got %d", len(events))
	}

	// Filter by type.
	events = store.QueryEvents(EventQuery{EventType: "run_end"})
	if len(events) != 2 {
		t.Errorf("expected 2 run_end events, got %d", len(events))
	}

	// Filter with limit.
	events = store.QueryEvents(EventQuery{Limit: 1})
	if len(events) != 1 {
		t.Errorf("expected 1 event with limit, got %d", len(events))
	}
}

func TestQueryEventsSince(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "old"})
	time.Sleep(10 * time.Millisecond)
	cutoff := time.Now().UTC()
	time.Sleep(10 * time.Millisecond)
	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "new"})

	events := store.QueryEvents(EventQuery{Since: cutoff})
	if len(events) != 1 {
		t.Errorf("expected 1 event since cutoff, got %d", len(events))
	}
	if events[0].RunID != "new" {
		t.Errorf("expected runID 'new', got '%s'", events[0].RunID)
	}
}

func TestPurgeOld(t *testing.T) {
	store := NewStore(Config{Enabled: true, RetentionDays: 0})

	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "r1", Timestamp: time.Now().UTC()})
	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "r2", Timestamp: time.Now().UTC()})

	purged := store.PurgeOld()
	if purged != 0 {
		t.Errorf("expected 0 purged, got %d", purged)
	}
	if store.Size() != 2 {
		t.Errorf("expected 2 events remaining, got %d", store.Size())
	}
}

func TestOTLPExport(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "r1"})

	events, err := store.OTLPExport("http://localhost:4318", time.Time{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 event, got %d", len(events))
	}
}

func TestHourlyBuckets(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{Type: "run_start", Agent: "a", RunID: "r1"})

	dash := store.GetDashboard()
	if len(dash.HourlyRuns) != 24 {
		t.Errorf("expected 24 hourly buckets, got %d", len(dash.HourlyRuns))
	}
}

func TestTotalTokensAndCost(t *testing.T) {
	store := NewStore(Config{Enabled: true})

	store.RecordEvent(Event{Type: "run_end", TokensUsed: 100, CostUSD: 0.001})
	store.RecordEvent(Event{Type: "run_end", TokensUsed: 200, CostUSD: 0.002})

	if store.GetTotalTokens() != 300 {
		t.Errorf("expected 300 total tokens, got %d", store.GetTotalTokens())
	}
	if store.GetTotalCost() != 0.003 {
		t.Errorf("expected $0.003 total cost, got %f", store.GetTotalCost())
	}
}
