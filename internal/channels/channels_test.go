package channels

import (
	"encoding/json"
	"testing"
)

func TestWebAdapterTranslateInbound(t *testing.T) {
	adapter := NewWebAdapter()

	raw := []byte(`{"agent":"test-agent","content":"Hello","user_id":"user-1"}`)
	msg, err := adapter.TranslateInbound(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Channel != ChannelWeb {
		t.Errorf("expected channel 'web', got '%s'", msg.Channel)
	}
	if msg.Content != "Hello" {
		t.Errorf("expected content 'Hello', got '%s'", msg.Content)
	}
}

func TestWebAdapterTranslateOutbound(t *testing.T) {
	adapter := NewWebAdapter()

	msg := &OutboundMessage{
		Content:  "Hi there!",
		ThreadID: "thread-1",
	}
	data, err := adapter.TranslateOutbound(msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Channel != ChannelWeb {
		t.Errorf("expected channel to be set to 'web'")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to parse output: %v", err)
	}
	if parsed["content"] != "Hi there!" {
		t.Errorf("expected content 'Hi there!', got %v", parsed["content"])
	}
}

func TestWebAdapterStreaming(t *testing.T) {
	adapter := NewWebAdapter()
	if !adapter.SupportsStreaming() {
		t.Error("expected web adapter to support streaming")
	}

	chunk, err := adapter.FormatStreamingChunk("agent", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(chunk, &parsed)
	if parsed["type"] != "text_chunk" {
		t.Errorf("expected type 'text_chunk', got %v", parsed["type"])
	}
}

func TestWebAdapterError(t *testing.T) {
	adapter := NewWebAdapter()
	data, err := adapter.FormatError("something went wrong")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["error"] != "something went wrong" {
		t.Errorf("expected error message, got %v", parsed["error"])
	}
}

func TestSlackAdapterTranslateInbound(t *testing.T) {
	adapter := NewSlackAdapter()

	raw := []byte(`{"event":{"type":"message","text":"Hello from Slack","user":"U123","channel":"C456"}}`)
	msg, err := adapter.TranslateInbound(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Channel != ChannelSlack {
		t.Errorf("expected channel 'slack', got '%s'", msg.Channel)
	}
	if msg.UserID != "U123" {
		t.Errorf("expected user 'U123', got '%s'", msg.UserID)
	}
}

func TestSlackAdapterNonMessageEvent(t *testing.T) {
	adapter := NewSlackAdapter()

	raw := []byte(`{"event":{"type":"reaction_added","user":"U123"}}`)
	_, err := adapter.TranslateInbound(raw)
	if err == nil {
		t.Error("expected error for non-message event")
	}
}

func TestSlackAdapterNoStreaming(t *testing.T) {
	adapter := NewSlackAdapter()
	if adapter.SupportsStreaming() {
		t.Error("expected Slack adapter to not support streaming")
	}
}

func TestTeamsAdapterTranslateInbound(t *testing.T) {
	adapter := NewTeamsAdapter()

	raw := []byte(`{"text":"Hello from Teams","from":{"id":"U789"},"conversation":{"id":"conv-123"}}`)
	msg, err := adapter.TranslateInbound(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Channel != ChannelTeams {
		t.Errorf("expected channel 'teams', got '%s'", msg.Channel)
	}
	if msg.Content != "Hello from Teams" {
		t.Errorf("expected content 'Hello from Teams', got '%s'", msg.Content)
	}
}

func TestTeamsAdapterNoStreaming(t *testing.T) {
	adapter := NewTeamsAdapter()
	if adapter.SupportsStreaming() {
		t.Error("expected Teams adapter to not support streaming")
	}
}

func TestManager(t *testing.T) {
	manager := NewManager()

	manager.Register(NewWebAdapter())
	manager.Register(NewSlackAdapter())
	manager.Register(NewTeamsAdapter())

	if len(manager.List()) != 3 {
		t.Errorf("expected 3 channels, got %d", len(manager.List()))
	}

	web, ok := manager.Get(ChannelWeb)
	if !ok || web == nil {
		t.Error("expected web adapter to be registered")
	}

	slack, ok := manager.Get(ChannelSlack)
	if !ok || slack == nil {
		t.Error("expected slack adapter to be registered")
	}

	_, ok = manager.Get("unknown")
	if ok {
		t.Error("expected unknown channel to not be found")
	}
}

func TestInvalidInbound(t *testing.T) {
	adapter := NewWebAdapter()
	_, err := adapter.TranslateInbound([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}
