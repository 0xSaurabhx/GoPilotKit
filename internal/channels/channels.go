// Package channels implements multi-surface adapters.
//
// A single agent can serve multiple client surfaces (Web, Mobile, Slack, Teams)
// through channel adapters that translate between surface-specific formats
// and the internal AG-UI protocol.
package channels

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// ChannelType represents the type of client surface.
type ChannelType string

const (
	ChannelWeb    ChannelType = "web"
	ChannelMobile ChannelType = "mobile"
	ChannelSlack  ChannelType = "slack"
	ChannelTeams  ChannelType = "teams"
)

// InboundMessage represents a message coming from a client surface.
type InboundMessage struct {
	ID           string            `json:"id"`
	Channel      ChannelType       `json:"channel"`
	Agent        string            `json:"agent"`
	UserID       string            `json:"user_id"`
	Content      string            `json:"content"`
	ThreadID     string            `json:"thread_id,omitempty"` // Surface-specific thread
	InternalThreadID string       `json:"internal_thread_id,omitempty"` // GoPilotKit thread
	Attachments  []Attachment      `json:"attachments,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
}

// OutboundMessage represents a message going to a client surface.
type OutboundMessage struct {
	ID           string            `json:"id"`
	Channel      ChannelType       `json:"channel"`
	Agent        string            `json:"agent"`
	Content      string            `json:"content"`
	ThreadID     string            `json:"thread_id,omitempty"`
	Attachments  []Attachment      `json:"attachments,omitempty"`
	Blocks       []Block           `json:"blocks,omitempty"` // Slack-style blocks
	Metadata     map[string]string `json:"metadata,omitempty"`
	Timestamp    time.Time         `json:"timestamp"`
}

// Attachment represents a file or media attachment.
type Attachment struct {
	Type    string `json:"type"`    // "image", "file", "audio", "video"
	URL     string `json:"url"`
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	MimeType string `json:"mime_type,omitempty"`
}

// Block represents a rich content block (Slack-style).
type Block struct {
	Type string          `json:"type"` // "section", "divider", "actions", "context"
	Text string          `json:"text,omitempty"`
	Elements []BlockElement `json:"elements,omitempty"`
}

// BlockElement represents an element within a block.
type BlockElement struct {
	Type string `json:"type"` // "button", "overflow", "datepicker"
	Text string `json:"text,omitempty"`
	Value string `json:"value,omitempty"`
}

// Adapter defines the interface for channel adapters.
// Each adapter translates between a surface's format and the internal protocol.
type Adapter interface {
	// Type returns the channel type.
	Type() ChannelType

	// TranslateInbound converts a surface-specific message to our internal format.
	TranslateInbound(raw []byte) (*InboundMessage, error)

	// TranslateOutbound converts our internal message to the surface format.
	TranslateOutbound(msg *OutboundMessage) ([]byte, error)

	// FormatStreamingChunk formats a streaming text chunk for the surface.
	FormatStreamingChunk(agent, chunk string) ([]byte, error)

	// FormatToolCall formats a tool call notification for the surface.
	FormatToolCall(toolName string, args json.RawMessage) ([]byte, error)

	// FormatError formats an error for the surface.
	FormatError(errMsg string) ([]byte, error)

	// SupportsStreaming returns whether this channel supports streaming.
	SupportsStreaming() bool
}

// Manager manages all registered channel adapters.
type Manager struct {
	adapters map[ChannelType]Adapter
	mu       sync.RWMutex
}

// NewManager creates a new channel manager.
func NewManager() *Manager {
	return &Manager{
		adapters: make(map[ChannelType]Adapter),
	}
}

// Register registers a channel adapter.
func (m *Manager) Register(adapter Adapter) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.adapters[adapter.Type()] = adapter
}

// Get returns the adapter for the given channel type.
func (m *Manager) Get(channel ChannelType) (Adapter, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	a, ok := m.adapters[channel]
	return a, ok
}

// List returns all registered channel types.
func (m *Manager) List() []ChannelType {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var types []ChannelType
	for t := range m.adapters {
		types = append(types, t)
	}
	return types
}

// --- Web Adapter (primary, fully functional) ---

// WebAdapter handles the web/HTTP channel.
type WebAdapter struct{}

// NewWebAdapter creates a new web channel adapter.
func NewWebAdapter() *WebAdapter {
	return &WebAdapter{}
}

func (a *WebAdapter) Type() ChannelType { return ChannelWeb }

func (a *WebAdapter) TranslateInbound(raw []byte) (*InboundMessage, error) {
	var msg InboundMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return nil, fmt.Errorf("web: invalid message format: %w", err)
	}
	msg.Channel = ChannelWeb
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now().UTC()
	}
	return &msg, nil
}

func (a *WebAdapter) TranslateOutbound(msg *OutboundMessage) ([]byte, error) {
	msg.Channel = ChannelWeb
	return json.Marshal(msg)
}

func (a *WebAdapter) FormatStreamingChunk(agent, chunk string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "text_chunk",
		"agent": agent,
		"delta": chunk,
	})
}

func (a *WebAdapter) FormatToolCall(toolName string, args json.RawMessage) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":      "tool_call",
		"tool_name": toolName,
		"arguments": json.RawMessage(args),
	})
}

func (a *WebAdapter) FormatError(errMsg string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": errMsg,
	})
}

func (a *WebAdapter) SupportsStreaming() bool { return true }

// --- Slack Adapter ---

// SlackAdapter handles the Slack channel.
type SlackAdapter struct{}

// NewSlackAdapter creates a new Slack channel adapter.
func NewSlackAdapter() *SlackAdapter {
	return &SlackAdapter{}
}

func (a *SlackAdapter) Type() ChannelType { return ChannelSlack }

func (a *SlackAdapter) TranslateInbound(raw []byte) (*InboundMessage, error) {
	// Slack event API format: {"event":{"type":"message","text":"...","user":"...","channel":"..."}}
	var event struct {
		Event struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			User    string `json:"user"`
			Channel string `json:"channel"`
			Thread  string `json:"thread_ts"`
		} `json:"event"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("slack: invalid event format: %w", err)
	}

	if event.Event.Type != "message" {
		return nil, fmt.Errorf("slack: unsupported event type: %s", event.Event.Type)
	}

	return &InboundMessage{
		Channel:    ChannelSlack,
		UserID:     event.Event.User,
		Content:    event.Event.Text,
		ThreadID:   event.Event.Channel,
		Timestamp:  time.Now().UTC(),
	}, nil
}

