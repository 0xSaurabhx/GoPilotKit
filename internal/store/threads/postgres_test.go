package threads

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// newTestPostgresStore creates a Postgres store for testing.
// Skips if DATABASE_URL is not set.
func newTestPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()
	connURI := os.Getenv("DATABASE_URL")
	if connURI == "" {
		t.Skip("DATABASE_URL not set, skipping Postgres tests")
	}

	// Use a unique schema per test to isolate tests.
	schemaName := "test_" + t.Name()
	store, err := NewPostgresStore(connURI)
	if err != nil {
		t.Fatalf("failed to create Postgres store: %v", err)
	}

	// Create a test schema and set search_path.
	ctx := t.Context()
	_, err = store.pool.Exec(ctx, "CREATE SCHEMA IF NOT EXISTS "+schemaName)
	if err != nil {
		store.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}
	_, err = store.pool.Exec(ctx, "SET search_path TO "+schemaName)
	if err != nil {
		store.Close()
		t.Fatalf("failed to set search_path: %v", err)
	}

	// Re-run migration in the test schema.
	if err := store.migrate(ctx); err != nil {
		store.Close()
		t.Fatalf("failed to migrate test schema: %v", err)
	}

	t.Cleanup(func() {
		store.pool.Exec(context.Background(), "DROP SCHEMA IF EXISTS "+schemaName+" CASCADE")
		store.Close()
	})

	return store
}

func TestPostgresCreateAndGetThread(t *testing.T) {
	store := newTestPostgresStore(t)

	thread := &Thread{
		ID:        "thread_001",
		Agent:     "test-agent",
		Title:     "Test Thread",
		State:     json.RawMessage(`{"key":"value"}`),
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	if err := store.CreateThread(thread); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.GetThread("thread_001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "thread_001" {
		t.Errorf("expected ID thread_001, got %s", got.ID)
	}
	if got.Agent != "test-agent" {
		t.Errorf("expected agent test-agent, got %s", got.Agent)
	}
	if got.Title != "Test Thread" {
		t.Errorf("expected title 'Test Thread', got %s", got.Title)
	}
}

func TestPostgresGetThreadNotFound(t *testing.T) {
	store := newTestPostgresStore(t)
	_, err := store.GetThread("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent thread")
	}
}

func TestPostgresUpdateThread(t *testing.T) {
	store := newTestPostgresStore(t)

	thread := &Thread{
		ID: "thread_001", Agent: "test-agent", Title: "Original",
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}
	store.CreateThread(thread)

	thread.Title = "Updated"
	thread.State = json.RawMessage(`{"updated":true}`)
	if err := store.UpdateThread(thread); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := store.GetThread("thread_001")
	if got.Title != "Updated" {
		t.Errorf("expected title 'Updated', got %s", got.Title)
	}
}

func TestPostgresDeleteThread(t *testing.T) {
	store := newTestPostgresStore(t)

	thread := &Thread{ID: "thread_001", Agent: "test-agent", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	store.CreateThread(thread)

	if err := store.DeleteThread("thread_001"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err := store.GetThread("thread_001")
	if err == nil {
		t.Fatal("expected error for deleted thread")
	}
}

func TestPostgresListThreads(t *testing.T) {
	store := newTestPostgresStore(t)

	for i := 0; i < 5; i++ {
		store.CreateThread(&Thread{
			ID: "thread_" + string(rune('0'+i)), Agent: "agent-" + string(rune('a'+i)),
			Title: "Thread " + string(rune('0'+i)), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		})
	}

	threads, total, err := store.ListThreads(ListThreadsParams{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 5 {
		t.Errorf("expected 5 threads, got %d", total)
	}
	if len(threads) != 5 {
		t.Errorf("expected 5 threads, got %d", len(threads))
	}
}

func TestPostgresAddAndGetMessages(t *testing.T) {
	store := newTestPostgresStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	msgs := []Message{
		{ID: "m1", ThreadID: "t1", Role: "user", Content: "Hello", CreatedAt: time.Now().UTC()},
		{ID: "m2", ThreadID: "t1", Role: "assistant", Content: "Hi there!", CreatedAt: time.Now().UTC()},
	}
	for _, m := range msgs {
		if err := store.AddMessage(&m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	got, err := store.GetMessages("t1", 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 messages, got %d", len(got))
	}
	if got[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got %s", got[0].Content)
	}
}

func TestPostgresCreateAndGetRun(t *testing.T) {
	store := newTestPostgresStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	now := time.Now().UTC()
	run := &Run{ID: "run_001", ThreadID: "t1", Agent: "test-agent", Status: "running", StartedAt: now}
	if err := store.CreateRun(run); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := store.GetRun("run_001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != "running" {
		t.Errorf("expected status 'running', got %s", got.Status)
	}
}

func TestPostgresUpdateRun(t *testing.T) {
	store := newTestPostgresStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	now := time.Now().UTC()
	store.CreateRun(&Run{ID: "run_001", ThreadID: "t1", Agent: "a", Status: "running", StartedAt: now})

	endTime := time.Now().UTC()
	store.UpdateRun(&Run{ID: "run_001", ThreadID: "t1", Agent: "a", Status: "completed", EndedAt: &endTime})

	got, _ := store.GetRun("run_001")
	if got.Status != "completed" {
		t.Errorf("expected status 'completed', got %s", got.Status)
	}
	if got.EndedAt == nil {
		t.Error("expected ended_at to be set")
	}
}

func TestPostgresThreadCascadeDelete(t *testing.T) {
	store := newTestPostgresStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	store.AddMessage(&Message{ID: "m1", ThreadID: "t1", Role: "user", Content: "hi", CreatedAt: time.Now().UTC()})
	store.CreateRun(&Run{ID: "r1", ThreadID: "t1", Agent: "a", Status: "running", StartedAt: time.Now().UTC()})

	store.DeleteThread("t1")

	msgs, _ := store.GetMessages("t1", 10)
	if len(msgs) != 0 {
		t.Error("expected messages to be cascade deleted")
	}

	_, err := store.GetRun("r1")
	if err == nil {
		t.Error("expected run to be cascade deleted")
	}
}
