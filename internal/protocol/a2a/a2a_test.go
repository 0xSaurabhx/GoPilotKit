package a2a

import (
	"testing"
	"time"
)

func TestAgentCard(t *testing.T) {
	card := AgentCard{
		Name:        "test-agent",
		Description: "A test agent",
		URL:         "http://localhost:8080",
		Version:     "v1",
		Capabilities: AgentCapabilities{
			Streaming: true,
		},
		Skills: []Skill{
			{
				ID:          "search",
				Name:        "Web Search",
				Description: "Search the web for information",
			},
		},
	}

	if card.Name != "test-agent" {
		t.Errorf("expected name 'test-agent', got '%s'", card.Name)
	}
	if !card.Capabilities.Streaming {
		t.Error("expected streaming capability")
	}
	if len(card.Skills) != 1 {
		t.Errorf("expected 1 skill, got %d", len(card.Skills))
	}
}

func TestTaskLifecycle(t *testing.T) {
	task := &Task{
		ID: "task_1",
		Status: TaskStatus{
			State:     TaskStateSubmitted,
			Timestamp: time.Now().UTC(),
		},
		History: []TaskMessage{
			{
				Role: "user",
				Parts: []MessagePart{
					{Type: "text", Text: "Hello, agent!"},
				},
			},
		},
	}

	if task.Status.State != TaskStateSubmitted {
		t.Errorf("expected submitted state, got %s", task.Status.State)
	}

	// Simulate state transitions.
	task.Status = TaskStatus{
		State:     TaskStateWorking,
		Timestamp: time.Now().UTC(),
	}
	if task.Status.State != TaskStateWorking {
		t.Errorf("expected working state, got %s", task.Status.State)
	}

	task.Status = TaskStatus{
		State:     TaskStateCompleted,
		Timestamp: time.Now().UTC(),
	}
	if task.Status.State != TaskStateCompleted {
		t.Errorf("expected completed state, got %s", task.Status.State)
	}
}

func TestTaskStates(t *testing.T) {
	states := []TaskState{
		TaskStateSubmitted,
		TaskStateWorking,
		TaskStateInputRequired,
		TaskStateCompleted,
		TaskStateFailed,
		TaskStateCanceled,
	}

	seen := make(map[TaskState]bool)
	for _, state := range states {
		if seen[state] {
			t.Errorf("duplicate state: %s", state)
		}
		seen[state] = true
	}
}

func TestServerHandleTask(t *testing.T) {
	card := AgentCard{
		Name:    "test-server",
		Version: "v1",
	}
	server := NewServer(card)

	// Register a handler.
	server.RegisterSkill("search", func(task *Task) (*Task, error) {
		task.Status = TaskStatus{
			State:     TaskStateCompleted,
			Timestamp: time.Now().UTC(),
		}
		task.Artifacts = []Artifact{
			{
				ID: "art_1",
				Parts: []MessagePart{
					{Type: "text", Text: "Search results here"},
				},
			},
		}
		return task, nil
	})

	// Send a task.
	task := &Task{
		ID: "task_1",
		Status: TaskStatus{
			State: TaskStateSubmitted,
		},
		History: []TaskMessage{
			{Role: "user", Parts: []MessagePart{{Type: "text", Text: "Search for Go"}}},
		},
	}

	result, err := server.HandleTask(task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status.State != TaskStateCompleted {
		t.Errorf("expected completed, got %s", result.Status.State)
	}
	if len(result.Artifacts) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(result.Artifacts))
	}
}

func TestDelegationManager(t *testing.T) {
	localCard := &AgentCard{Name: "local-agent", Version: "v1"}
	client := NewClient(ClientConfig{BaseURL: "http://localhost:8080"})
	dm := NewDelegationManager(localCard, client)

	// Register a remote agent.
	remoteCard := &AgentCard{
		Name: "remote-agent",
		URL:  "http://remote:9090",
	}
	dm.RegisterRemote(remoteCard)

	agents := dm.ListRemoteAgents()
	if len(agents) != 1 {
		t.Errorf("expected 1 remote agent, got %d", len(agents))
	}

	// Delegate a task.
	task := &Task{
		ID: "task_1",
		Status: TaskStatus{
			State: TaskStateSubmitted,
		},
		History: []TaskMessage{
			{Role: "user", Parts: []MessagePart{{Type: "text", Text: "Hello"}}},
		},
	}

	result, err := dm.Delegate("remote-agent", task)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status.State != TaskStateWorking {
		t.Errorf("expected working state after delegation, got %s", result.Status.State)
	}
}

func TestDelegationManagerNotFound(t *testing.T) {
	localCard := &AgentCard{Name: "local", Version: "v1"}
	client := NewClient(ClientConfig{BaseURL: "http://localhost:8080"})
	dm := NewDelegationManager(localCard, client)

	_, err := dm.Delegate("nonexistent", &Task{})
	if err == nil {
		t.Error("expected error for nonexistent agent")
	}
}
