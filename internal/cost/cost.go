// Package cost implements token counting, budget management, and rate limiting.
//
// Tracks per-request, per-user, and per-org token usage and cost.
// Enforces budget limits and rate limits based on deployment profile.
package cost

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// UsageRecord tracks token consumption for a single request.
type UsageRecord struct {
	RequestID       string    `json:"request_id"`
	ThreadID        string    `json:"thread_id,omitempty"`
	Agent           string    `json:"agent"`
	ProviderID      string    `json:"provider_id"`
	Model           string    `json:"model"`
	PromptTokens    int       `json:"prompt_tokens"`
	CompletionTokens int      `json:"completion_tokens"`
	TotalTokens     int       `json:"total_tokens"`
	CachedTokens    int       `json:"cached_tokens,omitempty"`
	CostUSD         float64   `json:"cost_usd"`
	Timestamp       time.Time `json:"timestamp"`
	DurationMs      int64     `json:"duration_ms,omitempty"`
}

// ProviderPricing defines per-token pricing for a provider/model combination.
type ProviderPricing struct {
	Provider        string  `json:"provider"`
	Model           string  `json:"model"`
	PromptPerToken  float64 `json:"prompt_per_token"`   // Cost per prompt token in USD
	CompletionPerToken float64 `json:"completion_per_token"` // Cost per completion token in USD
	CachedPerToken  float64 `json:"cached_per_token"`    // Cost per cached token in USD
}

// BudgetConfig defines budget limits.
type BudgetConfig struct {
	MaxDailyUSD   float64 `json:"max_daily_usd"`   // Maximum daily spend in USD
	MaxMonthlyUSD float64 `json:"max_monthly_usd"` // Maximum monthly spend in USD
	MaxPerRequest int     `json:"max_per_request"` // Maximum tokens per request
	RateLimitRPM  int     `json:"rate_limit_rpm"`  // Requests per minute
	RateLimitTPM  int     `json:"rate_limit_tpm"`  // Tokens per minute
}

// DefaultPricing returns default pricing for known providers.
func DefaultPricing() []ProviderPricing {
	return []ProviderPricing{
		{Provider: "openai", Model: "gpt-4o", PromptPerToken: 0.0000025, CompletionPerToken: 0.00001, CachedPerToken: 0.00000125},
		{Provider: "openai", Model: "gpt-4o-mini", PromptPerToken: 0.00000015, CompletionPerToken: 0.0000006, CachedPerToken: 0.000000075},
		{Provider: "anthropic", Model: "claude-3-5-sonnet", PromptPerToken: 0.000003, CompletionPerToken: 0.000015, CachedPerToken: 0.0000003},
		{Provider: "anthropic", Model: "claude-3-5-haiku", PromptPerToken: 0.0000008, CompletionPerToken: 0.000004, CachedPerToken: 0.00000008},
	}
}

// Tracker manages cost tracking and budget enforcement.
type Tracker struct {
	pricing  map[string]ProviderPricing // "provider:model" → pricing
	budget   BudgetConfig
	records  []UsageRecord
	mu       sync.RWMutex

	// Rate limiting state
	rateWindow    time.Time
	rateCount     int
	rateTokens    int
}

// NewTracker creates a new cost tracker.
func NewTracker(budget BudgetConfig) *Tracker {
	t := &Tracker{
		pricing: make(map[string]ProviderPricing),
		budget:  budget,
	}
	for _, p := range DefaultPricing() {
		key := p.Provider + ":" + p.Model
		t.pricing[key] = p
	}
	return t
}

// CalculateCost computes the USD cost for a given token usage.
func (t *Tracker) CalculateCost(provider, model string, promptTokens, completionTokens, cachedTokens int) float64 {
	key := provider + ":" + model
	t.mu.RLock()
	pricing, ok := t.pricing[key]
	t.mu.RUnlock()

	if !ok {
		// Default to a reasonable cost if pricing unknown.
		return float64(promptTokens+completionTokens) * 0.000005
	}

	cost := float64(promptTokens)*pricing.PromptPerToken +
		float64(completionTokens)*pricing.CompletionPerToken +
		float64(cachedTokens)*pricing.CachedPerToken
	return cost
}

// RecordUsage records a single usage event and checks budget limits.
func (t *Tracker) RecordUsage(record UsageRecord) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Calculate cost if not set.
	if record.CostUSD == 0 {
		key := record.ProviderID + ":" + record.Model
		if pricing, ok := t.pricing[key]; ok {
			record.CostUSD = float64(record.PromptTokens)*pricing.PromptPerToken +
				float64(record.CompletionTokens)*pricing.CompletionPerToken +
				float64(record.CachedTokens)*pricing.CachedPerToken
		}
	}

	// Check per-request token limit.
	if t.budget.MaxPerRequest > 0 && record.TotalTokens > t.budget.MaxPerRequest {
		return fmt.Errorf("request exceeded per-request token limit: %d > %d", record.TotalTokens, t.budget.MaxPerRequest)
	}

	// Check rate limit.
	if t.budget.RateLimitRPM > 0 {
		now := time.Now()
		if now.Sub(t.rateWindow) >= time.Minute {
			t.rateWindow = now
			t.rateCount = 0
			t.rateTokens = 0
		}
		t.rateCount++
		t.rateTokens += record.TotalTokens

		if t.rateCount > t.budget.RateLimitRPM {
			return fmt.Errorf("rate limit exceeded: %d requests in the last minute (limit: %d)", t.rateCount, t.budget.RateLimitRPM)
		}
		if t.budget.RateLimitTPM > 0 && t.rateTokens > t.budget.RateLimitTPM {
			return fmt.Errorf("token rate limit exceeded: %d tokens in the last minute (limit: %d)", t.rateTokens, t.budget.RateLimitTPM)
		}
	}

	// Check daily budget.
	if t.budget.MaxDailyUSD > 0 {
		dailyTotal := t.sumDailyLocked()
		if dailyTotal+record.CostUSD > t.budget.MaxDailyUSD {
			return fmt.Errorf("daily budget exceeded: $%.4f + $%.4f > $%.2f", dailyTotal, record.CostUSD, t.budget.MaxDailyUSD)
		}
	}

	// Check monthly budget.
	if t.budget.MaxMonthlyUSD > 0 {
		monthlyTotal := t.sumMonthlyLocked()
		if monthlyTotal+record.CostUSD > t.budget.MaxMonthlyUSD {
			return fmt.Errorf("monthly budget exceeded: $%.4f + $%.4f > $%.2f", monthlyTotal, record.CostUSD, t.budget.MaxMonthlyUSD)
		}
	}

	t.records = append(t.records, record)
	return nil
}

