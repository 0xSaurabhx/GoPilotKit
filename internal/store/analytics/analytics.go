// Package analytics implements product analytics and insights.
//
// Provides real-time dashboards, OTLP export, cost tracking,
// and SQL-queryable interaction data.
package analytics

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// Event represents a single analytics event.
type Event struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`       // "run_start", "run_end", "tool_call", "error", "user_feedback", etc.
	RunID     string          `json:"run_id"`
	ThreadID  string          `json:"thread_id"`
	Agent     string          `json:"agent"`
	UserID    string          `json:"user_id,omitempty"`
	OrgID     string          `json:"org_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"` // Event-specific data
	DurationMs int64          `json:"duration_ms,omitempty"`
	TokensUsed int            `json:"tokens_used,omitempty"`
	CostUSD   float64         `json:"cost_usd,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Dashboard provides real-time analytics data.
type Dashboard struct {
	TotalRuns      int64              `json:"total_runs"`
	ActiveRuns     int                `json:"active_runs"`
	TotalTokens    int64              `json:"total_tokens"`
	TotalCostUSD   float64            `json:"total_cost_usd"`
	AvgLatencyMs   float64            `json:"avg_latency_ms"`
	ErrorRate      float64            `json:"error_rate"`
	RunsByAgent    map[string]int64   `json:"runs_by_agent"`
	RunsByStatus   map[string]int64   `json:"runs_by_status"`
	HourlyRuns     []HourlyBucket     `json:"hourly_runs"`
	TopTokensByAgent map[string]int64 `json:"top_tokens_by_agent"`
}

// HourlyBucket holds run counts for a one-hour window.
type HourlyBucket struct {
	Hour  time.Time `json:"hour"`
	Runs  int64     `json:"runs"`
	Tokens int64    `json:"tokens"`
}

// Config configures the analytics engine.
type Config struct {
	Enabled         bool   `json:"enabled"`
	OTLPEndpoint    string `json:"otlp_endpoint,omitempty"`
	RetentionDays   int    `json:"retention_days"` // Days to keep events (0 = forever)
	MaxEvents       int    `json:"max_events"`     // Max events in memory (0 = unlimited)
}

// Store is the analytics data store.
type Store struct {
	config Config
	events []Event
	mu     sync.RWMutex
}

// NewStore creates a new analytics store.
func NewStore(cfg Config) *Store {
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 30
	}
	if cfg.MaxEvents <= 0 {
		cfg.MaxEvents = 100000
	}
	return &Store{config: cfg}
}

// RecordEvent records a single analytics event.
func (s *Store) RecordEvent(evt Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if evt.ID == "" {
		evt.ID = fmt.Sprintf("evt_%d", time.Now().UnixNano())
	}
	if evt.Timestamp.IsZero() {
		evt.Timestamp = time.Now().UTC()
	}

	// Evict oldest if at capacity.
	if len(s.events) >= s.config.MaxEvents {
		cutoff := len(s.events) - s.config.MaxEvents + 1000
		if cutoff > 0 {
			s.events = s.events[cutoff:]
		}
	}

	s.events = append(s.events, evt)
}

