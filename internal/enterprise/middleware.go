package enterprise

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Middleware holds all enterprise middleware components.
type Middleware struct {
	sso  *SSOManager
	rbac *RBACManager
	audit *AuditLogger
}

// NewMiddleware creates a new enterprise middleware.
func NewMiddleware(sso *SSOManager, rbac *RBACManager, audit *AuditLogger) *Middleware {
	return &Middleware{
		sso:   sso,
		rbac:  rbac,
		audit: audit,
	}
}

// Chain returns an HTTP middleware that applies SSO, RBAC, and audit logging.
func (m *Middleware) Chain(next http.Handler) http.Handler {
	handler := next

	// Apply audit logging first (outermost).
	if m.audit != nil {
		handler = m.auditLogging(handler)
	}

	// Apply RBAC if configured.
	if m.rbac != nil {
		handler = m.rbacEnforcement(handler)
	}

	// Apply SSO if configured (innermost).
	if m.sso != nil {
		handler = m.ssoAuth(handler)
	}

	return handler
}

// RequirePermission returns middleware that requires a specific permission.
func (m *Middleware) RequirePermission(permission Permission, handler http.HandlerFunc) http.Handler {
	return m.rbac.WithPermission(permission, handler)
}

// ssoAuth applies SSO authentication.
func (m *Middleware) ssoAuth(next http.Handler) http.Handler {
	return m.sso.Middleware(next)
}

// rbacEnforcement returns middleware that enforces RBAC.
func (m *Middleware) rbacEnforcement(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get session from context.
		session, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized","message":"no session found"}`, http.StatusUnauthorized)
			return
		}

		// Determine required permission based on method and path.
		permission := m.permissionForRequest(r)
		if permission == "" {
			// No specific permission required, allow.
			next.ServeHTTP(w, r)
			return
		}

		if !m.rbac.CheckPermission(session.Roles, permission) {
			// Log the denied attempt.
			if m.audit != nil {
				_ = m.audit.Log(&AuditEvent{
					Type:     EventAgentRun,
					Severity: SeverityWarning,
					UserID:   session.UserID,
					Action:   "permission_denied",
					Resource: r.URL.Path,
					Details: map[string]string{
						"required": string(permission),
						"method":   r.Method,
					},
					Status: "denied",
				})
			}

			http.Error(w, `{"error":"forbidden","message":"insufficient permissions"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// auditLogging returns middleware that logs all requests.
func (m *Middleware) auditLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Capture response status.
		wrapped := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(wrapped, r)

		// Log the request.
		duration := time.Since(start)

		session, _ := SessionFromContext(r.Context())

		event := &AuditEvent{
			Type:     EventDataRead,
			Severity: SeverityInfo,
			Action:   r.Method,
			Resource: r.URL.Path,
			IP:       r.RemoteAddr,
			UserAgent: r.UserAgent(),
			Details: map[string]string{
				"status_code": fmt.Sprintf("%d", wrapped.statusCode),
				"duration_ms": fmt.Sprintf("%d", duration.Milliseconds()),
			},
		}

		if session != nil {
			event.UserID = session.UserID
			event.Email = session.Email
		}

		_ = m.audit.Log(event)
	})
}

// permissionForRequest determines the required permission for a request.
func (m *Middleware) permissionForRequest(r *http.Request) Permission {
	path := r.URL.Path
	method := r.Method

	// Agent endpoints.
	if strings.HasPrefix(path, "/agent/") || strings.HasPrefix(path, "/run") {
		if method == "POST" || method == "PUT" {
			return PermissionAgentRun
		}
		return PermissionAgentRead
	}

	// Thread endpoints.
	if strings.HasPrefix(path, "/thread/") {
		if method == "DELETE" {
			return PermissionThreadDelete
		}
		if method == "POST" || method == "PUT" {
			return PermissionThreadWrite
		}
		return PermissionThreadRead
	}

	// Tool endpoints.
	if strings.HasPrefix(path, "/tool/") {
		if method == "POST" || method == "PUT" {
			return PermissionToolWrite
		}
		return PermissionToolRead
	}

	// Analytics endpoints.
	if strings.HasPrefix(path, "/analytics") {
		return PermissionAnalytics
	}

	// Config endpoints.
	if strings.HasPrefix(path, "/config") {
		if method == "POST" || method == "PUT" {
			return PermissionConfigWrite
		}
		return PermissionConfigRead
	}

	// User management endpoints.
	if strings.HasPrefix(path, "/user") || strings.HasPrefix(path, "/users") {
		if method == "POST" || method == "PUT" || method == "DELETE" {
			return PermissionUserManage
		}
		return PermissionUserRead
	}

	// Audit endpoints.
	if strings.HasPrefix(path, "/audit") {
		return PermissionAuditRead
	}

	// Health and public endpoints - no permission required.
	if path == "/health" || path == "/healthz" || path == "/readyz" {
		return ""
	}

	return PermissionAgentRead
}

// statusRecorder captures the response status code.
type statusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// sessionKey is the context key for SSO session.
type sessionKey struct{}

// ContextWithSession adds an SSO session to the context.
func ContextWithSession(ctx context.Context, session *SSOSession) context.Context {
	return context.WithValue(ctx, sessionKey{}, session)
}

// SessionFromContext extracts the SSO session from the context.
func SessionFromContext(ctx context.Context) (*SSOSession, bool) {
	session, ok := ctx.Value(sessionKey{}).(*SSOSession)
	return session, ok
}
