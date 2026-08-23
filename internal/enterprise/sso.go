// Package enterprise implements enterprise security features.
//
// Provides SSO/SAML integration, role-based access control (RBAC),
// audit logging, and data retention policies for enterprise deployments.
package enterprise

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SSOProvider represents the type of SSO provider.
type SSOProvider string

const (
	SSOProviderOIDC  SSOProvider = "oidc"
	SSOProviderSAML  SSOProvider = "saml"
	SSOProviderGitHub SSOProvider = "github"
)

// SSOConfig holds SSO configuration.
type SSOConfig struct {
	Enabled      bool        `yaml:"enabled"`
	Provider     SSOProvider `yaml:"provider"`
	Issuer       string      `yaml:"issuer"`
	ClientID     string      `yaml:"client_id"`
	ClientSecret string      `yaml:"client_secret"`
	RedirectURL  string      `yaml:"redirect_url"`
	Domains      []string    `yaml:"domains,omitempty"` // Allowed email domains
}

// Claims represents the user claims from the identity provider.
type Claims struct {
	Subject string            `json:"sub"`
	Email   string            `json:"email"`
	Name    string            `json:"name"`
	Roles   []string          `json:"roles,omitempty"`
	OrgID   string            `json:"org_id,omitempty"`
	Extra   map[string]string `json:"extra,omitempty"`
}

// SSOSession represents an authenticated session.
type SSOSession struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Roles     []string  `json:"roles"`
	OrgID     string    `json:"org_id,omitempty"`
	Provider  string    `json:"provider"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	IssuedAt  time.Time `json:"issued_at"`
}

// SSOManager handles SSO authentication flows.
type SSOManager struct {
	config     SSOConfig
	sessions   map[string]*SSOSession // token -> session
	mu         sync.RWMutex
	httpClient *http.Client
}

// NewSSOManager creates a new SSO manager.
func NewSSOManager(cfg SSOConfig) *SSOManager {
	return &SSOManager{
		config:   cfg,
		sessions: make(map[string]*SSOSession),
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// InitiateLogin starts the SSO login flow and returns the authorization URL.
func (m *SSOManager) InitiateLogin() (string, string, error) {
	state, err := generateState()
	if err != nil {
		return "", "", fmt.Errorf("generating state: %w", err)
	}

	var authURL string
	switch m.config.Provider {
	case SSOProviderOIDC:
		authURL = fmt.Sprintf("%s/authorize?client_id=%s&redirect_uri=%s&response_type=code&scope=openid+email+profile&state=%s",
			m.config.Issuer,
			m.config.ClientID,
			m.config.RedirectURL,
			state,
		)
	case SSOProviderGitHub:
		authURL = fmt.Sprintf("https://github.com/login/oauth/authorize?client_id=%s&redirect_uri=%s&scope=user:email&state=%s",
			m.config.ClientID,
			m.config.RedirectURL,
			state,
		)
	default:
		return "", "", fmt.Errorf("unsupported SSO provider: %s", m.config.Provider)
	}

	return authURL, state, nil
}

// HandleCallback processes the OAuth callback and creates a session.
func (m *SSOManager) HandleCallback(ctx context.Context, code, state string) (*SSOSession, error) {
	if m.config.Provider == SSOProviderGitHub {
		return m.handleGitHubCallback(ctx, code)
	}
	return m.handleOIDCCallback(ctx, code)
}

// handleGitHubCallback processes GitHub OAuth callback.
func (m *SSOManager) handleGitHubCallback(ctx context.Context, code string) (*SSOSession, error) {
	// Exchange code for access token.
	tokenURL := "https://github.com/login/oauth/access_token"
	payload := fmt.Sprintf(`{"client_id":"%s","client_secret":"%s","code":"%s"}`,
		m.config.ClientID, m.config.ClientSecret, code)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("GitHub OAuth error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// Fetch user info.
	userReq, err := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, fmt.Errorf("creating user request: %w", err)
	}
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)

	userResp, err := m.httpClient.Do(userReq)
	if err != nil {
		return nil, fmt.Errorf("fetching user info: %w", err)
	}
	defer userResp.Body.Close()

	var user struct {
		ID    int    `json:"id"`
		Login string `json:"login"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decoding user info: %w", err)
	}

	// Fetch user emails if primary email is empty.
	if user.Email == "" {
		emailReq, _ := http.NewRequestWithContext(ctx, "GET", "https://api.github.com/user/emails", nil)
		emailReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		emailResp, err := m.httpClient.Do(emailReq)
		if err == nil {
			defer emailResp.Body.Close()
			var emails []struct {
				Email   string `json:"email"`
				Primary bool   `json:"primary"`
			}
			if json.NewDecoder(emailResp.Body).Decode(&emails) == nil {
				for _, e := range emails {
					if e.Primary {
						user.Email = e.Email
						break
					}
				}
			}
		}
	}

	return m.createSession(user.Email, user.Name, fmt.Sprintf("github_%d", user.ID), []string{"user"})
}

