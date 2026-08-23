// Package guardrails implements input/output validation and content filtering.
//
// Provides configurable policy chains for content filtering, output
// schema validation, toxicity detection, and jailbreak prevention.
package guardrails

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Severity represents how severely a guardrail violation is treated.
type Severity int

const (
	SeverityLow    Severity = iota // Logged but allowed
	SeverityMedium                 // Scrubbed/masked
	SeverityHigh                   // Blocked entirely
)

// Violation represents a single guardrail violation detected in content.
type Violation struct {
	Rule    string   `json:"rule"`              // Rule that triggered (e.g. "pii_email")
	Severity Severity `json:"severity"`          // How severe
	Content string   `json:"content,omitempty"` // The violating content (redacted)
	Offset  int      `json:"offset,omitempty"`  // Position in original text
	Length  int      `json:"length,omitempty"`  // Length of violating span
}

// Result is the outcome of running content through guardrails.
type Result struct {
	Allowed    bool        `json:"allowed"`              // Whether the content passed
	Scrubbed   string      `json:"scrubbed,omitempty"`   // Content after scrubbing
	Violations []Violation `json:"violations,omitempty"`  // All violations found
	Blocked    bool        `json:"blocked"`              // Whether content was blocked
	BlockReason string    `json:"block_reason,omitempty"` // Why it was blocked
}

// Policy defines a guardrail rule.
type Policy struct {
	Name     string   `json:"name"`
	Enabled  bool     `json:"enabled"`
	Severity Severity `json:"severity"`
}

// Engine is the guardrails engine that runs a chain of policies on content.
type Engine struct {
	policies []Policy
	mu       sync.RWMutex

	// PII patterns
	piiPatterns []*regexp.Regexp

	// Content filtering patterns (toxicity, jailbreak, etc.)
	contentPatterns []*regexp.Regexp

	// Blocked words/phrases
	blockedWords []string
}

// Config configures the guardrails engine.
type Config struct {
	EnablePIIScrubbing  bool     `json:"enable_pii_scrubbing"`
	EnableContentFilter bool     `json:"enable_content_filter"`
	BlockedWords         []string `json:"blocked_words,omitempty"`
}

// DefaultConfig returns a default guardrails configuration.
func DefaultConfig() Config {
	return Config{
		EnablePIIScrubbing:  true,
		EnableContentFilter: true,
	}
}

