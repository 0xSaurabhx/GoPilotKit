// Package cache implements semantic caching for LLM responses.
//
// Caches semantically similar queries to avoid redundant LLM calls,
// providing 30-60% cost savings on repeated or similar queries.
package cache
