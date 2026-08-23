package enterprise

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// RetentionPolicy defines how long data should be retained.
type RetentionPolicy struct {
	Name      string        `json:"name"`
	Resource  string        `json:"resource"`  // "threads", "messages", "audit_logs", "analytics"
	MaxAge    time.Duration `json:"max_age"`
	MaxCount  int           `json:"max_count,omitempty"` // 0 = no limit
	KeepLatest int          `json:"keep_latest,omitempty"` // Always keep N most recent
}

// RetentionConfig holds data retention configuration.
type RetentionConfig struct {
	Enabled bool              `yaml:"enabled"`
	Days    int               `yaml:"days"` // Default retention in days
	Policies []RetentionPolicy `yaml:"policies,omitempty"`
}

// DefaultRetentionConfig returns default retention configuration.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		Enabled: false,
		Days:    90,
		Policies: []RetentionPolicy{
			{Name: "threads", Resource: "threads", MaxAge: 90 * 24 * time.Hour, KeepLatest: 100},
			{Name: "messages", Resource: "messages", MaxAge: 90 * 24 * time.Hour},
			{Name: "audit_logs", Resource: "audit_logs", MaxAge: 365 * 24 * time.Hour},
			{Name: "analytics", Resource: "analytics", MaxAge: 30 * 24 * time.Hour},
		},
	}
}

// RetentionDeleter defines the interface for deleting expired data.
type RetentionDeleter interface {
	// DeleteBefore deletes records older than the given time.
	DeleteBefore(resource string, before time.Time) (int64, error)
}

// RetentionManager enforces data retention policies.
type RetentionManager struct {
	config  RetentionConfig
	deleter RetentionDeleter
	policies map[string]*RetentionPolicy
	mu      sync.RWMutex
	stopCh  chan struct{}
}

// NewRetentionManager creates a new retention manager.
func NewRetentionManager(cfg RetentionConfig, deleter RetentionDeleter) *RetentionManager {
	m := &RetentionManager{
		config:  cfg,
		deleter: deleter,
		policies: make(map[string]*RetentionPolicy),
		stopCh:  make(chan struct{}),
	}

	// Index policies by resource.
	for i := range cfg.Policies {
		p := cfg.Policies[i]
		m.policies[p.Resource] = &p
	}

	return m
}

// Enforce runs the retention enforcement for all policies.
func (m *RetentionManager) Enforce() error {
	if !m.config.Enabled {
		return nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var lastErr error
	for _, policy := range m.policies {
		if err := m.enforcePolicy(policy); err != nil {
			log.Printf("retention: error enforcing policy %s: %v", policy.Name, err)
			lastErr = err
		}
	}

	return lastErr
}

// enforcePolicy enforces a single retention policy.
func (m *RetentionManager) enforcePolicy(policy *RetentionPolicy) error {
	cutoff := time.Now().UTC().Add(-policy.MaxAge)

	deleted, err := m.deleter.DeleteBefore(policy.Resource, cutoff)
	if err != nil {
		return fmt.Errorf("deleting expired %s: %w", policy.Resource, err)
	}

	if deleted > 0 {
		log.Printf("retention: deleted %d expired %s records", deleted, policy.Resource)
	}

	return nil
}

// Start begins periodic retention enforcement.
func (m *RetentionManager) Start(interval time.Duration) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := m.Enforce(); err != nil {
					log.Printf("retention: enforcement error: %v", err)
				}
			case <-m.stopCh:
				return
			}
		}
	}()
}

// Stop stops the periodic retention enforcement.
func (m *RetentionManager) Stop() {
	close(m.stopCh)
}

// GetPolicies returns all configured retention policies.
func (m *RetentionManager) GetPolicies() []RetentionPolicy {
	m.mu.RLock()
	defer m.mu.RUnlock()
	policies := make([]RetentionPolicy, 0, len(m.policies))
	for _, p := range m.policies {
		policies = append(policies, *p)
	}
	return policies
}

// GetPolicy returns the retention policy for a resource.
func (m *RetentionManager) GetPolicy(resource string) (*RetentionPolicy, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[resource]
	return p, ok
}
