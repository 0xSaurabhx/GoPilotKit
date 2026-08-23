package provider

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota // Normal operation
	CircuitOpen                         // Failing, skip this provider
	CircuitHalfOpen                     // Testing if provider recovered
)

// CircuitBreaker wraps a provider with retry logic and state tracking.
type CircuitBreaker struct {
	provider      Provider
	state         CircuitState
	failures      int
	successes     int
	lastFailure   time.Time
	maxFailures   int
	resetTimeout  time.Duration
	mu            sync.Mutex
}

// NewCircuitBreaker creates a new circuit breaker for a provider.
func NewCircuitBreaker(provider Provider, maxFailures int, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		provider:     provider,
		state:        CircuitClosed,
		maxFailures:  maxFailures,
		resetTimeout: resetTimeout,
	}
}

func (cb *CircuitBreaker) ID() string   { return cb.provider.ID() }
func (cb *CircuitBreaker) Type() string { return cb.provider.Type() }

func (cb *CircuitBreaker) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	if !cb.allowRequest() {
		return nil, fmt.Errorf("circuit open for provider %s", cb.provider.ID())
	}

	resp, err := cb.provider.ChatCompletion(ctx, req)
	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	cb.recordSuccess()
	return resp, nil
}

func (cb *CircuitBreaker) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	if !cb.allowRequest() {
		return nil, fmt.Errorf("circuit open for provider %s", cb.provider.ID())
	}

	chunks, err := cb.provider.ChatCompletionStream(ctx, req)
	if err != nil {
		cb.recordFailure()
		return nil, err
	}

	cb.recordSuccess()
	return chunks, nil
}

func (cb *CircuitBreaker) Close() error {
	return cb.provider.Close()
}

func (cb *CircuitBreaker) allowRequest() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.state == CircuitClosed {
		return true
	}

	if cb.state == CircuitOpen {
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = CircuitHalfOpen
			cb.successes = 0
			log.Printf("circuit breaker: %s → half-open", cb.provider.ID())
			return true
		}
		return false
	}

	// Half-open: allow requests
	return true
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()
	cb.successes = 0

	if cb.failures >= cb.maxFailures {
		cb.state = CircuitOpen
		log.Printf("circuit breaker: %s → open (failures: %d)", cb.provider.ID(), cb.failures)
	}
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.successes++
	if cb.state == CircuitHalfOpen && cb.successes >= 3 {
		cb.state = CircuitClosed
		cb.failures = 0
		log.Printf("circuit breaker: %s → closed (recovered)", cb.provider.ID())
	}
}

// GetState returns the current circuit state.
func (cb *CircuitBreaker) GetState() CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// --- Fallback Provider ---

// FallbackProvider tries providers in order, falling back on error.
type FallbackProvider struct {
	providers []Provider
	current   int
	mu        sync.Mutex
}

// NewFallbackProvider creates a provider that tries multiple providers in order.
func NewFallbackProvider(providers ...Provider) *FallbackProvider {
	return &FallbackProvider{
		providers: providers,
	}
}

func (fp *FallbackProvider) ID() string {
	fp.mu.Lock()
	defer fp.mu.Unlock()
	if fp.current < len(fp.providers) {
		return fp.providers[fp.current].ID()
	}
	return "fallback"
}

func (fp *FallbackProvider) Type() string { return "fallback" }

func (fp *FallbackProvider) ChatCompletion(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	fp.mu.Lock()
	startIdx := fp.current
	fp.mu.Unlock()

	var lastErr error
	for i := startIdx; i < len(fp.providers); i++ {
		p := fp.providers[i]
		log.Printf("fallback: trying provider %s (%s)", p.ID(), p.Type())

		resp, err := p.ChatCompletion(ctx, req)
		if err != nil {
			log.Printf("fallback: provider %s failed: %v", p.ID(), err)
			lastErr = err

			// Non-retryable errors stop the chain immediately
			if pe, ok := err.(*ProviderError); ok {
				if !pe.Retryable {
					return nil, fmt.Errorf("provider %s: non-retryable error: %w", p.ID(), err)
				}
				fp.mu.Lock()
				if fp.current == i {
					fp.current = i + 1
				}
				fp.mu.Unlock()
				continue
			}
			if _, ok := err.(*RateLimitError); ok {
				fp.mu.Lock()
				if fp.current == i {
					fp.current = i + 1
				}
				fp.mu.Unlock()
				continue
			}
			// Unknown error type — stop
			return nil, fmt.Errorf("provider %s: unexpected error: %w", p.ID(), err)
		}

		// Success — reset to this provider for future requests
		fp.mu.Lock()
		fp.current = i
		fp.mu.Unlock()

		return resp, nil
	}

	return nil, fmt.Errorf("all providers failed, last error: %w", lastErr)
}

func (fp *FallbackProvider) ChatCompletionStream(ctx context.Context, req *ChatRequest) (<-chan ChatChunk, error) {
	fp.mu.Lock()
	startIdx := fp.current
	fp.mu.Unlock()

	var lastErr error
	for i := startIdx; i < len(fp.providers); i++ {
		p := fp.providers[i]
		log.Printf("fallback: trying provider %s (%s) for stream", p.ID(), p.Type())

		chunks, err := p.ChatCompletionStream(ctx, req)
		if err != nil {
			log.Printf("fallback: provider %s stream failed: %v", p.ID(), err)
			lastErr = err

			if pe, ok := err.(*ProviderError); ok {
				if !pe.Retryable {
					return nil, fmt.Errorf("provider %s: non-retryable stream error: %w", p.ID(), err)
				}
				fp.mu.Lock()
				if fp.current == i {
					fp.current = i + 1
				}
				fp.mu.Unlock()
				continue
			}
			return nil, fmt.Errorf("provider %s: unexpected stream error: %w", p.ID(), err)
		}

		fp.mu.Lock()
		fp.current = i
		fp.mu.Unlock()

		return chunks, nil
	}

	return nil, fmt.Errorf("all providers failed for stream, last error: %w", lastErr)
}

func (fp *FallbackProvider) Close() error {
	for _, p := range fp.providers {
		p.Close()
	}
	return nil
}
