package channels

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// MobileConfig configures the mobile channel adapter.
type MobileConfig struct {
	// Push notification providers.
	FCM   *FCMConfig   `json:"fcm,omitempty"`   // Firebase Cloud Messaging (Android)
	APNs  *APNsConfig  `json:"apns,omitempty"`  // Apple Push Notification service (iOS)

	// Delivery settings.
	Streaming bool `json:"streaming"` // Whether to push streaming chunks
	BatchSize int  `json:"batch_size"` // Number of chunks to batch before pushing
}

// FCMConfig configures Firebase Cloud Messaging.
type FCMConfig struct {
	ProjectID string `json:"project_id"`
	APIKey    string `json:"api_key"`
}

// APNsConfig configures Apple Push Notification service.
type APNsConfig struct {
	KeyID      string `json:"key_id"`
	TeamID     string `json:"team_id"`
	BundleID   string `json:"bundle_id"`
	PrivateKey string `json:"private_key"` // Path to .p8 key file
	Production bool   `json:"production"`  // Use production APNs
}

// DeviceToken represents a registered device for push notifications.
type DeviceToken struct {
	UserID  string `json:"user_id"`
	Token   string `json:"token"`
	Platform string `json:"platform"` // "ios" or "android"
}

// MobileAdapter handles the mobile channel with push notifications.
type MobileAdapter struct {
	config    MobileConfig
	deviceMu  sync.RWMutex
	devices   map[string][]DeviceToken // userID → device tokens
	client    *http.Client
}

