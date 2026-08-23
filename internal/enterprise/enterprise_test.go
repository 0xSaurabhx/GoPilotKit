package enterprise

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// --- SSO Tests ---

func TestNewSSOManager(t *testing.T) {
	cfg := SSOConfig{
		Enabled:      true,
		Provider:     SSOProviderGitHub,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		Domains:      []string{"example.com"},
	}

	mgr := NewSSOManager(cfg)
	if mgr == nil {
		t.Fatal("expected non-nil SSO manager")
	}
}

func TestSSOManager_InitiateLogin(t *testing.T) {
	cfg := SSOConfig{
		Enabled:      true,
		Provider:     SSOProviderGitHub,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
	}

	mgr := NewSSOManager(cfg)
	authURL, state, err := mgr.InitiateLogin()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if authURL == "" {
		t.Error("expected non-empty auth URL")
	}
	if state == "" {
		t.Error("expected non-empty state")
	}
}

func TestSSOManager_UnsupportedProvider(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: "unsupported",
	}

	mgr := NewSSOManager(cfg)
	_, _, err := mgr.InitiateLogin()
	if err == nil {
		t.Error("expected error for unsupported provider")
	}
}

func TestSSOManager_CreateSession_DomainValidation(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: SSOProviderGitHub,
		Domains:  []string{"example.com"},
	}

	mgr := NewSSOManager(cfg)

	// Should succeed with allowed domain.
	session, err := mgr.createSession("user@example.com", "Test User", "user-123", []string{"user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Email != "user@example.com" {
		t.Errorf("expected email user@example.com, got %s", session.Email)
	}

	// Should fail with disallowed domain.
	_, err = mgr.createSession("user@other.com", "Other User", "user-456", []string{"user"})
	if err == nil {
		t.Error("expected error for disallowed domain")
	}
}

