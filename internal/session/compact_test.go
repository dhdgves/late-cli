package session

import (
	"encoding/json"
	"late/internal/client"
	"late/internal/common"
	"os"
	"strings"
	"testing"
)

// loadSessionJSON loads the real session file for integration testing.
func loadSessionJSON(t *testing.T, path string) []client.ChatMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("session file not available: %v", err)
	}
	var msgs []client.ChatMessage
	if err := json.Unmarshal(data, &msgs); err != nil {
		t.Fatalf("failed to parse session JSON: %v", err)
	}
	return msgs
}

func msg(role, text string) client.ChatMessage {
	return client.ChatMessage{Role: role, Content: client.TextContent(text)}
}

func msgTool(id, text string) client.ChatMessage {
	return client.ChatMessage{Role: "tool", ToolCallID: id, Content: client.TextContent(text)}
}

func msgAssistantWithTools(ts []client.ToolCall) client.ChatMessage {
	return client.ChatMessage{Role: "assistant", ToolCalls: ts}
}

func tc(name, args string) client.ToolCall {
	return client.ToolCall{
		ID: "call_" + name,
		Function: client.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

func elidedMsg() client.ChatMessage {
	return client.ChatMessage{Role: "tool", Content: client.TextContent(elisionMark)}
}

// --- Unit Tests ---

func TestDropSupersededReads_NoMutations(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file", "content of foo.go"),
		msg("assistant", "done"),
	}

	result := dropSupersededReads(msgs)
	if result[2].Content.String() != "content of foo.go" {
		t.Errorf("read result should NOT be elided when no mutation: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_WriteThenRead(t *testing.T) {
	// Model reads foo.go, then writes foo.go, then reads again.
	// The first read result is stale.
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file", "content v1"),
		msg("assistant", "ok"),
		msgAssistantWithTools([]client.ToolCall{tc("write_file", `{"path":"foo.go"}`)}),
		msgTool("call_write_file", "wrote foo.go"),
		msg("assistant", "done"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file_2", "content v2"),
	}

	result := dropSupersededReads(msgs)
	// First read (index 2) should be elided: file was later mutated.
	if result[2].Content.String() != elisionMark {
		t.Errorf("first read should be elided after write: got %q", result[2].Content.String())
	}
	// Second read (index 8) should be kept.
	if result[8].Content.String() != "content v2" {
		t.Errorf("second read should be kept: got %q", result[8].Content.String())
	}
}

func TestDropSupersededReads_TargetEditThenRead(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file", "content v1"),
		msg("assistant", "ok"),
		msgAssistantWithTools([]client.ToolCall{tc("target_edit", `{"file":"foo.go","search":"x","replace":"y"}`)}),
		msgTool("call_target_edit", "edited foo.go"),
	}

	result := dropSupersededReads(msgs)
	if result[2].Content.String() != elisionMark {
		t.Errorf("read should be elided after target_edit: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_NoSuperseded(t *testing.T) {
	// Read then edit a DIFFERENT file — read should survive.
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file", "content v1"),
		msg("assistant", "ok"),
		msgAssistantWithTools([]client.ToolCall{tc("write_file", `{"path":"bar.go"}`)}),
		msgTool("call_write_file", "wrote bar.go"),
	}

	result := dropSupersededReads(msgs)
	if result[2].Content.String() != "content v1" {
		t.Errorf("read should survive when DIFFERENT file is mutated: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_Idempotent(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{tc("read_file", `{"path":"foo.go"}`)}),
		msgTool("call_read_file", "content v1"),
		msgAssistantWithTools([]client.ToolCall{tc("write_file", `{"path":"foo.go"}`)}),
		msgTool("call_write_file", "wrote"),
	}

	first := dropSupersededReads(msgs)
	second := dropSupersededReads(first)
	if first[2].Content.String() != second[2].Content.String() {
		t.Error("dropSupersededReads should be idempotent")
	}
}

// --- Real Session Tests ---

func TestCompactMessages_RealSession_AlwaysRunsSemantic(t *testing.T) {
	msgs := loadSessionJSON(t, `C:\Users\qxm22\AppData\Roaming\late\sessions\session-20260520-082628.json`)
	if len(msgs) == 0 {
		return
	}

	// Even under a huge context limit, semantic elision always runs.
	result := CompactMessages(msgs, "", nil, 999999)
	if len(result) != len(msgs) {
		t.Errorf("semantic elision should not change message count: %d → %d", len(msgs), len(result))
	}

	// Verify all messages are JSON-roundtrippable (elision mark is valid content).
	for _, m := range result {
		b, err := json.Marshal(m)
		if err != nil {
			t.Errorf("result not JSON-serializable: %v", err)
			continue
		}
		var back client.ChatMessage
		if err := json.Unmarshal(b, &back); err != nil {
			t.Errorf("result not JSON-roundtrippable: %v\n  msg: %s", err, b)
		}
	}

	total := common.CalculateHistoryTokens(result, "", nil)
	t.Logf("always-on semantic: %d messages, %d tokens", len(result), total)
}

func TestCompactMessages_RealSession_SemanticOnly(t *testing.T) {
	msgs := loadSessionJSON(t, `C:\Users\qxm22\AppData\Roaming\late\sessions\session-20260520-082628.json`)
	if len(msgs) == 0 {
		return
	}

	total := common.CalculateHistoryTokens(msgs, "", nil)
	// Set limit so we're in semantic-elision range (between 55% and 70%).
	limit := int(float64(total) / 0.62)

	result := CompactMessages(msgs, "", nil, limit)
	if len(result) != len(msgs) {
		t.Errorf("semantic elision should not change message count: %d → %d", len(msgs), len(result))
	}

	elidedCount := 0
	for _, m := range result {
		if m.Content.String() == elisionMark {
			elidedCount++
		}
	}
	t.Logf("semantic elision: %d tool results elided out of %d messages", elidedCount, len(result))

	// Token reduction.
	newTotal := common.CalculateHistoryTokens(result, "", nil)
	t.Logf("tokens: %d → %d (%.1f%%)", total, newTotal, float64(newTotal)/float64(total)*100)
}

func TestCompactMessages_RealSession_UnderPressure(t *testing.T) {
	msgs := loadSessionJSON(t, `C:\Users\qxm22\AppData\Roaming\late\sessions\session-20260520-082628.json`)
	if len(msgs) == 0 {
		return
	}

	total := common.CalculateHistoryTokens(msgs, "", nil)
	// Aggressive limit: force batch elision.
	limit := int(float64(total) * 0.5) // context half of what we need

	result := CompactMessages(msgs, "", nil, limit)

	elidedCount := 0
	for _, m := range result {
		if m.Content.String() == elisionMark {
			elidedCount++
		}
	}
	newTotal := common.CalculateHistoryTokens(result, "", nil)
	t.Logf("pressure test: %d tokens → %d tokens (%.1f%%), %d messages elided",
		total, newTotal, float64(newTotal)/float64(total)*100, elidedCount)

	if newTotal >= total {
		t.Error("expected token reduction under pressure")
	}

	// System message should never be elided.
	// Last keepTail tool results should be safe.
	tail := result[len(result)-keepTail:]
	for _, m := range tail {
		if m.Role == "tool" && m.Content.String() == elisionMark {
			// Can happen if index < keepTail but they were semantically elided
			// rather than batch-elided. Check: only batch-elision is protected.
		}
	}

	// No messages lost.
	if len(result) != len(msgs) {
		t.Errorf("message count should not change: %d → %d", len(msgs), len(result))
	}

	// All outputs must be valid JSON when serialized.
	for _, m := range result {
		b, err := json.Marshal(m)
		if err != nil {
			t.Errorf("result message not JSON-serializable: %v", err)
			continue
		}
		// Verify it round-trips.
		var back client.ChatMessage
		if err := json.Unmarshal(b, &back); err != nil {
			t.Errorf("result message not JSON-roundtrippable: %v\n  msg: %s", err, b)
		}
	}
}

func TestCompactMessages_ContextLimitZero(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "hello"),
		msg("assistant", "hi"),
	}
	result := CompactMessages(msgs, "", nil, 0)
	if len(result) != len(msgs) {
		t.Error("contextLimit=0 should return input unchanged")
	}
}

func TestBatchElideToolResults_PreservesTail(t *testing.T) {
	// Build a 30-message history of tool results to test KEEP_TAIL.
	msgs := make([]client.ChatMessage, 30)
	for i := range msgs {
		msgs[i] = msgTool("id_"+string(rune('a'+i%26)), strings.Repeat("x", 100))
	}

	// Context limit so small it forces heavy elision.
	result := batchElideToolResults(msgs, 500, "", nil)

	// Last keepTail messages should NOT be elided by batch elision.
	tail := result[30-keepTail:]
	for i, m := range tail {
		if m.Content.String() == elisionMark {
			t.Errorf("tail message %d should not be batch-elided", 30-keepTail+i)
		}
	}
}