func (a *SlackAdapter) TranslateOutbound(msg *OutboundMessage) ([]byte, error) {
	// Slack message format with blocks for rich formatting.
	payload := map[string]interface{}{
		"channel": msg.ThreadID,
		"text":    msg.Content,
		"blocks":  msg.Blocks,
	}
	if len(msg.Blocks) == 0 {
		payload["blocks"] = []map[string]interface{}{
			{
				"type": "section",
				"text": map[string]string{
					"type": "mrkdwn",
					"text": msg.Content,
				},
			},
		}
	}
	return json.Marshal(payload)
}

func (a *SlackAdapter) FormatStreamingChunk(agent, chunk string) ([]byte, error) {
	// Slack doesn't natively support streaming; batch updates via responses.update.
	return json.Marshal(map[string]interface{}{
		"type":  "text_chunk",
		"agent": agent,
		"delta": chunk,
		"channel_type": "slack",
	})
}

func (a *SlackAdapter) FormatToolCall(toolName string, args json.RawMessage) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":      "tool_call",
		"tool_name": toolName,
		"arguments": json.RawMessage(args),
		"channel_type": "slack",
	})
}

func (a *SlackAdapter) FormatError(errMsg string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": errMsg,
		"channel_type": "slack",
	})
}

func (a *SlackAdapter) SupportsStreaming() bool { return false }

// --- Teams Adapter ---

// TeamsAdapter handles the Microsoft Teams channel.
type TeamsAdapter struct{}

// NewTeamsAdapter creates a new Teams channel adapter.
func NewTeamsAdapter() *TeamsAdapter {
	return &TeamsAdapter{}
}

func (a *TeamsAdapter) Type() ChannelType { return ChannelTeams }

func (a *TeamsAdapter) TranslateInbound(raw []byte) (*InboundMessage, error) {
	// Teams Bot Framework format: {"text":"...", "from":{"id":"..."}, "conversation":{"id":"..."}}
	var event struct {
		Text         string `json:"text"`
		From         struct {
			ID string `json:"id"`
		} `json:"from"`
		Conversation struct {
			ID string `json:"id"`
		} `json:"conversation"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return nil, fmt.Errorf("teams: invalid event format: %w", err)
	}

	return &InboundMessage{
		Channel:  ChannelTeams,
		UserID:   event.From.ID,
		Content:  event.Text,
		ThreadID: event.Conversation.ID,
		Timestamp: time.Now().UTC(),
	}, nil
}

func (a *TeamsAdapter) TranslateOutbound(msg *OutboundMessage) ([]byte, error) {
	// Teams Bot Framework message format.
	payload := map[string]interface{}{
		"type":    "message",
		"from":    map[string]string{"id": "gopilotkit"},
		"conversation": map[string]string{"id": msg.ThreadID},
		"text":    msg.Content,
		"attachments": []map[string]interface{}{
			{
				"contentType": "application/vnd.microsoft.card.hero",
				"content": map[string]interface{}{
					"buttons": []map[string]interface{}{},
				},
			},
		},
	}
	return json.Marshal(payload)
}

func (a *TeamsAdapter) FormatStreamingChunk(agent, chunk string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "text_chunk",
		"agent": agent,
		"delta": chunk,
		"channel_type": "teams",
	})
}

func (a *TeamsAdapter) FormatToolCall(toolName string, args json.RawMessage) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":      "tool_call",
		"tool_name": toolName,
		"arguments": json.RawMessage(args),
		"channel_type": "teams",
	})
}

func (a *TeamsAdapter) FormatError(errMsg string) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"type":  "error",
		"error": errMsg,
		"channel_type": "teams",
	})
}

func (a *TeamsAdapter) SupportsStreaming() bool { return false }
