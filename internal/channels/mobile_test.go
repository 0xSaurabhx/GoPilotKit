package channels

import (
	"encoding/json"
	"testing"
	"time"
)

func TestMobileAdapterTranslateInbound(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	raw := []byte(`{
		"content": "What's the weather?",
		"agent": "weather-bot",
		"user_id": "user123",
		"thread_id": "thread456"
	}`)

	msg, err := adapter.TranslateInbound(raw)
	if err != nil {
		t.Fatalf("TranslateInbound: %v", err)
	}

	if msg.Channel != ChannelMobile {
		t.Errorf("expected channel mobile, got %s", msg.Channel)
	}
	if msg.Content != "What's the weather?" {
		t.Errorf("expected 'What's the weather?', got %q", msg.Content)
	}
	if msg.UserID != "user123" {
		t.Errorf("expected user 'user123', got %q", msg.UserID)
	}
	if msg.ThreadID != "thread456" {
		t.Errorf("expected thread 'thread456', got %q", msg.ThreadID)
	}
}

func TestMobileAdapterTranslateOutbound(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	msg := &OutboundMessage{
		ID:        "msg_1",
		Agent:     "weather-bot",
		Content:   "It's sunny today!",
		ThreadID:  "thread456",
		Timestamp: time.Now().UTC(),
	}

	data, err := adapter.TranslateOutbound(msg)
	if err != nil {
		t.Fatalf("TranslateOutbound: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("parsing outbound: %v", err)
	}

	if parsed["type"] != "message" {
		t.Errorf("expected type 'message', got %v", parsed["type"])
	}
	if parsed["content"] != "It's sunny today!" {
		t.Errorf("expected content 'It's sunny today!', got %v", parsed["content"])
	}
}

func TestMobileAdapterFormatStreamingChunk(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	data, err := adapter.FormatStreamingChunk("bot", "hello ")
	if err != nil {
		t.Fatalf("FormatStreamingChunk: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["type"] != "text_chunk" {
		t.Errorf("expected type 'text_chunk', got %v", parsed["type"])
	}
	if parsed["delta"] != "hello " {
		t.Errorf("expected delta 'hello ', got %v", parsed["delta"])
	}
}

func TestMobileAdapterFormatToolCall(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	args, _ := json.Marshal(map[string]string{"city": "London"})
	data, err := adapter.FormatToolCall("get_weather", args)
	if err != nil {
		t.Fatalf("FormatToolCall: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["tool_name"] != "get_weather" {
		t.Errorf("expected tool_name 'get_weather', got %v", parsed["tool_name"])
	}
}

func TestMobileAdapterFormatError(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	data, err := adapter.FormatError("something went wrong")
	if err != nil {
		t.Fatalf("FormatError: %v", err)
	}

	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)

	if parsed["error"] != "something went wrong" {
		t.Errorf("expected error 'something went wrong', got %v", parsed["error"])
	}
}

func TestMobileAdapterSupportsStreaming(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})
	if !adapter.SupportsStreaming() {
		t.Error("mobile adapter should support streaming")
	}
}

func TestMobileDeviceRegistration(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	// Register devices.
	adapter.RegisterDevice("user1", "ios_token_1", "ios")
	adapter.RegisterDevice("user1", "android_token_1", "android")
	adapter.RegisterDevice("user2", "ios_token_2", "ios")

	// Check device tokens.
	tokens := adapter.GetDeviceTokens("user1")
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens for user1, got %d", len(tokens))
	}

	tokens = adapter.GetDeviceTokens("user2")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token for user2, got %d", len(tokens))
	}

	// User with no devices.
	tokens = adapter.GetDeviceTokens("user3")
	if len(tokens) != 0 {
		t.Errorf("expected 0 tokens for user3, got %d", len(tokens))
	}
}

func TestMobileDeviceUnregister(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	adapter.RegisterDevice("user1", "token_a", "ios")
	adapter.RegisterDevice("user1", "token_b", "android")

	adapter.UnregisterDevice("user1", "token_a")

	tokens := adapter.GetDeviceTokens("user1")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token after unregister, got %d", len(tokens))
	}
	if tokens[0].Token != "token_b" {
		t.Errorf("expected remaining token to be 'token_b', got %q", tokens[0].Token)
	}
}

func TestMobileDeviceDuplicateRegistration(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	adapter.RegisterDevice("user1", "token_a", "ios")
	adapter.RegisterDevice("user1", "token_a", "android") // Same token, updated platform.

	tokens := adapter.GetDeviceTokens("user1")
	if len(tokens) != 1 {
		t.Fatalf("expected 1 token (no duplicate), got %d", len(tokens))
	}
	if tokens[0].Platform != "android" {
		t.Errorf("expected platform 'android', got %q", tokens[0].Platform)
	}
}

func TestMobileAdapterTranslateInboundInvalidJSON(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	_, err := adapter.TranslateInbound([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMobileAdapterTranslateInboundMinimal(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})

	raw := []byte(`{"content": "hi", "agent": "bot", "user_id": "u1"}`)
	msg, err := adapter.TranslateInbound(raw)
	if err != nil {
		t.Fatalf("TranslateInbound: %v", err)
	}

	if msg.Content != "hi" {
		t.Errorf("expected 'hi', got %q", msg.Content)
	}
	if msg.ThreadID != "" {
		t.Errorf("expected empty thread_id, got %q", msg.ThreadID)
	}
}

func TestMobileAdapterType(t *testing.T) {
	adapter := NewMobileAdapter(MobileConfig{})
	if adapter.Type() != ChannelMobile {
		t.Errorf("expected channel type 'mobile', got %q", adapter.Type())
	}
}