// GetDashboard returns a real-time dashboard snapshot.
func (s *Store) GetDashboard() Dashboard {
	s.mu.RLock()
	defer s.mu.RUnlock()

	dash := Dashboard{
		RunsByAgent:   make(map[string]int64),
		RunsByStatus:  make(map[string]int64),
		TopTokensByAgent: make(map[string]int64),
		HourlyRuns:    make([]HourlyBucket, 0, 24),
	}

	var totalLatency int64
	var errorCount int64
	activeCount := 0

	for _, evt := range s.events {
		switch evt.Type {
		case "run_start":
			dash.TotalRuns++
			dash.RunsByAgent[evt.Agent]++
			activeCount++

		case "run_end":
			if activeCount > 0 {
				activeCount--
			}
			dash.TotalTokens += int64(evt.TokensUsed)
			dash.TotalCostUSD += evt.CostUSD
			totalLatency += evt.DurationMs
			dash.RunsByStatus["completed"]++
			dash.TopTokensByAgent[evt.Agent] += int64(evt.TokensUsed)

		case "run_error":
			if activeCount > 0 {
				activeCount--
			}
			errorCount++
			dash.RunsByStatus["error"]++
		}
	}

	dash.ActiveRuns = activeCount
	if dash.TotalRuns > 0 {
		dash.ErrorRate = float64(errorCount) / float64(dash.TotalRuns) * 100
	}
	if dash.TotalRuns > 0 {
		dash.AvgLatencyMs = float64(totalLatency) / float64(dash.TotalRuns)
	}

	// Build hourly buckets for the last 24 hours.
	now := time.Now().UTC()
	for i := 23; i >= 0; i-- {
		hourStart := now.Add(-time.Duration(i) * time.Hour).Truncate(time.Hour)
		hourEnd := hourStart.Add(time.Hour)
		bucket := HourlyBucket{Hour: hourStart}
		for _, evt := range s.events {
			if evt.Timestamp.After(hourStart) && evt.Timestamp.Before(hourEnd) {
				bucket.Runs++
				bucket.Tokens += int64(evt.TokensUsed)
			}
		}
		dash.HourlyRuns = append(dash.HourlyRuns, bucket)
	}

	return dash
}

// QueryEvents returns events matching the given filter criteria.
func (s *Store) QueryEvents(query EventQuery) []Event {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var results []Event
	for _, evt := range s.events {
		if query.Agent != "" && evt.Agent != query.Agent {
			continue
		}
		if query.ThreadID != "" && evt.ThreadID != query.ThreadID {
			continue
		}
		if query.RunID != "" && evt.RunID != query.RunID {
			continue
		}
		if query.EventType != "" && evt.Type != query.EventType {
			continue
		}
		if !query.Since.IsZero() && evt.Timestamp.Before(query.Since) {
			continue
		}
		results = append(results, evt)
		if query.Limit > 0 && len(results) >= query.Limit {
			break
		}
	}
	return results
}