// NewMobileAdapter creates a new mobile channel adapter.
func NewMobileAdapter(cfg MobileConfig) *MobileAdapter {
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 5
	}
	return &MobileAdapter{
		config:  cfg,
		devices: make(map[string][]DeviceToken),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

func (a *MobileAdapter) Type() ChannelType { return ChannelMobile }

// RegisterDevice registers a device token for push notifications.
func (a *MobileAdapter) RegisterDevice(userID, token, platform string) {
	a.deviceMu.Lock()
	defer a.deviceMu.Unlock()

	// Remove existing token for this user if present.
	tokens := a.devices[userID]
	for i, t := range tokens {
		if t.Token == token {
			tokens[i].Platform = platform
			a.devices[userID] = tokens
			return
		}
	}

	a.devices[userID] = append(a.devices[userID], DeviceToken{
		UserID:   userID,
		Token:    token,
		Platform: platform,
	})
}

// UnregisterDevice removes a device token.
func (a *MobileAdapter) UnregisterDevice(userID, token string) {
	a.deviceMu.Lock()
	defer a.deviceMu.Unlock()

	tokens := a.devices[userID]
	for i, t := range tokens {
		if t.Token == token {
			a.devices[userID] = append(tokens[:i], tokens[i+1:]...)
			return
		}
	}
}

// GetDeviceTokens returns all device tokens for a user.
func (a *MobileAdapter) GetDeviceTokens(userID string) []DeviceToken {
	a.deviceMu.RLock()
	defer a.deviceMu.RUnlock()
	tokens := a.devices[userID]
	result := make([]DeviceToken, len(tokens))
	copy(result, tokens)
	return result
}

func (a *MobileAdapter) TranslateInbound(raw []byte) (*InboundMessage, error) {
	// Mobile app format: {"content": "...", "agent": "...", "user_id": "...", "thread_id": "..."}
	var msg struct {
		Content  string            `json:"content"`
		Agent    string            `json:"agent"`
		UserID   string            `json:"user_id"`
		ThreadID string            `json:"thread_id,omitempty"`
		Meta     map[string]string `json:"meta,omitempty"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("mobile: invalid message format: %w", err)
	}

	return &InboundMessage{
		Channel:   ChannelMobile,
		Agent:     msg.Agent,
		UserID:    msg.UserID,
		Content:   msg.Content,
		ThreadID:  msg.ThreadID,
		Metadata:  msg.Meta,
		Timestamp: time.Now().UTC(),
	}, nil
}

func (a *MobileAdapter) TranslateOutbound(msg *OutboundMessage) ([]byte, error) {
	// Mobile app receives a clean JSON payload.
	msg.Channel = ChannelMobile
	return json.Marshal(map[string]interface{}{
		"type":       "message",
		"id":         msg.ID,
		"agent":      msg.Agent,
		"content":    msg.Content,
		"thread_id":  msg.ThreadID,
		"attachments": msg.Attachments,
		"metadata":   msg.Metadata,
		"timestamp":  msg.Timestamp.Format(time.RFC3339),
	})
}

func (a *MobileAdapter) FormatStreamingChunk(agent, chunk string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "text_chunk",
		"agent": agent,
		"delta": chunk,
	})
}

func (a *MobileAdapter) FormatToolCall(toolName string, args json.RawMessage) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":      "tool_call",
		"tool_name": toolName,
		"arguments": args,
	})
}

func (a *MobileAdapter) FormatError(errMsg string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": errMsg,
	})
}

func (a *MobileAdapter) SupportsStreaming() bool { return true }

// --- Push Notification Methods ---

// PushNotification represents a push notification payload.
type PushNotification struct {
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	Data     map[string]string `json:"data,omitempty"`
	Badge    int               `json:"badge,omitempty"`
	Sound    string            `json:"sound,omitempty"`
	ImageURL string            `json:"image_url,omitempty"`
}

// SendPush sends a push notification to all devices of a user.
func (a *MobileAdapter) SendPush(userID string, notif PushNotification) error {
	if notif.Sound == "" {
		notif.Sound = "default"
	}

	tokens := a.GetDeviceTokens(userID)
	if len(tokens) == 0 {
		return nil // No registered devices.
	}

	var lastErr error
	for _, dt := range tokens {
		switch dt.Platform {
		case "android":
			if a.config.FCM != nil {
				if err := a.sendFCM(dt.Token, notif); err != nil {
					lastErr = fmt.Errorf("FCM push to %s: %w", dt.Token[:10], err)
				}
			}
		case "ios":
			if a.config.APNs != nil {
				if err := a.sendAPNs(dt.Token, notif); err != nil {
					lastErr = fmt.Errorf("APNs push to %s: %w", dt.Token[:10], err)
				}
			}
		}
	}

	return lastErr
}

// PushAgentResponse pushes the complete agent response via push notification.
func (a *MobileAdapter) PushAgentResponse(userID, agent, content string) error {
	title := fmt.Sprintf("New message from %s", agent)
	if len(content) > 200 {
		content = content[:197] + "..."
	}

	return a.SendPush(userID, PushNotification{
		Title: title,
		Body:  content,
		Data: map[string]string{
			"agent": agent,
			"type":  "agent_response",
		},
		Badge: 1,
	})
}

// PushToolResult pushes a tool execution result notification.
func (a *MobileAdapter) PushToolResult(userID, toolName, result string) error {
	if len(result) > 100 {
		result = result[:97] + "..."
	}

	return a.SendPush(userID, PushNotification{
		Title: fmt.Sprintf("Tool result: %s", toolName),
		Body:  result,
		Data: map[string]string{
			"tool": toolName,
			"type": "tool_result",
		},
	})
}

// sendFCM sends a push notification via Firebase Cloud Messaging.
func (a *MobileAdapter) sendFCM(token string, notif PushNotification) error {
	if a.config.FCM == nil {
		return fmt.Errorf("FCM not configured")
	}

	// FCM v1 API request.
	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"token": token,
			"notification": map[string]string{
				"title": notif.Title,
				"body":  notif.Body,
			},
			"data": notif.Data,
			"android": map[string]interface{}{
				"priority": "high",
				"notification": map[string]interface{}{
					"sound": notif.Sound,
					"image": notif.ImageURL,
				},
			},
		},
	}

	body, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://fcm.googleapis.com/v1/projects/%s/messages:send", a.config.FCM.ProjectID)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.config.FCM.APIKey)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("FCM HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("FCM returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}

// sendAPNs sends a push notification via Apple Push Notification service.
func (a *MobileAdapter) sendAPNs(token string, notif PushNotification) error {
	if a.config.APNs == nil {
		return fmt.Errorf("APNs not configured")
	}

	// APNs payload (HTTP/2-based, but we use the simplified HTTP/1.1 fallback for testing).
	payload := map[string]interface{}{
		"aps": map[string]interface{}{
			"alert": map[string]string{
				"title": notif.Title,
				"body":  notif.Body,
			},
			"sound": notif.Sound,
			"badge": notif.Badge,
		},
		"data": notif.Data,
	}

	body, _ := json.Marshal(payload)

	// Choose APNs endpoint.
	host := "https://api.push.apple.com"
	if !a.config.APNs.Production {
		host = "https://api.sandbox.push.apple.com"
	}
	url := fmt.Sprintf("%s/3/device/%s", host, token)

	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", a.config.APNs.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("APNs HTTP request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("APNs returned %d: %s", resp.StatusCode, respBody)
	}

	return nil
}
