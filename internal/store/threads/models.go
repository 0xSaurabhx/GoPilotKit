package threads

import (
	"encoding/json"
	"time"
)

// Thread represents a persistent conversation thread.
type Thread struct {
	ID        string          `json:"id"`
	Agent     string          `json:"agent"`
	Title     string          `json:"title,omitempty"`
	State     json.RawMessage `json:"state,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Archived  bool            `json:"archived"`
}

// Message represents a single message in a thread.
type Message struct {
	ID        string          `json:"id"`
	ThreadID  string          `json:"thread_id"`
	Role      string          `json:"role"` // "user", "assistant", "system", "tool"
	Content   string          `json:"content,omitempty"`
	Events    json.RawMessage `json:"events,omitempty"` // Serialized AG-UI events
	Metadata  json.RawMessage `json:"metadata,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// Run represents a persisted agent run.
type Run struct {
	ID        string          `json:"id"`
	ThreadID  string          `json:"thread_id"`
	Agent     string          `json:"agent"`
	Status    string          `json:"status"` // "running", "completed", "error", "cancelled"
	State     json.RawMessage `json:"state,omitempty"`
	Error     string          `json:"error,omitempty"`
	StartedAt time.Time       `json:"started_at"`
	EndedAt   *time.Time      `json:"ended_at,omitempty"`
}

// ListThreadsParams controls thread listing.
type ListThreadsParams struct {
	Agent    string
	Archived bool
	Limit    int
	Offset   int
}

// Store defines the interface for thread persistence.
type Store interface {
	// Thread operations
	CreateThread(thread *Thread) error
	GetThread(id string) (*Thread, error)
	UpdateThread(thread *Thread) error
	DeleteThread(id string) error
	ListThreads(params ListThreadsParams) ([]Thread, int, error)
	RenameThread(id, title string) error
	ArchiveThread(id string) error

	// Message operations
	AddMessage(msg *Message) error
	GetMessages(threadID string, limit int) ([]Message, error)

	// Run operations
	CreateRun(run *Run) error
	UpdateRun(run *Run) error
	GetRun(id string) (*Run, error)

	// Lifecycle
	Close() error
}
