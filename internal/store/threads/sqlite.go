package threads

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using SQLite with WAL mode.
type SQLiteStore struct {
	db *sql.DB
}

// NewSQLiteStore creates a new SQLite-backed thread store.
// Enables WAL mode and busy_timeout for safe concurrent access.
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite: opening database: %w", err)
	}

	// Enable WAL mode for concurrent reads/writes
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: enabling WAL mode: %w", err)
	}

	// Set busy timeout to 5 seconds
	if _, err := db.Exec("PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: setting busy timeout: %w", err)
	}

	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: enabling foreign keys: %w", err)
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("sqlite: migration failed: %w", err)
	}

	return store, nil
}

// migrate creates the database schema.
func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		agent TEXT NOT NULL,
		title TEXT DEFAULT '',
		state TEXT DEFAULT '{}',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		archived INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_threads_agent ON threads(agent);
	CREATE INDEX IF NOT EXISTS idx_threads_archived ON threads(archived);
	CREATE INDEX IF NOT EXISTS idx_threads_updated ON threads(updated_at DESC);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT DEFAULT '',
		events TEXT DEFAULT '[]',
		metadata TEXT DEFAULT '{}',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id, created_at);

	CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL,
		agent TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		state TEXT DEFAULT '{}',
		error TEXT DEFAULT '',
		started_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		ended_at DATETIME,
		FOREIGN KEY (thread_id) REFERENCES threads(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_runs_thread ON runs(thread_id);
	`

	_, err := s.db.Exec(schema)
	return err
}

func (s *SQLiteStore) CreateThread(thread *Thread) error {
	stateJSON, _ := json.Marshal(thread.State)
	_, err := s.db.Exec(
		"INSERT INTO threads (id, agent, title, state, created_at, updated_at, archived) VALUES (?, ?, ?, ?, ?, ?, ?)",
		thread.ID, thread.Agent, thread.Title, string(stateJSON), thread.CreatedAt, thread.UpdatedAt, 0,
	)
	return err
}

func (s *SQLiteStore) GetThread(id string) (*Thread, error) {
	var t Thread
	var stateStr string
	var archived int
	err := s.db.QueryRow(
		"SELECT id, agent, title, state, created_at, updated_at, archived FROM threads WHERE id = ?", id,
	).Scan(&t.ID, &t.Agent, &t.Title, &stateStr, &t.CreatedAt, &t.UpdatedAt, &archived)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("thread %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	t.Archived = archived == 1
	json.Unmarshal([]byte(stateStr), &t.State)
	return &t, nil
}

func (s *SQLiteStore) UpdateThread(thread *Thread) error {
	stateJSON, _ := json.Marshal(thread.State)
	thread.UpdatedAt = time.Now().UTC()
	result, err := s.db.Exec(
		"UPDATE threads SET title = ?, state = ?, updated_at = ? WHERE id = ?",
		thread.Title, string(stateJSON), thread.UpdatedAt, thread.ID,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("thread %s not found", thread.ID)
	}
	return nil
}

func (s *SQLiteStore) DeleteThread(id string) error {
	result, err := s.db.Exec("DELETE FROM threads WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *SQLiteStore) ListThreads(params ListThreadsParams) ([]Thread, int, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}

	// Count total
	countQuery := "SELECT COUNT(*) FROM threads WHERE archived = ?"
	countArgs := []interface{}{0}
	if params.Agent != "" {
		countQuery += " AND agent = ?"
		countArgs = append(countArgs, params.Agent)
	}
	if params.Archived {
		countQuery = "SELECT COUNT(*) FROM threads WHERE archived = 1"
		countArgs = []interface{}{}
		if params.Agent != "" {
			countQuery += " AND agent = ?"
			countArgs = append(countArgs, params.Agent)
		}
	}

	var total int
	if err := s.db.QueryRow(countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Fetch threads
	query := "SELECT id, agent, title, state, created_at, updated_at, archived FROM threads WHERE archived = ?"
	args := []interface{}{0}
	if params.Agent != "" {
		query += " AND agent = ?"
		args = append(args, params.Agent)
	}
	if params.Archived {
		query = "SELECT id, agent, title, state, created_at, updated_at, archived FROM threads WHERE archived = 1"
		args = []interface{}{}
		if params.Agent != "" {
			query += " AND agent = ?"
			args = append(args, params.Agent)
		}
	}
	query += " ORDER BY updated_at DESC LIMIT ? OFFSET ?"
	args = append(args, params.Limit, params.Offset)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var t Thread
		var stateStr string
		var archived int
		if err := rows.Scan(&t.ID, &t.Agent, &t.Title, &stateStr, &t.CreatedAt, &t.UpdatedAt, &archived); err != nil {
			return nil, 0, err
		}
		t.Archived = archived == 1
		json.Unmarshal([]byte(stateStr), &t.State)
		threads = append(threads, t)
	}

	return threads, total, nil
}

func (s *SQLiteStore) RenameThread(id, title string) error {
	result, err := s.db.Exec(
		"UPDATE threads SET title = ?, updated_at = ? WHERE id = ?",
		title, time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *SQLiteStore) ArchiveThread(id string) error {
	result, err := s.db.Exec(
		"UPDATE threads SET archived = 1, updated_at = ? WHERE id = ?",
		time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	rows, _ := result.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *SQLiteStore) AddMessage(msg *Message) error {
	eventsJSON, _ := json.Marshal(msg.Events)
	metaJSON, _ := json.Marshal(msg.Metadata)
	_, err := s.db.Exec(
		"INSERT INTO messages (id, thread_id, role, content, events, metadata, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		msg.ID, msg.ThreadID, msg.Role, msg.Content, string(eventsJSON), string(metaJSON), msg.CreatedAt,
	)
	return err
}

func (s *SQLiteStore) GetMessages(threadID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(
		"SELECT id, thread_id, role, content, events, metadata, created_at FROM messages WHERE thread_id = ? ORDER BY created_at ASC LIMIT ?",
		threadID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var eventsStr, metaStr string
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &eventsStr, &metaStr, &m.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(eventsStr), &m.Events)
		json.Unmarshal([]byte(metaStr), &m.Metadata)
		messages = append(messages, m)
	}
	return messages, nil
}

func (s *SQLiteStore) CreateRun(run *Run) error {
	stateJSON, _ := json.Marshal(run.State)
	_, err := s.db.Exec(
		"INSERT INTO runs (id, thread_id, agent, status, state, error, started_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		run.ID, run.ThreadID, run.Agent, run.Status, string(stateJSON), run.Error, run.StartedAt,
	)
	return err
}

func (s *SQLiteStore) UpdateRun(run *Run) error {
	stateJSON, _ := json.Marshal(run.State)
	_, err := s.db.Exec(
		"UPDATE runs SET status = ?, state = ?, error = ?, ended_at = ? WHERE id = ?",
		run.Status, string(stateJSON), run.Error, run.EndedAt, run.ID,
	)
	return err
}

func (s *SQLiteStore) GetRun(id string) (*Run, error) {
	var r Run
	var stateStr string
	err := s.db.QueryRow(
		"SELECT id, thread_id, agent, status, state, error, started_at, ended_at FROM runs WHERE id = ?", id,
	).Scan(&r.ID, &r.ThreadID, &r.Agent, &r.Status, &stateStr, &r.Error, &r.StartedAt, &r.EndedAt)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("run %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(stateStr), &r.State)
	return &r, nil
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
