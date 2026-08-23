package enterprise

import (
	"encoding/json"
	"net/http"
	"sync"
)

// Permission represents a single permission.
type Permission string

const (
	PermissionAgentRead    Permission = "agent:read"
	PermissionAgentWrite   Permission = "agent:write"
	PermissionAgentRun     Permission = "agent:run"
	PermissionThreadRead   Permission = "thread:read"
	PermissionThreadWrite  Permission = "thread:write"
	PermissionThreadDelete Permission = "thread:delete"
	PermissionToolRead     Permission = "tool:read"
	PermissionToolWrite    Permission = "tool:write"
	PermissionAnalytics    Permission = "analytics:read"
	PermissionConfigRead   Permission = "config:read"
	PermissionConfigWrite  Permission = "config:write"
	PermissionUserRead     Permission = "user:read"
	PermissionUserManage   Permission = "user:manage"
	PermissionAuditRead    Permission = "audit:read"
)

// Role represents a named role with associated permissions.
type Role struct {
	Name        string       `json:"name"`
	Permissions []Permission `json:"permissions"`
	Description string       `json:"description,omitempty"`
}

// RBACManager manages roles and permission checks.
type RBACManager struct {
	roles map[string]*Role
	mu    sync.RWMutex
}

// NewRBACManager creates a new RBAC manager with default roles.
func NewRBACManager() *RBACManager {
	m := &RBACManager{
		roles: make(map[string]*Role),
	}

	// Register default roles.
	m.RegisterRole(&Role{
		Name:        "admin",
		Permissions: []Permission{
			PermissionAgentRead, PermissionAgentWrite, PermissionAgentRun,
			PermissionThreadRead, PermissionThreadWrite, PermissionThreadDelete,
			PermissionToolRead, PermissionToolWrite,
			PermissionAnalytics, PermissionConfigRead, PermissionConfigWrite,
			PermissionUserRead, PermissionUserManage, PermissionAuditRead,
		},
		Description: "Full system access",
	})

	m.RegisterRole(&Role{
		Name:        "operator",
		Permissions: []Permission{
			PermissionAgentRead, PermissionAgentRun,
			PermissionThreadRead, PermissionThreadWrite,
			PermissionToolRead,
			PermissionAnalytics,
		},
		Description: "Can run agents and manage threads",
	})

	m.RegisterRole(&Role{
		Name:        "viewer",
		Permissions: []Permission{
			PermissionAgentRead,
			PermissionThreadRead,
			PermissionAnalytics,
		},
		Description: "Read-only access",
	})

	return m
}

// RegisterRole registers a new role.
func (m *RBACManager) RegisterRole(role *Role) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.roles[role.Name] = role
}

// GetRole returns a role by name.
func (m *RBACManager) GetRole(name string) (*Role, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	role, ok := m.roles[name]
	return role, ok
}

// ListRoles returns all registered roles.
func (m *RBACManager) ListRoles() []*Role {
	m.mu.RLock()
	defer m.mu.RUnlock()
	roles := make([]*Role, 0, len(m.roles))
	for _, r := range m.roles {
		roles = append(roles, r)
	}
	return roles
}

// HasPermission checks if a set of roles includes a specific permission.
func (m *RBACManager) HasPermission(userRoles []Permission, required Permission) bool {
	for _, role := range userRoles {
		if role == required {
			return true
		}
	}
	return false
}

// CheckPermission checks if a user with the given roles has a specific permission.
func (m *RBACManager) CheckPermission(userRoles []string, required Permission) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, roleName := range userRoles {
		role, ok := m.roles[roleName]
		if !ok {
			continue
		}
		for _, p := range role.Permissions {
			if p == required {
				return true
			}
		}
	}
	return false
}

// Middleware returns an HTTP middleware that enforces RBAC.
func (m *RBACManager) Middleware(requiredPermission Permission, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get session from context (requires SSO middleware to run first).
		session, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, `{"error":"unauthorized","message":"no session found"}`, http.StatusUnauthorized)
			return
		}

		if !m.CheckPermission(session.Roles, requiredPermission) {
			http.Error(w, `{"error":"forbidden","message":"insufficient permissions"}`, http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// WithPermission wraps an HTTP handler with RBAC check.
func (m *RBACManager) WithPermission(permission Permission, handler http.HandlerFunc) http.Handler {
	return m.Middleware(permission, http.HandlerFunc(handler))
}

// UserPermissions returns all permissions for a set of roles.
func (m *RBACManager) UserPermissions(roles []string) []Permission {
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := make(map[Permission]bool)
	var perms []Permission

	for _, roleName := range roles {
		role, ok := m.roles[roleName]
		if !ok {
			continue
		}
		for _, p := range role.Permissions {
			if !seen[p] {
				seen[p] = true
				perms = append(perms, p)
			}
		}
	}
	return perms
}

// MarshalJSON implements json.Marshaler for RBACManager.
func (m *RBACManager) MarshalJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.Marshal(m.roles)
}