// EventQuery defines filter criteria for event queries.
type EventQuery struct {
	Agent     string    `json:"agent,omitempty"`
	ThreadID  string    `json:"thread_id,omitempty"`
	RunID     string    `json:"run_id,omitempty"`
	EventType string    `json:"event_type,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	Limit     int       `json:"limit,omitempty"`
}

// GetTotalTokens returns total tokens across all events.
func (s *Store) GetTotalTokens() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total int64
	for _, e := range s.events {
		total += int64(e.TokensUsed)
	}
	return total
}

// GetTotalCost returns total cost across all events.
func (s *Store) GetTotalCost() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var total float64
	for _, e := range s.events {
		total += e.CostUSD
	}
	return total
}

// OTLPExport sends analytics events to an OTLP collector via HTTP.
// The endpoint should be the OTLP HTTP receiver URL, e.g. "http://localhost:4318".
// Events are converted to OTLP log records and sent in batches.
func (s *Store) OTLPExport(endpoint string, since time.Time) ([]Event, error) {
	events := s.QueryEvents(EventQuery{Since: since, Limit: 1000})
	if len(events) == 0 {
		return nil, nil
	}

	// Convert events to OTLP log payload.
	payload := buildOTLPPayload(events)

	// Send to the OTLP collector.
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("otlp: marshaling payload: %w", err)
	}

	url := endpoint + "/v1/logs"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("otlp: creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("otlp: sending to %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("otlp: collector returned %d: %s", resp.StatusCode, string(body))
	}

	log.Printf("otlp: exported %d events to %s", len(events), url)
	return events, nil
}

// otlpPayload is the OTLP JSON export payload for logs.
type otlpPayload struct {
	ResourceLogs []otlpResourceLogs `json:"resourceLogs"`
}

// otlpResourceLogs groups log records by resource.
type otlpResourceLogs struct {
	Resource  otlpResource    `json:"resource"`
	ScopeLogs []otlpScopeLogs `json:"scopeLogs"`
}

// otlpResource describes the service emitting the logs.
type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

// otlpScopeLogs groups log records by instrumentation scope.
type otlpScopeLogs struct {
	Scope     otlpScope      `json:"scope"`
	LogRecords []otlpLogRecord `json:"logRecords"`
}

// otlpScope identifies the instrumentation scope.
type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// otlpLogRecord is a single OTLP log record.
type otlpLogRecord struct {
	TimeUnixNano string           `json:"timeUnixNano"`
	Body         otlpAnyValue    `json:"body"`
	Attributes   []otlpAttribute `json:"attributes"`
}

// otlpAttribute is a key-value pair.
type otlpAttribute struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

// otlpAnyValue holds a typed value.
type otlpAnyValue struct {
	StringValue  string  `json:"stringValue,omitempty"`
	IntValue     int64   `json:"intValue,omitempty"`
	DoubleValue  float64 `json:"doubleValue,omitempty"`
}

// buildOTLPPayload converts analytics events to OTLP log format.
func buildOTLPPayload(events []Event) otlpPayload {
	logRecords := make([]otlpLogRecord, 0, len(events))
	for _, evt := range events {
		record := otlpLogRecord{
			TimeUnixNano: fmt.Sprintf("%d", evt.Timestamp.UnixNano()),
			Body:         otlpAnyValue{StringValue: evt.Type},
			Attributes: []otlpAttribute{
				{Key: "event.id", Value: otlpAnyValue{StringValue: evt.ID}},
				{Key: "event.run_id", Value: otlpAnyValue{StringValue: evt.RunID}},
				{Key: "event.thread_id", Value: otlpAnyValue{StringValue: evt.ThreadID}},
				{Key: "event.agent", Value: otlpAnyValue{StringValue: evt.Agent}},
			},
		}
		if evt.UserID != "" {
			record.Attributes = append(record.Attributes, otlpAttribute{Key: "event.user_id", Value: otlpAnyValue{StringValue: evt.UserID}})
		}
		if evt.OrgID != "" {
			record.Attributes = append(record.Attributes, otlpAttribute{Key: "event.org_id", Value: otlpAnyValue{StringValue: evt.OrgID}})
		}
		if evt.DurationMs > 0 {
			record.Attributes = append(record.Attributes, otlpAttribute{Key: "event.duration_ms", Value: otlpAnyValue{IntValue: evt.DurationMs}})
		}
		if evt.TokensUsed > 0 {
			record.Attributes = append(record.Attributes, otlpAttribute{Key: "event.tokens_used", Value: otlpAnyValue{IntValue: int64(evt.TokensUsed)}})
		}
		if evt.CostUSD > 0 {
			record.Attributes = append(record.Attributes, otlpAttribute{Key: "event.cost_usd", Value: otlpAnyValue{DoubleValue: evt.CostUSD}})
		}
		logRecords = append(logRecords, record)
	}

	return otlpPayload{
		ResourceLogs: []otlpResourceLogs{{
			Resource: otlpResource{
				Attributes: []otlpAttribute{
					{Key: "service.name", Value: otlpAnyValue{StringValue: "gopilotkit"}},
					{Key: "service.version", Value: otlpAnyValue{StringValue: "v1alpha"}},
				},
			},
			ScopeLogs: []otlpScopeLogs{{
				Scope:     otlpScope{Name: "gopilotkit-analytics", Version: "1.0.0"},
				LogRecords: logRecords,
			}},
		}},
	}
}

// PurgeOld removes events older than the retention period.
func (s *Store) PurgeOld() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().UTC().Add(-time.Duration(s.config.RetentionDays) * 24 * time.Hour)
	before := len(s.events)
	filtered := s.events[:0]
	for _, evt := range s.events {
		if evt.Timestamp.After(cutoff) {
			filtered = append(filtered, evt)
		}
	}
	s.events = filtered
	return before - len(s.events)
}

// Size returns the number of events in the store.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.events)
}