// handleOIDCCallback processes OIDC OAuth callback.
func (m *SSOManager) handleOIDCCallback(ctx context.Context, code string) (*SSOSession, error) {
	// Exchange code for tokens.
	tokenURL := m.config.Issuer + "/token"
	data := fmt.Sprintf("grant_type=authorization_code&code=%s&client_id=%s&client_secret=%s&redirect_uri=%s",
		code, m.config.ClientID, m.config.ClientSecret, m.config.RedirectURL)

	req, err := http.NewRequestWithContext(ctx, "POST", tokenURL, strings.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("creating token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("exchanging code: %w", err)
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		IDToken      string `json:"id_token"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("decoding token response: %w", err)
	}
	if tokenResp.Error != "" {
		return nil, fmt.Errorf("OIDC error: %s - %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// Decode ID token claims (simplified; production should verify signature).
	claims, err := decodeIDToken(tokenResp.IDToken)
	if err != nil {
		return nil, fmt.Errorf("decoding ID token: %w", err)
	}

	return m.createSession(claims.Email, claims.Name, claims.Subject, claims.Roles)
}

// createSession creates a new authenticated session.
func (m *SSOManager) createSession(email, name, subject string, roles []string) (*SSOSession, error) {
	// Validate email domain if configured.
	if len(m.config.Domains) > 0 {
		domain := strings.SplitN(email, "@", 2)
		if len(domain) != 2 {
			return nil, fmt.Errorf("invalid email format: %s", email)
		}
		allowed := false
		for _, d := range m.config.Domains {
			if strings.EqualFold(domain[1], d) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("email domain %s not allowed", domain[1])
		}
	}

	token, err := generateToken()
	if err != nil {
		return nil, fmt.Errorf("generating session token: %w", err)
	}

	now := time.Now().UTC()
	session := &SSOSession{
		UserID:    subject,
		Email:     email,
		Name:      name,
		Roles:     roles,
		Provider:  string(m.config.Provider),
		Token:     token,
		ExpiresAt: now.Add(24 * time.Hour),
		IssuedAt:  now,
	}

	m.mu.Lock()
	m.sessions[token] = session
	m.mu.Unlock()

	return session, nil
}

// ValidateToken validates a session token and returns the session.
func (m *SSOManager) ValidateToken(token string) (*SSOSession, error) {
	m.mu.RLock()
	session, ok := m.sessions[token]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("invalid session token")
	}

	if time.Now().UTC().After(session.ExpiresAt) {
		m.mu.Lock()
		delete(m.sessions, token)
		m.mu.Unlock()
		return nil, fmt.Errorf("session expired")
	}

	return session, nil
}

// RevokeToken revokes a session token.
func (m *SSOManager) RevokeToken(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

// middleware returns an HTTP middleware that enforces SSO authentication.
func (m *SSOManager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for login/callback endpoints.
		if strings.HasPrefix(r.URL.Path, "/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}

		session, err := m.ValidateToken(token)
		if err != nil {
			http.Error(w, `{"error":"unauthorized","message":"`+err.Error()+`"}`, http.StatusUnauthorized)
			return
		}

		// Add session to context.
		ctx := ContextWithSession(r.Context(), session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// decodeIDToken decodes the claims from an OIDC ID token.
// In production, this should verify the token signature.
func decodeIDToken(idToken string) (*Claims, error) {
	parts := strings.Split(idToken, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid ID token format")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decoding token payload: %w", err)
	}

	var claims Claims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("parsing token claims: %w", err)
	}

	return &claims, nil
}

// extractBearerToken extracts the Bearer token from the Authorization header.
func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// generateState generates a random state parameter for OAuth.
func generateState() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// generateToken generates a random session token.
func generateToken() (string, error) {
	b := make([]byte, 48)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}
