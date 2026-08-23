// Package containers implements learning container scoping.
//
// Containers isolate learning data per user, organization, or project,
// ensuring that each scope's learning is independent and auditable.
package containers

import (
	"encoding/json"
	"fmt"
	"time"
)

// ContainerScope defines the scope of a learning container.
type ContainerScope string

const (
	ScopeUser         ContainerScope = "user"
	ScopeOrganization ContainerScope = "organization"
	ScopeProject      ContainerScope = "project"
	ScopeAgent        ContainerScope = "agent"
)

// Container represents a scoped learning context.
type Container struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Scope       ContainerScope `json:"scope"`
	ScopeID     string          `json:"scope_id"`     // ID of the user/org/project
	Agent       string          `json:"agent"`
	Enabled     bool            `json:"enabled"`

	// Learning config for this container
	MinInteractions     int     `json:"min_interactions"`
	ImprovementThreshold float64 `json:"improvement_threshold"`

	// Stats
	TotalInteractions   int       `json:"total_interactions"`
	ImprovementsMade    int       `json:"improvements_made"`
	LastImprovementAt   *time.Time `json:"last_improvement_at,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Engine manages learning containers.
type Engine struct {
	containers map[string]*Container // id → container
	counter    int
}

// NewEngine creates a new container engine.
func NewEngine() *Engine {
	return &Engine{
		containers: make(map[string]*Container),
	}
}

// Create creates a new learning container.
func (e *Engine) Create(name string, scope ContainerScope, scopeID, agent string, config ContainerConfig) *Container {
	e.counter++
	now := time.Now().UTC()
	container := &Container{
		ID:                  fmt.Sprintf("container_%d", e.counter),
		Name:                name,
		Scope:               scope,
		ScopeID:             scopeID,
		Agent:               agent,
		Enabled:             true,
		MinInteractions:     config.MinInteractions,
		ImprovementThreshold: config.ImprovementThreshold,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if container.MinInteractions <= 0 {
		container.MinInteractions = 10
	}
	if container.ImprovementThreshold <= 0 {
		container.ImprovementThreshold = 0.1
	}
	e.containers[container.ID] = container
	return container
}

// Get returns a container by ID.
func (e *Engine) Get(id string) (*Container, bool) {
	c, ok := e.containers[id]
	return c, ok
}

// FindByScope finds containers matching a scope and scope ID.
func (e *Engine) FindByScope(scope ContainerScope, scopeID string) []*Container {
	var result []*Container
	for _, c := range e.containers {
		if c.Scope == scope && c.ScopeID == scopeID {
			result = append(result, c)
		}
	}
	return result
}

// FindByAgent finds all containers for an agent.
func (e *Engine) FindByAgent(agent string) []*Container {
	var result []*Container
	for _, c := range e.containers {
		if c.Agent == agent {
			result = append(result, c)
		}
	}
	return result
}

// RecordInteraction records an interaction in a container.
func (e *Engine) RecordInteraction(containerID string) {
	c, ok := e.containers[containerID]
	if !ok {
		return
	}
	c.TotalInteractions++
	c.UpdatedAt = time.Now().UTC()
}

// ShouldImprove checks if the container has enough interactions to justify improvement.
func (e *Engine) ShouldImprove(containerID string) bool {
	c, ok := e.containers[containerID]
	if !ok || !c.Enabled {
		return false
	}
	return c.TotalInteractions >= c.MinInteractions
}

// RecordImprovement records that an improvement was made.
func (e *Engine) RecordImprovement(containerID string) {
	c, ok := e.containers[containerID]
	if !ok {
		return
	}
	c.ImprovementsMade++
	now := time.Now().UTC()
	c.LastImprovementAt = &now
	c.UpdatedAt = now
}

// List returns all containers.
func (e *Engine) List() []*Container {
	var result []*Container
	for _, c := range e.containers {
		result = append(result, c)
	}
	return result
}

// Count returns the total number of containers.
func (e *Engine) Count() int {
	return len(e.containers)
}

// ContainerConfig holds configuration for creating a container.
type ContainerConfig struct {
	MinInteractions      int     `json:"min_interactions"`
	ImprovementThreshold float64 `json:"improvement_threshold"`
}

// MarshalJSON implements json.Marshaler for Container.
func (c Container) MarshalJSON() ([]byte, error) {
	type Alias Container
	return json.Marshal(struct {
		Alias
		ReadyToImprove bool `json:"ready_to_improve"`
	}{
		Alias:           Alias(c),
		ReadyToImprove:  c.TotalInteractions >= c.MinInteractions,
	})
}
