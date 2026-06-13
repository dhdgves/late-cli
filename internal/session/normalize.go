package session

import (
	"encoding/json"
	"late/internal/client"
)

// interruptedToolResult stands in for a tool call whose result never
// arrived (interrupted stream, crash). Without this, an assistant
// message with tool_calls would be followed by zero tool messages,
// which trips the OpenAI "An assistant message with 'tool_calls'
// must be followed by tool messages" 400 error.
const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

// NormalizeMessages repairs and normalizes a conversation history so it
// satisfies the tool-call contract APIs enforce: every assistant tool_calls
// entry must be answered by a following tool message for its id, and tool
// results must be ordered to match their tool_call order.
//
// This is directly inspired by DeepSeek-Reasonix's SanitizeToolPairing.
// The normalized output is deterministic — critical for prefix KV-cache
// reuse across consecutive API calls.
//
// Pipeline:
//  1. For each assistant message with tool_calls:
//     a. Reorder following tool messages to match tool_call order
//     b. Backfill placeholder for any unanswered tool_call
//  2. Drop orphan tool messages (no preceding assistant tool_calls)
//  3. User/system messages pass through unchanged
func NormalizeMessages(msgs []client.ChatMessage) []client.ChatMessage {
	if len(msgs) == 0 {
		return msgs
	}

	out := make([]client.ChatMessage, 0, len(msgs))

	for i := 0; i < len(msgs); {
		m := msgs[i]

		// Assistant with tool_calls: pair with following tool messages.
		if m.Role == "assistant" && len(m.ToolCalls) > 0 {
			// Repair any truncated JSON arguments.
			for j := range m.ToolCalls {
				args := m.ToolCalls[j].Function.Arguments
				if args != "" && !json.Valid([]byte(args)) {
					m.ToolCalls[j].Function.Arguments = closeTruncatedJSON(args)
				}
			}

			out = append(out, m)
			i++

			// Collect following tool messages belonging to this turn.
			j := i
			for j < len(msgs) && msgs[j].Role == "tool" {
				j++
			}
			toolMsgs := msgs[i:j]

			// Reorder tool messages to match tool_call order.
			byID := make(map[string]client.ChatMessage, len(toolMsgs))
			for _, tm := range toolMsgs {
				byID[tm.ToolCallID] = tm
			}

			for _, tc := range m.ToolCalls {
				if tm, ok := byID[tc.ID]; ok {
					out = append(out, tm)
				} else {
					// Backfill placeholder for unanswered call.
					out = append(out, client.ChatMessage{
						Role:       "tool",
						ToolCallID: tc.ID,
						Content:    client.TextContent(interruptedToolResult),
					})
				}
			}

			i = j // consumed tool messages
			continue
		}

		// Orphan tool message: drop it.
		if m.Role == "tool" {
			i++
			continue
		}

		// User, system, assistant without tools: pass through.
		out = append(out, m)
		i++
	}

	return out
}

// closeTruncatedJSON best-effort completes a JSON string cut off mid-stream.
// Falls back to "{}" if repair fails.
func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := s
	if esc {
		out = out[:len(out)-1]
	}
	if inStr {
		out += `"`
	}
	trimmed := out
	for len(trimmed) > 0 && (trimmed[len(trimmed)-1] == ' ' || trimmed[len(trimmed)-1] == '\t' || trimmed[len(trimmed)-1] == '\r' || trimmed[len(trimmed)-1] == '\n') {
		trimmed = trimmed[:len(trimmed)-1]
	}
	switch {
	case len(trimmed) > 0 && trimmed[len(trimmed)-1] == ',':
		out = trimmed[:len(trimmed)-1]
	case len(trimmed) > 0 && trimmed[len(trimmed)-1] == ':':
		out = trimmed + "null"
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
	}
	if !json.Valid([]byte(out)) {
		return "{}"
	}
	return out
}