func TestSSOManager_ValidateToken(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: SSOProviderGitHub,
	}

	mgr := NewSSOManager(cfg)

	// Create a session.
	session, err := mgr.createSession("user@example.com", "Test User", "user-123", []string{"user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Validate the token.
	validated, err := mgr.ValidateToken(session.Token)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if validated.UserID != session.UserID {
		t.Errorf("expected user ID %s, got %s", session.UserID, validated.UserID)
	}

	// Validate with invalid token.
	_, err = mgr.ValidateToken("invalid-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestSSOManager_ValidateToken_Expired(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: SSOProviderGitHub,
	}

	mgr := NewSSOManager(cfg)

	// Create a session with expired token.
	mgr.mu.Lock()
	token, _ := generateToken()
	mgr.sessions[token] = &SSOSession{
		UserID:    "user-123",
		Email:     "user@example.com",
		Token:     token,
		ExpiresAt: time.Now().UTC().Add(-1 * time.Hour), // Already expired
	}
	mgr.mu.Unlock()

	// Validate should fail.
	_, err := mgr.ValidateToken(token)
	if err == nil {
		t.Error("expected error for expired token")
	}
}

func TestSSOManager_RevokeToken(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: SSOProviderGitHub,
	}

	mgr := NewSSOManager(cfg)

	// Create a session.
	session, err := mgr.createSession("user@example.com", "Test User", "user-123", []string{"user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Revoke the token.
	mgr.RevokeToken(session.Token)

	// Validate should fail.
	_, err = mgr.ValidateToken(session.Token)
	if err == nil {
		t.Error("expected error for revoked token")
	}
}

func TestSSOManager_Middleware(t *testing.T) {
	cfg := SSOConfig{
		Enabled:  true,
		Provider: SSOProviderGitHub,
	}

	mgr := NewSSOManager(cfg)

	// Create a session.
	session, err := mgr.createSession("user@example.com", "Test User", "user-123", []string{"user"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test handler that checks for session.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s, ok := SessionFromContext(r.Context())
		if !ok {
			http.Error(w, "no session", http.StatusUnauthorized)
			return
		}
		w.Write([]byte(s.Email))
	})

	// Test with valid token.
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	rr := httptest.NewRecorder()

	mgr.Middleware(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Test without token.
	req = httptest.NewRequest("GET", "/test", nil)
	rr = httptest.NewRecorder()

	mgr.Middleware(handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}
}

func TestExtractBearerToken(t *testing.T) {
	tests := []struct {
		name     string
		auth     string
		expected string
	}{
		{"valid bearer", "Bearer abc123", "abc123"},
		{"no bearer", "Basic abc123", ""},
		{"empty", "", ""},
		{"bearer only", "Bearer ", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			result := extractBearerToken(req)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

// --- RBAC Tests ---

func TestNewRBACManager(t *testing.T) {
	mgr := NewRBACManager()
	if mgr == nil {
		t.Fatal("expected non-nil RBAC manager")
	}

	roles := mgr.ListRoles()
	if len(roles) != 3 {
		t.Errorf("expected 3 default roles, got %d", len(roles))
	}
}

func TestRBACManager_GetRole(t *testing.T) {
	mgr := NewRBACManager()

	role, ok := mgr.GetRole("admin")
	if !ok {
		t.Fatal("expected to find admin role")
	}
	if role.Name != "admin" {
		t.Errorf("expected role name admin, got %s", role.Name)
	}

	_, ok = mgr.GetRole("nonexistent")
	if ok {
		t.Error("expected not to find nonexistent role")
	}
}

func TestRBACManager_CheckPermission(t *testing.T) {
	mgr := NewRBACManager()

	// Admin should have all permissions.
	if !mgr.CheckPermission([]string{"admin"}, PermissionAgentRun) {
		t.Error("expected admin to have agent:run permission")
	}

	if !mgr.CheckPermission([]string{"admin"}, PermissionConfigWrite) {
		t.Error("expected admin to have config:write permission")
	}

	// Viewer should not have write permissions.
	if mgr.CheckPermission([]string{"viewer"}, PermissionAgentWrite) {
		t.Error("expected viewer not to have agent:write permission")
	}

	// Operator should have agent run but not config write.
	if !mgr.CheckPermission([]string{"operator"}, PermissionAgentRun) {
		t.Error("expected operator to have agent:run permission")
	}
	if mgr.CheckPermission([]string{"operator"}, PermissionConfigWrite) {
		t.Error("expected operator not to have config:write permission")
	}

	// Unknown role should not have permissions.
	if mgr.CheckPermission([]string{"unknown"}, PermissionAgentRead) {
		t.Error("expected unknown role not to have any permissions")
	}
}

func TestRBACManager_UserPermissions(t *testing.T) {
	mgr := NewRBACManager()

	perms := mgr.UserPermissions([]string{"viewer"})
	if len(perms) == 0 {
		t.Error("expected viewer to have some permissions")
	}

	// Admin should have more permissions than viewer.
	adminPerms := mgr.UserPermissions([]string{"admin"})
	if len(adminPerms) <= len(perms) {
		t.Error("expected admin to have more permissions than viewer")
	}
}

func TestRBACManager_Middleware(t *testing.T) {
	mgr := NewRBACManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	// Test without session.
	req := httptest.NewRequest("GET", "/test", nil)
	rr := httptest.NewRecorder()

	mgr.Middleware(PermissionAgentRead, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rr.Code)
	}

	// Test with session that has permission.
	ctx := ContextWithSession(context.Background(), &SSOSession{
		UserID: "user-123",
		Roles:  []string{"admin"},
	})
	req = httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()

	mgr.Middleware(PermissionAgentRead, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rr.Code)
	}

	// Test with session that lacks permission.
	ctx = ContextWithSession(context.Background(), &SSOSession{
		UserID: "user-123",
		Roles:  []string{"viewer"},
	})
	req = httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctx)
	rr = httptest.NewRecorder()

	mgr.Middleware(PermissionConfigWrite, handler).ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rr.Code)
	}
}

// --- Audit Tests ---

func TestNewAuditLogger(t *testing.T) {
	cfg := DefaultAuditConfig()
	logger, err := NewAuditLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()
}

func TestAuditLogger_Log(t *testing.T) {
	cfg := AuditConfig{
		Enabled:     true,
		Output:      "stdout",
		MinSeverity: SeverityInfo,
	}

	logger, err := NewAuditLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	err = logger.Log(&AuditEvent{
		Type:     EventLogin,
		Severity: SeverityInfo,
		UserID:   "user-123",
		Action:   "login",
		Status:   "success",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuditLogger_LogAuth(t *testing.T) {
	cfg := DefaultAuditConfig()
	logger, err := NewAuditLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	err = logger.LogAuth(EventLogin, "user-123", "user@example.com", "127.0.0.1", "success", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuditLogger_LogAgent(t *testing.T) {
	cfg := DefaultAuditConfig()
	logger, err := NewAuditLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	err = logger.LogAgent(EventAgentRun, "user-123", "personal-assistant", "thread-123", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAuditLogger_Disabled(t *testing.T) {
	cfg := AuditConfig{Enabled: false}
	logger, err := NewAuditLogger(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer logger.Close()

	// Should not error when disabled.
	err = logger.Log(&AuditEvent{
		Type:     EventLogin,
		Severity: SeverityInfo,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- Retention Tests ---

type mockDeleter struct {
	deleted int64
	err     error
}

func (d *mockDeleter) DeleteBefore(resource string, before time.Time) (int64, error) {
	return d.deleted, d.err
}

func TestNewRetentionManager(t *testing.T) {
	cfg := DefaultRetentionConfig()
	deleter := &mockDeleter{}

	mgr := NewRetentionManager(cfg, deleter)
	if mgr == nil {
		t.Fatal("expected non-nil retention manager")
	}
}

func TestRetentionManager_Enforce_Disabled(t *testing.T) {
	cfg := RetentionConfig{Enabled: false}
	deleter := &mockDeleter{}

	mgr := NewRetentionManager(cfg, deleter)
	err := mgr.Enforce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetentionManager_Enforce_Enabled(t *testing.T) {
	cfg := DefaultRetentionConfig()
	cfg.Enabled = true
	deleter := &mockDeleter{deleted: 10}

	mgr := NewRetentionManager(cfg, deleter)
	err := mgr.Enforce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRetentionManager_GetPolicies(t *testing.T) {
	cfg := DefaultRetentionConfig()
	deleter := &mockDeleter{}

	mgr := NewRetentionManager(cfg, deleter)
	policies := mgr.GetPolicies()

	if len(policies) == 0 {
		t.Error("expected at least one policy")
	}
}

func TestRetentionManager_GetPolicy(t *testing.T) {
	cfg := DefaultRetentionConfig()
	deleter := &mockDeleter{}

	mgr := NewRetentionManager(cfg, deleter)

	policy, ok := mgr.GetPolicy("threads")
	if !ok {
		t.Error("expected to find threads policy")
	}
	if policy.Name != "threads" {
		t.Errorf("expected policy name threads, got %s", policy.Name)
	}
}

// --- Middleware Tests ---

func TestMiddleware_Chain(t *testing.T) {
	sso := NewSSOManager(SSOConfig{Enabled: true, Provider: SSOProviderGitHub})
	rbac := NewRBACManager()
	logger, _ := NewAuditLogger(DefaultAuditConfig())
	defer logger.Close()

	middleware := NewMiddleware(sso, rbac, logger)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	chain := middleware.Chain(handler)
	if chain == nil {
		t.Fatal("expected non-nil chain")
	}
}

func TestMiddleware_permissionForRequest(t *testing.T) {
	m := &Middleware{}

	tests := []struct {
		method   string
		path     string
		expected Permission
	}{
		{"GET", "/health", ""},
		{"GET", "/agent/123", PermissionAgentRead},
		{"POST", "/run", PermissionAgentRun},
		{"GET", "/thread/123", PermissionThreadRead},
		{"DELETE", "/thread/123", PermissionThreadDelete},
		{"GET", "/analytics", PermissionAnalytics},
		{"GET", "/config", PermissionConfigRead},
		{"POST", "/config", PermissionConfigWrite},
		{"GET", "/users", PermissionUserRead},
		{"DELETE", "/users/123", PermissionUserManage},
		{"GET", "/audit", PermissionAuditRead},
	}

	for _, tt := range tests {
		t.Run(tt.method+" "+tt.path, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			result := m.permissionForRequest(req)
			if result != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, result)
			}
		})
	}
}
