// Package a2a implements the Agent-to-Agent (A2A) protocol.
//
// A2A enables agents to discover, delegate to, and collaborate with other agents.
// It uses Agent Cards for capability discovery and Tasks for execution.
package a2a

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// --- Agent Card (capability discovery) ---

// AgentCard describes an agent's capabilities, following the A2A spec.
type AgentCard struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	URL          string            `json:"url"`           // Base URL for the agent
	Version      string            `json:"version"`
	Capabilities AgentCapabilities `json:"capabilities"`
	Skills       []Skill           `json:"skills"`
	Authentication *AuthConfig     `json:"authentication,omitempty"`
	DefaultInputModes  []string    `json:"default_input_modes"`  // ["text", "file", etc.]
	DefaultOutputModes []string    `json:"default_output_modes"`
}

// AgentCapabilities describes what the agent supports.
type AgentCapabilities struct {
	Streaming     bool `json:"streaming"`
	PushNotifications bool `json:"push_notifications"`
	StateTransitionHistory bool `json:"state_transition_history"`
}

// Skill represents a capability the agent exposes.
type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Examples    []string `json:"examples,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// AuthConfig describes authentication requirements.
type AuthConfig struct {
	Schemes    []string `json:"schemes"`     // ["bearer", "api_key", "oauth2"]
	Credentials string  `json:"credentials,omitempty"`
}

// --- Task Management ---

// TaskState represents the state of an A2A task.
type TaskState string

const (
	TaskStateSubmitted     TaskState = "submitted"
	TaskStateWorking       TaskState = "working"
	TaskStateInputRequired TaskState = "input-required"
	TaskStateCompleted     TaskState = "completed"
	TaskStateFailed        TaskState = "failed"
	TaskStateCanceled      TaskState = "canceled"
)

// Task represents a unit of work delegated to an agent.
type Task struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id,omitempty"`
	Status    TaskStatus      `json:"status"`
	Artifacts []Artifact      `json:"artifacts,omitempty"`
	History   []TaskMessage   `json:"history,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// TaskStatus holds the current status of a task.
type TaskStatus struct {
	State     TaskState `json:"state"`
	Message   *TaskMessage `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// TaskMessage is a message within a task.
type TaskMessage struct {
	Role      string          `json:"role"` // "user" or "agent"
	Parts     []MessagePart   `json:"parts"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}

// MessagePart is a part of a task message (text, file, data).
type MessagePart struct {
	Type     string          `json:"type"` // "text", "file", "data"
	Text     string          `json:"text,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
	FilePath string          `json:"file_path,omitempty"`
	MimeType string          `json:"mime_type,omitempty"`
}

// Artifact is an output produced by a task.
type Artifact struct {
	ID       string          `json:"id"`
	Name     string          `json:"name,omitempty"`
	Parts    []MessagePart   `json:"parts"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
}

// --- A2A Client (for calling remote agents) ---

// ClientConfig configures an A2A client.
type ClientConfig struct {
	BaseURL       string `json:"base_url"`
	Timeout       time.Duration `json:"timeout"`
	AuthHeader    string `json:"auth_header,omitempty"`
}

// Client communicates with remote A2A agents.
type Client struct {
	config  ClientConfig
	agents  map[string]*AgentCard // Cached agent cards
	mu      sync.RWMutex
}

// NewClient creates a new A2A client.
func NewClient(cfg ClientConfig) *Client {
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &Client{
		config: cfg,
		agents: make(map[string]*AgentCard),
	}
}

// DiscoverAgent fetches the agent card from a remote agent.
func (c *Client) DiscoverAgent(url string) (*AgentCard, error) {
	// In a real implementation, this would make an HTTP GET to {url}/.well-known/agent.json
	// For now, return a placeholder.
	return &AgentCard{
		Name:        "remote-agent",
		Description: "Discovered remote agent",
		URL:         url,
		Version:     "v1",
		Capabilities: AgentCapabilities{
			Streaming: true,
		},
	}, nil
}

// SendTask sends a task to a remote agent.
func (c *Client) SendTask(card *AgentCard, task *Task) (*Task, error) {
	if card == nil {
		return nil, fmt.Errorf("agent card is nil")
	}

	// In a real implementation, this would POST to card.URL/tasks/send
	// For now, simulate task acceptance.
	task.Status = TaskStatus{
		State:     TaskStateWorking,
		Timestamp: time.Now().UTC(),
	}

	return task, nil
}

// GetTask retrieves the status of a remote task.
func (c *Client) GetTask(card *AgentCard, taskID string) (*Task, error) {
	if card == nil {
		return nil, fmt.Errorf("agent card is nil")
	}
	// In a real implementation, GET to card.URL/tasks/{taskID}
	return &Task{
		ID: taskID,
		Status: TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: time.Now().UTC(),
		},
	}, nil
}

// --- A2A Server (for hosting an agent) ---

// TaskHandler is a function that processes an A2A task.
type TaskHandler func(task *Task) (*Task, error)

// Server is an A2A server that exposes an agent's capabilities.
type Server struct {
	card     AgentCard
	handlers map[string]TaskHandler
	mu       sync.RWMutex
}

// NewServer creates a new A2A server.
func NewServer(card AgentCard) *Server {
	return &Server{
		card:     card,
		handlers: make(map[string]TaskHandler),
	}
}

// RegisterSkill registers a task handler for a skill.
func (s *Server) RegisterSkill(skillID string, handler TaskHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[skillID] = handler
}

// GetAgentCard returns the agent card.
func (s *Server) GetAgentCard() AgentCard {
	return s.card
}

// HandleTask processes an incoming task.
func (s *Server) HandleTask(task *Task) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Route to the appropriate handler based on task metadata.
	if task.Status.State != TaskStateSubmitted {
		return nil, fmt.Errorf("unexpected task state: %s", task.Status.State)
	}

	// Find a handler — use the first registered one if no skill specified.
	for _, handler := range s.handlers {
		return handler(task)
	}

	return nil, fmt.Errorf("no handler registered for task")
}

// --- Delegation Manager ---

// DelegationManager manages agent-to-agent delegation.
type DelegationManager struct {
	localCard  *AgentCard
	remoteCards map[string]*AgentCard
	client     *Client
	mu         sync.RWMutex
}

// NewDelegationManager creates a delegation manager.
func NewDelegationManager(localCard *AgentCard, client *Client) *DelegationManager {
	return &DelegationManager{
		localCard:  localCard,
		remoteCards: make(map[string]*AgentCard),
		client:     client,
	}
}

// RegisterRemote registers a remote agent's card.
func (dm *DelegationManager) RegisterRemote(card *AgentCard) {
	dm.mu.Lock()
	defer dm.mu.Unlock()
	dm.remoteCards[card.Name] = card
}

// Delegate sends a task to a remote agent.
func (dm *DelegationManager) Delegate(agentName string, task *Task) (*Task, error) {
	dm.mu.RLock()
	card, ok := dm.remoteCards[agentName]
	dm.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("remote agent %q not registered", agentName)
	}

	return dm.client.SendTask(card, task)
}

// ListRemoteAgents returns all registered remote agents.
func (dm *DelegationManager) ListRemoteAgents() []*AgentCard {
	dm.mu.RLock()
	defer dm.mu.RUnlock()
	var cards []*AgentCard
	for _, card := range dm.remoteCards {
		cards = append(cards, card)
	}
	return cards
}
