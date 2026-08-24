package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store using PostgreSQL via pgx.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore creates a new PostgreSQL-backed thread store.
// The connURI should be a standard PostgreSQL connection string,
// e.g. "postgres://user:pass@localhost:5432/gopilotkit?sslmode=disable".
func NewPostgresStore(connURI string) (*PostgresStore, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, connURI)
	if err != nil {
		return nil, fmt.Errorf("postgres: creating connection pool: %w", err)
	}

	// Verify the connection.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: pinging database: %w", err)
	}

	store := &PostgresStore{pool: pool}
	if err := store.migrate(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: migration failed: %w", err)
	}

	return store, nil
}

// migrate creates the database schema.
func (s *PostgresStore) migrate(ctx context.Context) error {
	schema := `
	CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		agent TEXT NOT NULL,
		title TEXT DEFAULT '',
		state JSONB DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		archived BOOLEAN NOT NULL DEFAULT FALSE
	);

	CREATE INDEX IF NOT EXISTS idx_threads_agent ON threads(agent);
	CREATE INDEX IF NOT EXISTS idx_threads_archived ON threads(archived);
	CREATE INDEX IF NOT EXISTS idx_threads_updated ON threads(updated_at DESC);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		content TEXT DEFAULT '',
		events JSONB DEFAULT '[]',
		metadata JSONB DEFAULT '{}',
		created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
	);

	CREATE INDEX IF NOT EXISTS idx_messages_thread ON messages(thread_id, created_at);

	CREATE TABLE IF NOT EXISTS runs (
		id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL REFERENCES threads(id) ON DELETE CASCADE,
		agent TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'running',
		state JSONB DEFAULT '{}',
		error TEXT DEFAULT '',
		started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		ended_at TIMESTAMPTZ
	);

	CREATE INDEX IF NOT EXISTS idx_runs_thread ON runs(thread_id);
	`

	_, err := s.pool.Exec(ctx, schema)
	return err
}

func (s *PostgresStore) CreateThread(thread *Thread) error {
	stateJSON, _ := json.Marshal(thread.State)
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO threads (id, agent, title, state, created_at, updated_at, archived)
		 VALUES ($1, $2, $3, $4, $5, $6, FALSE)`,
		thread.ID, thread.Agent, thread.Title, stateJSON, thread.CreatedAt, thread.UpdatedAt,
	)
	return err
}

func (s *PostgresStore) GetThread(id string) (*Thread, error) {
	var t Thread
	var stateJSON []byte
	err := s.pool.QueryRow(context.Background(),
		`SELECT id, agent, title, state, created_at, updated_at, archived
		 FROM threads WHERE id = $1`, id,
	).Scan(&t.ID, &t.Agent, &t.Title, &stateJSON, &t.CreatedAt, &t.UpdatedAt, &t.Archived)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("thread %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(stateJSON, &t.State)
	return &t, nil
}

func (s *PostgresStore) UpdateThread(thread *Thread) error {
	stateJSON, _ := json.Marshal(thread.State)
	thread.UpdatedAt = time.Now().UTC()
	tag, err := s.pool.Exec(context.Background(),
		`UPDATE threads SET title = $1, state = $2, updated_at = $3 WHERE id = $4`,
		thread.Title, stateJSON, thread.UpdatedAt, thread.ID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("thread %s not found", thread.ID)
	}
	return nil
}

func (s *PostgresStore) DeleteThread(id string) error {
	tag, err := s.pool.Exec(context.Background(), `DELETE FROM threads WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *PostgresStore) ListThreads(params ListThreadsParams) ([]Thread, int, error) {
	if params.Limit <= 0 {
		params.Limit = 50
	}

	ctx := context.Background()

	// Count total
	var total int
	if params.Archived {
		if params.Agent != "" {
			err := s.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM threads WHERE archived = TRUE AND agent = $1`, params.Agent,
			).Scan(&total)
			if err != nil {
				return nil, 0, err
			}
		} else {
			err := s.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM threads WHERE archived = TRUE`,
			).Scan(&total)
			if err != nil {
				return nil, 0, err
			}
		}
	} else {
		if params.Agent != "" {
			err := s.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM threads WHERE archived = FALSE AND agent = $1`, params.Agent,
			).Scan(&total)
			if err != nil {
				return nil, 0, err
			}
		} else {
			err := s.pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM threads WHERE archived = FALSE`,
			).Scan(&total)
			if err != nil {
				return nil, 0, err
			}
		}
	}

	// Fetch threads
	var query string
	var args []interface{}
	argIdx := 1

	if params.Archived {
		query = fmt.Sprintf(`SELECT id, agent, title, state, created_at, updated_at, archived
			FROM threads WHERE archived = TRUE`)
	} else {
		query = fmt.Sprintf(`SELECT id, agent, title, state, created_at, updated_at, archived
			FROM threads WHERE archived = FALSE`)
	}

	if params.Agent != "" {
		query += fmt.Sprintf(` AND agent = $%d`, argIdx)
		args = append(args, params.Agent)
		argIdx++
	}

	query += fmt.Sprintf(` ORDER BY updated_at DESC LIMIT $%d OFFSET $%d`, argIdx, argIdx+1)
	args = append(args, params.Limit, params.Offset)

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var threads []Thread
	for rows.Next() {
		var t Thread
		var stateJSON []byte
		if err := rows.Scan(&t.ID, &t.Agent, &t.Title, &stateJSON, &t.CreatedAt, &t.UpdatedAt, &t.Archived); err != nil {
			return nil, 0, err
		}
		json.Unmarshal(stateJSON, &t.State)
		threads = append(threads, t)
	}

	return threads, total, nil
}

