package runtime

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/0xsaurabhx/GoPilotKit/internal/config"
	"github.com/0xsaurabhx/GoPilotKit/internal/provider"
	"github.com/0xsaurabhx/GoPilotKit/internal/store/threads"
)

// BuildPrompt constructs the message list for the LLM from the agent config,
// conversation history, and current user message.
//
// The resulting messages follow the standard chat format:
//   - system prompt (from agent config)
//   - conversation history (from thread store, sliding window)
//   - current user message
func BuildPrompt(
	agentCfg *config.AgentConfig,
	history []threads.Message,
	userMessage string,
	state json.RawMessage,
) []provider.Message {
	var messages []provider.Message

	// 1. System prompt.
	if agentCfg.SystemPrompt != "" {
		messages = append(messages, provider.Message{
			Role:    "system",
			Content: agentCfg.SystemPrompt,
		})
	}

	// 2. Conversation history.
	for _, msg := range history {
		messages = append(messages, provider.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// 3. Current user message.
	messages = append(messages, provider.Message{
		Role:    "user",
		Content: userMessage,
	})

	return messages
}

// BuildPromptWithTools is like BuildPrompt but also includes tool definitions
// when the agent has tools configured. This is used when tool support is active.
func BuildPromptWithTools(
	agentCfg *config.AgentConfig,
	history []threads.Message,
	userMessage string,
	state json.RawMessage,
	toolDefs []provider.ToolDef,
) []provider.Message {
	messages := BuildPrompt(agentCfg, history, userMessage, state)

	// Append tool definitions if any (these go in the request, not as messages,
	// but we return them alongside for convenience).
	_ = toolDefs // Tool defs are passed in ChatRequest.Tools, not messages.

	return messages
}

// BuildUserContextMessage creates an optional context message that includes
// metadata from the request state. This gives the LLM additional context
// about the user's environment when available.
func BuildUserContextMessage(state json.RawMessage) string {
	if len(state) == 0 {
		return ""
	}

	var ctxParts []string
	var stateMap map[string]interface{}
	if err := json.Unmarshal(state, &stateMap); err == nil {
		for k, v := range stateMap {
			if k == "timestamp" {
				continue // Skip internal fields.
			}
			ctxParts = append(ctxParts, fmt.Sprintf("%s: %v", k, v))
		}
	}

	if len(ctxParts) == 0 {
		return ""
	}

	return strings.Join(ctxParts, "\n")
}

// FormatTimestamp formats a time for inclusion in prompts.
func FormatTimestamp(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04:05 UTC")
}