// GetDailyTotal returns the total spend for today in USD.
func (t *Tracker) GetDailyTotal() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sumDailyLocked()
}

// GetMonthlyTotal returns the total spend for this month in USD.
func (t *Tracker) GetMonthlyTotal() float64 {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sumMonthlyLocked()
}

// GetRecords returns all usage records (optionally filtered).
func (t *Tracker) GetRecords(limit int) []UsageRecord {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if limit <= 0 || limit > len(t.records) {
		limit = len(t.records)
	}
	result := make([]UsageRecord, limit)
	copy(result, t.records[len(t.records)-limit:])
	return result
}

// GetBudgetStatus returns the current budget usage status.
func (t *Tracker) GetBudgetStatus() map[string]interface{} {
	t.mu.RLock()
	defer t.mu.RUnlock()

	daily := t.sumDailyLocked()
	monthly := t.sumMonthlyLocked()

	status := map[string]interface{}{
		"daily_used":   fmt.Sprintf("$%.4f", daily),
		"monthly_used": fmt.Sprintf("$%.4f", monthly),
		"total_records": len(t.records),
	}
	if t.budget.MaxDailyUSD > 0 {
		status["daily_limit"] = fmt.Sprintf("$%.2f", t.budget.MaxDailyUSD)
		status["daily_remaining"] = fmt.Sprintf("$%.4f", t.budget.MaxDailyUSD-daily)
		status["daily_pct"] = fmt.Sprintf("%.1f%%", daily/t.budget.MaxDailyUSD*100)
	}
	if t.budget.MaxMonthlyUSD > 0 {
		status["monthly_limit"] = fmt.Sprintf("$%.2f", t.budget.MaxMonthlyUSD)
		status["monthly_remaining"] = fmt.Sprintf("$%.4f", t.budget.MaxMonthlyUSD-monthly)
	}
	return status
}

// sumDailyLocked sums all records from today. Caller must hold lock.
func (t *Tracker) sumDailyLocked() float64 {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var total float64
	for _, r := range t.records {
		if r.Timestamp.After(today) || r.Timestamp.Equal(today) {
			total += r.CostUSD
		}
	}
	return total
}

// sumMonthlyLocked sums all records from this month. Caller must hold lock.
func (t *Tracker) sumMonthlyLocked() float64 {
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var total float64
	for _, r := range t.records {
		if r.Timestamp.After(monthStart) || r.Timestamp.Equal(monthStart) {
			total += r.CostUSD
		}
	}
	return total
}

// SetPricing adds or updates pricing for a provider/model.
func (t *Tracker) SetPricing(p ProviderPricing) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.pricing[p.Provider+":"+p.Model] = p
}

// Summary aggregates all usage into a summary report.
type Summary struct {
	TotalRequests   int     `json:"total_requests"`
	TotalTokens     int     `json:"total_tokens"`
	TotalCostUSD    float64 `json:"total_cost_usd"`
	AvgTokensPerReq float64 `json:"avg_tokens_per_req"`
	AvgCostPerReq   float64 `json:"avg_cost_per_req"`
	ByProvider      map[string]ProviderSummary `json:"by_provider"`
}

// ProviderSummary aggregates usage per provider.
type ProviderSummary struct {
	Requests    int     `json:"requests"`
	Tokens      int     `json:"tokens"`
	CostUSD     float64 `json:"cost_usd"`
}

// GetSummary returns an aggregated usage summary.
func (t *Tracker) GetSummary() Summary {
	t.mu.RLock()
	defer t.mu.RUnlock()

	s := Summary{
		ByProvider: make(map[string]ProviderSummary),
	}

	for _, r := range t.records {
		s.TotalRequests++
		s.TotalTokens += r.TotalTokens
		s.TotalCostUSD += r.CostUSD

		ps := s.ByProvider[r.ProviderID]
		ps.Requests++
		ps.Tokens += r.TotalTokens
		ps.CostUSD += r.CostUSD
		s.ByProvider[r.ProviderID] = ps
	}

	if s.TotalRequests > 0 {
		s.AvgTokensPerReq = float64(s.TotalTokens) / float64(s.TotalRequests)
		s.AvgCostPerReq = s.TotalCostUSD / float64(s.TotalRequests)
	}

	return s
}

// MarshalJSON implements json.Marshaler for Summary.
func (s Summary) MarshalJSON() ([]byte, error) {
	type Alias Summary
	return json.Marshal(struct {
		Alias
		TotalCostUSD string `json:"total_cost_usd"`
		AvgCostPerReq string `json:"avg_cost_per_req"`
	}{
		Alias:         Alias(s),
		TotalCostUSD:  fmt.Sprintf("$%.4f", s.TotalCostUSD),
		AvgCostPerReq: fmt.Sprintf("$%.6f", s.AvgCostPerReq),
	})
}