func (s *PostgresStore) RenameThread(id, title string) error {
	tag, err := s.pool.Exec(context.Background(),
		`UPDATE threads SET title = $1, updated_at = $2 WHERE id = $3`,
		title, time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *PostgresStore) ArchiveThread(id string) error {
	tag, err := s.pool.Exec(context.Background(),
		`UPDATE threads SET archived = TRUE, updated_at = $1 WHERE id = $2`,
		time.Now().UTC(), id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("thread %s not found", id)
	}
	return nil
}

func (s *PostgresStore) AddMessage(msg *Message) error {
	eventsJSON, _ := json.Marshal(msg.Events)
	metaJSON, _ := json.Marshal(msg.Metadata)
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO messages (id, thread_id, role, content, events, metadata, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		msg.ID, msg.ThreadID, msg.Role, msg.Content, eventsJSON, metaJSON, msg.CreatedAt,
	)
	return err
}

func (s *PostgresStore) GetMessages(threadID string, limit int) ([]Message, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.pool.Query(context.Background(),
		`SELECT id, thread_id, role, content, events, metadata, created_at
		 FROM messages WHERE thread_id = $1 ORDER BY created_at ASC LIMIT $2`,
		threadID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var eventsJSON, metaJSON []byte
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.Role, &m.Content, &eventsJSON, &metaJSON, &m.CreatedAt); err != nil {
			return nil, err
		}
		json.Unmarshal(eventsJSON, &m.Events)
		json.Unmarshal(metaJSON, &m.Metadata)
		messages = append(messages, m)
	}
	return messages, nil
}

func (s *PostgresStore) CreateRun(run *Run) error {
	stateJSON, _ := json.Marshal(run.State)
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO runs (id, thread_id, agent, status, state, error, started_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		run.ID, run.ThreadID, run.Agent, run.Status, stateJSON, run.Error, run.StartedAt,
	)
	return err
}

func (s *PostgresStore) UpdateRun(run *Run) error {
	stateJSON, _ := json.Marshal(run.State)
	_, err := s.pool.Exec(context.Background(),
		`UPDATE runs SET status = $1, state = $2, error = $3, ended_at = $4 WHERE id = $5`,
		run.Status, stateJSON, run.Error, run.EndedAt, run.ID,
	)
	return err
}

func (s *PostgresStore) GetRun(id string) (*Run, error) {
	var r Run
	var stateJSON []byte
	err := s.pool.QueryRow(context.Background(),
		`SELECT id, thread_id, agent, status, state, error, started_at, ended_at
		 FROM runs WHERE id = $1`, id,
	).Scan(&r.ID, &r.ThreadID, &r.Agent, &r.Status, &stateJSON, &r.Error, &r.StartedAt, &r.EndedAt)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("run %s not found", id)
	}
	if err != nil {
		return nil, err
	}
	json.Unmarshal(stateJSON, &r.State)
	return &r, nil
}

func (s *PostgresStore) Close() error {
	s.pool.Close()
	return nil
}
