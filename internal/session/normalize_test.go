package session

import (
	"late/internal/client"
	"testing"
)

// --- NormalizeMessages Tests (Reasonix-style) ---

func TestNormalizeMessages_ToolResultsReorderedByCallOrder(t *testing.T) {
	// Tool results arrive in reverse order (parallel execution).
	// Normalize should reorder them to match tool_call order.
	msgs := []client.ChatMessage{
		msg("user", "do two things"),
		msgAssistantWithTools([]client.ToolCall{
			tc("read_file", `{"path":"a.go"}`),
			tc("bash", `{"command":"echo hi"}`),
		}),
		msgTool("call_bash", "hi"),           // bash result arrives first
		msgTool("call_read_file", "a.go content"), // read arrives second
	}

	result := NormalizeMessages(msgs)
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
	// Result should be in tool_call order: read_file result first, then bash.
	if result[2].ToolCallID != "call_read_file" {
		t.Errorf("expected read_file result first, got %q", result[2].ToolCallID)
	}
	if result[3].ToolCallID != "call_bash" {
		t.Errorf("expected bash result second, got %q", result[3].ToolCallID)
	}
}

func TestNormalizeMessages_OrphanToolDropped(t *testing.T) {
	// A tool result with no matching assistant tool_call should be dropped.
	msgs := []client.ChatMessage{
		msg("user", "hello"),
		msg("assistant", "ok"),
		msgTool("orphan_id", "orphan result"), // no assistant with this tool_call
		msg("user", "next question"),
	}

	result := NormalizeMessages(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3 messages (orphan dropped), got %d", len(result))
	}
	for _, m := range result {
		if m.ToolCallID == "orphan_id" {
			t.Error("orphan tool message should be dropped")
		}
	}
}

func TestNormalizeMessages_UnansweredToolCallBackfilled(t *testing.T) {
	// Assistant made 2 tool_calls but only 1 result arrived (interrupted stream).
	// The unanswered call should get a placeholder result.
	msgs := []client.ChatMessage{
		msg("user", "read and write"),
		msgAssistantWithTools([]client.ToolCall{
			tc("read_file", `{"path":"a.go"}`),
			tc("write_file", `{"path":"a.go"}`),
		}),
		msgTool("call_read_file", "content"), // only this one arrived
	}

	result := NormalizeMessages(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 messages (2 tool results), got %d", len(result))
	}
	// First result: read_file (answer arrived)
	if result[2].ToolCallID != "call_read_file" || result[2].Content.String() != "content" {
		t.Errorf("first result should be read_file: got id=%q content=%q", result[2].ToolCallID, result[2].Content.String())
	}
	// Second result: write_file placeholder
	if result[3].ToolCallID != "call_write_file" {
		t.Errorf("second result should be write_file placeholder: got id=%q", result[3].ToolCallID)
	}
	if result[3].Content.String() != interruptedToolResult {
		t.Errorf("unanswered call should be '%s', got '%s'", interruptedToolResult, result[3].Content.String())
	}
}

func TestNormalizeMessages_MultipleAssistantTurns(t *testing.T) {
	// Multiple assistant+tool cycles: each turn's tools should be
	// independently normalized.
	msgs := []client.ChatMessage{
		msg("user", "step 1"),
		msgAssistantWithTools([]client.ToolCall{
			tc("read_file", `{"path":"a.go"}`),
		}),
		msgTool("call_read_file", "a.go v1"),
		msg("assistant", "ok let me edit"),
		msgAssistantWithTools([]client.ToolCall{
			tc("write_file", `{"path":"a.go"}`),
			tc("read_file", `{"path":"a.go"}`),
		}),
		msgTool("call_read_file", "a.go v2"),  // reversed order
		msgTool("call_write_file", "wrote"),
	}

	result := NormalizeMessages(msgs)
	if len(result) != len(msgs) {
		t.Fatalf("expected %d messages, got %d", len(msgs), len(result))
	}
	// Turn 1: read_file alone — unchanged.
	if result[2].ToolCallID != "call_read_file" || result[2].Content.String() != "a.go v1" {
		t.Errorf("turn 1 read_file mismatch")
	}
	// Turn 2: write_file first, then read_file (reordered from original read,write).
	if result[5].ToolCallID != "call_write_file" {
		t.Errorf("turn 2 expected write_file first, got %q", result[5].ToolCallID)
	}
	if result[6].ToolCallID != "call_read_file" {
		t.Errorf("turn 2 expected read_file second, got %q", result[6].ToolCallID)
	}
}

func TestNormalizeMessages_NoToolCalls_PassThrough(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "hello"),
		msg("assistant", "hi"),
		msg("user", "how are you"),
	}

	result := NormalizeMessages(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3 messages, got %d", len(result))
	}
	for i := range msgs {
		if result[i].Content.String() != msgs[i].Content.String() {
			t.Errorf("message %d changed: got %q want %q", i, result[i].Content.String(), msgs[i].Content.String())
		}
	}
}

func TestNormalizeMessages_EmptyHistory(t *testing.T) {
	result := NormalizeMessages(nil)
	if result != nil {
		t.Errorf("expected nil for nil input, got %v", result)
	}
	result = NormalizeMessages([]client.ChatMessage{})
	if len(result) != 0 {
		t.Errorf("expected empty for empty input, got %d", len(result))
	}
}

func TestNormalizeMessages_SystemMessageUntouched(t *testing.T) {
	msgs := []client.ChatMessage{
		{Role: "system", Content: client.TextContent("You are a helpful assistant")},
		msg("user", "hello"),
	}

	result := NormalizeMessages(msgs)
	if len(result) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result))
	}
	if result[0].Role != "system" || result[0].Content.String() != "You are a helpful assistant" {
		t.Error("system message modified")
	}
}

func TestNormalizeMessages_Idempotent(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "do it"),
		msgAssistantWithTools([]client.ToolCall{
			tc("read_file", `{"path":"a.go"}`),
			tc("bash", `{"command":"x"}`),
		}),
		msgTool("call_read_file", "content"),
		msgTool("call_bash", "output"),
	}

	first := NormalizeMessages(msgs)
	second := NormalizeMessages(first)

	if len(first) != len(second) {
		t.Fatalf("idempotency failed: %d != %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Content.String() != second[i].Content.String() {
			t.Errorf("idempotency broken at message %d", i)
		}
	}
}