// NewEngine creates a new guardrails engine with the given configuration.
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		policies: []Policy{
			{Name: "pii_scrubbing", Enabled: cfg.EnablePIIScrubbing, Severity: SeverityMedium},
			{Name: "content_filter", Enabled: cfg.EnableContentFilter, Severity: SeverityHigh},
			{Name: "blocked_words", Enabled: len(cfg.BlockedWords) > 0, Severity: SeverityHigh},
		},
		blockedWords: cfg.BlockedWords,
	}

	// Compile PII patterns
	e.piiPatterns = []*regexp.Regexp{
		// Email addresses
		regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
		// US phone numbers (various formats)
		regexp.MustCompile(`\b(?:\+?1[-.\s]?)?\(?\d{3}\)?[-.\s]?\d{3}[-.\s]?\d{4}\b`),
		// SSN-like patterns (XXX-XX-XXXX)
		regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`),
		// Credit card numbers (13-19 digits with optional separators)
		regexp.MustCompile(`\b(?:\d{4}[-\s]?){3}\d{1,7}\b`),
		// IP addresses
		regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`),
	}

	// Compile content filter patterns (basic toxicity/jailbreak detection)
	e.contentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\b(hack|exploit|bypass|jailbreak)\b.*\b(system|prompt|rules|filter)\b`),
		regexp.MustCompile(`(?i)\bignore\s+(all\s+)?(previous|prior|above|your)\s+(instructions|rules|prompts)\b`),
		regexp.MustCompile(`(?i)\byou\s+are\s+now\s+(?:a|an)\b`),
	}

	return e
}

// CheckInput validates and scrubs user input before it reaches the LLM.
func (e *Engine) CheckInput(content string) *Result {
	return e.check(content, "input")
}

// CheckOutput validates assistant output before it reaches the user.
func (e *Engine) CheckOutput(content string) *Result {
	return e.check(content, "output")
}

// check is the internal implementation that runs all enabled policies.
func (e *Engine) check(content, direction string) *Result {
	e.mu.RLock()
	defer e.mu.RUnlock()

	result := &Result{
		Allowed:  true,
		Scrubbed: content,
	}

	for _, policy := range e.policies {
		if !policy.Enabled {
			continue
		}

		switch policy.Name {
		case "pii_scrubbing":
			piiViolations := e.detectPII(result.Scrubbed)
			result.Violations = append(result.Violations, piiViolations...)
			if policy.Severity == SeverityMedium {
				result.Scrubbed = e.scrubPII(result.Scrubbed)
			}

		case "content_filter":
			violations := e.detectContent(result.Scrubbed)
			for _, v := range violations {
				if v.Severity >= SeverityHigh {
					result.Blocked = true
					result.BlockReason = fmt.Sprintf("content policy violation: %s", v.Rule)
					result.Allowed = false
				}
				result.Violations = append(result.Violations, v)
			}

		case "blocked_words":
			violations := e.detectBlockedWords(result.Scrubbed)
			for _, v := range violations {
				if v.Severity >= SeverityHigh {
					result.Blocked = true
					result.BlockReason = fmt.Sprintf("blocked word detected: %s", v.Rule)
					result.Allowed = false
				}
				result.Violations = append(result.Violations, v)
			}
		}
	}

	return result
}

// detectPII finds PII patterns in content.
func (e *Engine) detectPII(content string) []Violation {
	var violations []Violation
	type piiDef struct {
		pattern *regexp.Regexp
		rule    string
	}
	defs := []piiDef{
		{e.piiPatterns[0], "pii_email"},
		{e.piiPatterns[1], "pii_phone"},
		{e.piiPatterns[2], "pii_ssn"},
		{e.piiPatterns[3], "pii_credit_card"},
		{e.piiPatterns[4], "pii_ip_address"},
	}

	for _, def := range defs {
		matches := def.pattern.FindAllStringIndex(content, -1)
		for _, loc := range matches {
			violations = append(violations, Violation{
				Rule:     def.rule,
				Severity: SeverityMedium,
				Content:  maskString(content[loc[0]:loc[1]]),
				Offset:   loc[0],
				Length:   loc[1] - loc[0],
			})
		}
	}
	return violations
}

// scrubPII replaces PII with redacted placeholders.
func (e *Engine) scrubPII(content string) string {
	type scrubDef struct {
		pattern     *regexp.Regexp
		replacement string
	}
	defs := []scrubDef{
		{e.piiPatterns[0], "[EMAIL_REDACTED]"},
		{e.piiPatterns[1], "[PHONE_REDACTED]"},
		{e.piiPatterns[2], "[SSN_REDACTED]"},
		{e.piiPatterns[3], "[CARD_REDACTED]"},
		{e.piiPatterns[4], "[IP_REDACTED]"},
	}

	result := content
	for _, def := range defs {
		result = def.pattern.ReplaceAllString(result, def.replacement)
	}
	return result
}

// detectContent finds content policy violations.
func (e *Engine) detectContent(content string) []Violation {
	var violations []Violation
	for i, pattern := range e.contentPatterns {
		if pattern.MatchString(content) {
			violations = append(violations, Violation{
				Rule:     fmt.Sprintf("content_policy_%d", i),
				Severity: SeverityHigh,
			})
		}
	}
	return violations
}

// detectBlockedWords finds blocked words/phrases in content.
func (e *Engine) detectBlockedWords(content string) []Violation {
	var violations []Violation
	lower := strings.ToLower(content)
	for _, word := range e.blockedWords {
		if strings.Contains(lower, strings.ToLower(word)) {
			violations = append(violations, Violation{
				Rule:     fmt.Sprintf("blocked_word:%s", word),
				Severity: SeverityHigh,
			})
		}
	}
	return violations
}

// maskString replaces the middle characters of a string with asterisks.
func maskString(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

// PolicyList returns the list of configured policies.
func (e *Engine) PolicyList() []Policy {
	e.mu.RLock()
	defer e.mu.RUnlock()
	result := make([]Policy, len(e.policies))
	copy(result, e.policies)
	return result
}

// MarshalJSON implements json.Marshaler for Severity.
func (s Severity) MarshalJSON() ([]byte, error) {
	switch s {
	case SeverityLow:
		return json.Marshal("low")
	case SeverityMedium:
		return json.Marshal("medium")
	case SeverityHigh:
		return json.Marshal("high")
	default:
		return json.Marshal("unknown")
	}
}
