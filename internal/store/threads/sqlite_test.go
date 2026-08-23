package threads

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	tmpFile := t.TempDir() + "/test.db"
	store, err := NewSQLiteStore(tmpFile)
	if err != nil {
		t.Fatalf("failed to create test store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestCreateAndGetThread(t *testing.T) {
	store := newTestStore(t)

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

func TestGetThreadNotFound(t *testing.T) {
	store := newTestStore(t)

	_, err := store.GetThread("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent thread")
	}
}

func TestUpdateThread(t *testing.T) {
	store := newTestStore(t)

	thread := &Thread{
		ID:        "thread_001",
		Agent:     "test-agent",
		Title:     "Original",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
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

func TestDeleteThread(t *testing.T) {
	store := newTestStore(t)

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

func TestDeleteThreadNotFound(t *testing.T) {
	store := newTestStore(t)

	err := store.DeleteThread("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent thread")
	}
}

func TestListThreads(t *testing.T) {
	store := newTestStore(t)

	for i := 0; i < 5; i++ {
		store.CreateThread(&Thread{
			ID:        "thread_" + string(rune('0'+i)),
			Agent:     "agent-" + string(rune('a'+i)),
			Title:     "Thread " + string(rune('0'+i)),
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
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

func TestListThreadsFilteredByAgent(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "agent-a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	store.CreateThread(&Thread{ID: "t2", Agent: "agent-b", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})
	store.CreateThread(&Thread{ID: "t3", Agent: "agent-a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	threads, total, _ := store.ListThreads(ListThreadsParams{Agent: "agent-a"})
	if total != 2 {
		t.Errorf("expected 2 threads for agent-a, got %d", total)
	}
	if len(threads) != 2 {
		t.Errorf("expected 2 threads, got %d", len(threads))
	}
}

func TestRenameThread(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", Title: "Old", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	if err := store.RenameThread("t1", "New Title"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, _ := store.GetThread("t1")
	if got.Title != "New Title" {
		t.Errorf("expected 'New Title', got %s", got.Title)
	}
}

func TestArchiveThread(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	if err := store.ArchiveThread("t1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should not appear in non-archived list
	threads, total, _ := store.ListThreads(ListThreadsParams{})
	if total != 0 {
		t.Errorf("expected 0 non-archived threads, got %d", total)
	}
	_ = threads

	// Should appear in archived list
	archived, total, _ := store.ListThreads(ListThreadsParams{Archived: true})
	if total != 1 {
		t.Errorf("expected 1 archived thread, got %d", total)
	}
	if len(archived) != 1 || archived[0].ID != "t1" {
		t.Error("expected t1 in archived list")
	}
}

func TestAddAndGetMessages(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	msgs := []Message{
		{ID: "m1", ThreadID: "t1", Role: "user", Content: "Hello", CreatedAt: time.Now().UTC()},
		{ID: "m2", ThreadID: "t1", Role: "assistant", Content: "Hi there!", CreatedAt: time.Now().UTC()},
		{ID: "m3", ThreadID: "t1", Role: "user", Content: "How are you?", CreatedAt: time.Now().UTC()},
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
	if len(got) != 3 {
		t.Errorf("expected 3 messages, got %d", len(got))
	}
	if got[0].Content != "Hello" {
		t.Errorf("expected 'Hello', got %s", got[0].Content)
	}
	if got[1].Content != "Hi there!" {
		t.Errorf("expected 'Hi there!', got %s", got[1].Content)
	}
}

func TestGetMessagesLimit(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	for i := 0; i < 10; i++ {
		store.AddMessage(&Message{
			ID:        "m" + string(rune('0'+i)),
			ThreadID:  "t1",
			Role:      "user",
			Content:   "msg",
			CreatedAt: time.Now().UTC(),
		})
	}

	got, _ := store.GetMessages("t1", 3)
	if len(got) != 3 {
		t.Errorf("expected 3 messages with limit, got %d", len(got))
	}
}

func TestCreateAndGetRun(t *testing.T) {
	store := newTestStore(t)

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	now := time.Now().UTC()
	run := &Run{
		ID:        "run_001",
		ThreadID:  "t1",
		Agent:     "test-agent",
		Status:    "running",
		StartedAt: now,
	}

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

func TestUpdateRun(t *testing.T) {
	store := newTestStore(t)

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

func TestThreadCascadeDelete(t *testing.T) {
	store := newTestStore(t)

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

// Benchmark
func BenchmarkCreateThread(b *testing.B) {
	tmpFile := b.TempDir() + "/bench.db"
	store, _ := NewSQLiteStore(tmpFile)
	defer store.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.CreateThread(&Thread{
			ID:        "bench_" + string(rune(i)),
			Agent:     "bench-agent",
			Title:     "Bench",
			CreatedAt: time.Now().UTC(),
			UpdatedAt: time.Now().UTC(),
		})
	}
}

func BenchmarkAddMessage(b *testing.B) {
	tmpFile := b.TempDir() + "/bench.db"
	store, _ := NewSQLiteStore(tmpFile)
	defer store.Close()

	store.CreateThread(&Thread{ID: "t1", Agent: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.AddMessage(&Message{
			ID:        "bench_" + string(rune(i)),
			ThreadID:  "t1",
			Role:      "user",
			Content:   "benchmark message",
			CreatedAt: time.Now().UTC(),
		})
	}
}

// Suppress unused import
var _ = os.Getenv
