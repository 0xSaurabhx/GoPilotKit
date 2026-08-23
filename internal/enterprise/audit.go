package enterprise

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// AuditEventType represents the type of audit event.
type AuditEventType string

const (
	// Authentication events
	EventLogin          AuditEventType = "auth.login"
	EventLogout         AuditEventType = "auth.logout"
	EventLoginFailed    AuditEventType = "auth.login_failed"
	EventTokenRefresh   AuditEventType = "auth.token_refresh"
	EventTokenRevoke    AuditEventType = "auth.token_revoke"

	// Agent events
	EventAgentRun       AuditEventType = "agent.run"
	EventAgentRunEnd    AuditEventType = "agent.run_end"
	EventAgentError     AuditEventType = "agent.error"

	// Data events
	EventDataRead       AuditEventType = "data.read"
	EventDataWrite      AuditEventType = "data.write"
	EventDataDelete     AuditEventType = "data.delete"
	EventDataExport     AuditEventType = "data.export"

	// Configuration events
	EventConfigRead     AuditEventType = "config.read"
	EventConfigUpdate   AuditEventType = "config.update"

	// System events
	EventSystemStart    AuditEventType = "system.start"
	EventSystemStop     AuditEventType = "system.stop"
	EventSystemError    AuditEventType = "system.error"
)

// AuditSeverity represents the severity of an audit event.
type AuditSeverity string

const (
	SeverityInfo    AuditSeverity = "info"
	SeverityWarning AuditSeverity = "warning"
	SeverityError   AuditSeverity = "error"
	SeverityCritical AuditSeverity = "critical"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	ID        string            `json:"id"`
	Timestamp time.Time         `json:"timestamp"`
	Type      AuditEventType    `json:"type"`
	Severity  AuditSeverity     `json:"severity"`
	UserID    string            `json:"user_id,omitempty"`
	Email     string            `json:"email,omitempty"`
	Action    string            `json:"action"`
	Resource  string            `json:"resource,omitempty"`
	Details   map[string]string `json:"details,omitempty"`
	IP        string            `json:"ip,omitempty"`
	UserAgent string            `json:"user_agent,omitempty"`
	Status    string            `json:"status,omitempty"` // "success" or "failure"
}

// AuditConfig configures the audit logger.
type AuditConfig struct {
	Enabled     bool   `yaml:"enabled"`
	Output      string `yaml:"output"`      // "file", "stdout", "stderr"
	FilePath    string `yaml:"file_path"`   // Path to audit log file
	MinSeverity AuditSeverity `yaml:"min_severity"` // Minimum severity to log
}

// DefaultAuditConfig returns default audit configuration.
func DefaultAuditConfig() AuditConfig {
	return AuditConfig{
		Enabled:     true,
		Output:      "stdout",
		MinSeverity: SeverityInfo,
	}
}

// AuditLogger logs audit events to a configured output.
type AuditLogger struct {
	config   AuditConfig
	writer   io.Writer
	file     *os.File
	mu       sync.Mutex
	eventID  int64
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(cfg AuditConfig) (*AuditLogger, error) {
	l := &AuditLogger{
		config: cfg,
	}

	if !cfg.Enabled {
		// Discard logs when disabled.
		l.writer = io.Discard
		return l, nil
	}

	switch cfg.Output {
	case "file":
		if cfg.FilePath == "" {
			cfg.FilePath = "audit.log"
		}
		f, err := os.OpenFile(cfg.FilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, fmt.Errorf("opening audit log file: %w", err)
		}
		l.file = f
		l.writer = f
	case "stderr":
		l.writer = os.Stderr
	default:
		l.writer = os.Stdout
	}

	return l, nil
}

// Log writes an audit event.
func (l *AuditLogger) Log(event *AuditEvent) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	if event.Severity == "" {
		event.Severity = SeverityInfo
	}

	// Check minimum severity.
	if !l.severityMeetsThreshold(event.Severity, l.config.MinSeverity) {
		return nil
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshaling audit event: %w", err)
	}

	_, err = fmt.Fprintf(l.writer, "%s\n", data)
	return err
}

// LogAuth logs an authentication event.
func (l *AuditLogger) LogAuth(eventType AuditEventType, userID, email, ip string, status string, details map[string]string) error {
	return l.Log(&AuditEvent{
		Type:     eventType,
		Severity: SeverityInfo,
		UserID:   userID,
		Email:    email,
		Action:   string(eventType),
		IP:       ip,
		Status:   status,
		Details:  details,
	})
}

// LogAgent logs an agent-related event.
func (l *AuditLogger) LogAgent(eventType AuditEventType, userID, agentName, threadID string, details map[string]string) error {
	return l.Log(&AuditEvent{
		Type:     eventType,
		Severity: SeverityInfo,
		UserID:   userID,
		Action:   string(eventType),
		Resource: agentName,
		Details:  details,
	})
}

// LogData logs a data access event.
func (l *AuditLogger) LogData(eventType AuditEventType, userID, resource string, details map[string]string) error {
	return l.Log(&AuditEvent{
		Type:     eventType,
		Severity: SeverityInfo,
		UserID:   userID,
		Action:   string(eventType),
		Resource: resource,
		Details:  details,
	})
}

// LogError logs an error event.
func (l *AuditLogger) LogError(eventType AuditEventType, userID, message string, details map[string]string) error {
	return l.Log(&AuditEvent{
		Type:     eventType,
		Severity: SeverityError,
		UserID:   userID,
		Action:   string(eventType),
		Status:   "failure",
		Details:  details,
	})
}

// Query queries audit events by type and time range.
func (l *AuditLogger) Query(file string, eventType AuditEventType, since, until time.Time) ([]*AuditEvent, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("opening audit log: %w", err)
	}
	defer f.Close()

	var events []*AuditEvent
	decoder := json.NewDecoder(f)
	for {
		var event AuditEvent
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, fmt.Errorf("decoding audit event: %w", err)
		}

		// Filter by type.
		if eventType != "" && event.Type != eventType {
			continue
		}

		// Filter by time range.
		if !since.IsZero() && event.Timestamp.Before(since) {
			continue
		}
		if !until.IsZero() && event.Timestamp.After(until) {
			continue
		}

		events = append(events, &event)
	}

	return events, nil
}

// Close closes the audit logger and releases resources.
func (l *AuditLogger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// severityMeetsThreshold checks if severity meets the minimum threshold.
func (l *AuditLogger) severityMeetsThreshold(actual, threshold AuditSeverity) bool {
	levels := map[AuditSeverity]int{
		SeverityInfo:     0,
		SeverityWarning:  1,
		SeverityError:    2,
		SeverityCritical: 3,
	}
	return levels[actual] >= levels[threshold]
}
