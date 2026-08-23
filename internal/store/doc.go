// Package store implements durable thread persistence and analytics storage.
//
// Supports SQLite (embedded, zero-CGO via modernc.org/sqlite) for personal
// profiles and PostgreSQL for team/enterprise deployments. SQLite runs
// in WAL mode with busy_timeout for safe concurrent access.
package store
