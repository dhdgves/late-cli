package session

import (
	"encoding/json"
	"fmt"
	"late/internal/client"
	"late/internal/common"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// Test Helpers
// ============================================================

// loadSessionJSON loads a real session file or skips if missing.
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

// tc creates a tool call with ID "call_<name>".
func tc(name, args string) client.ToolCall {
	return client.ToolCall{
		ID: "call_" + name,
		Function: client.FunctionCall{
			Name:      name,
			Arguments: args,
		},
	}
}

// assertTokenReduction verifies compaction reduces tokens under pressure.
func assertTokenReduction(t *testing.T, msgs []client.ChatMessage, label string, limit int) []client.ChatMessage {
	t.Helper()
	before := common.CalculateHistoryTokens(msgs, "", nil)
	result := CompactMessages(msgs, "", nil, limit)
	after := common.CalculateHistoryTokens(result, "", nil)

	elided := 0
	for _, m := range result {
		if m.Content.String() == elisionMark {
			elided++
		}
	}
	for _, m := range result {
		if m.Role == "tool" && m.Content.String() == elisionMark {
			// also count tool messages explicitly
		}
	}

	t.Logf("%s: %d→%d tokens (%.1f%%), %d elided, %d messages",
		label, before, after, float64(after)/float64(before)*100, elided, len(result))

	if after >= before && limit > 0 {
		t.Error("expected token reduction under pressure")
	}
	if len(result) != len(msgs) {
		t.Errorf("message count unchanged: %d → %d", len(msgs), len(result))
	}

	// All messages must JSON-roundtrip.
	for i, m := range result {
		b, err := json.Marshal(m)
		if err != nil {
			t.Errorf("[%d] not JSON-serializable: %v", i, err)
			continue
		}
		var back client.ChatMessage
		if err := json.Unmarshal(b, &back); err != nil {
			t.Errorf("[%d] not JSON-roundtrippable: %v\n  msg: %s", i, err, b)
		}
	}
	return result
}

// ============================================================
// A. dropSupersededReads — Unit Tests
// ============================================================

func TestDropSupersededReads_NoMutations_PassThrough(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		msgAssistantWithTools([]client.ToolCall{
			{ID: "call_0", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}),
		msgTool("call_0", "content of foo.go"),
		msg("assistant", "done"),
	}
	result := dropSupersededReads(msgs)
	if result[2].Content.String() != "content of foo.go" {
		t.Errorf("read should NOT be elided without mutation: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_PreWriteElided_PostWriteSurvives(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "check foo.go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r1", "buggy code v1"),
		msg("assistant", "fixing..."),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_w1", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_w1", "wrote"),
		msg("assistant", "now verify..."),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r2", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r2", "fixed code v2"),
	}

	result := dropSupersededReads(msgs)
	if result[2].Content.String() != elisionMark {
		t.Errorf("pre-write read should be elided: %q", result[2].Content.String())
	}
	if result[8].Content.String() != "fixed code v2" {
		t.Errorf("post-write read MUST survive: got %q", result[8].Content.String())
	}
}

func TestDropSupersededReads_MultiWrite_ReadBetweenWrites(t *testing.T) {
	// write → read → write. The read result between two writes is stale
	// (the second write invalidated it). lastMutation must catch this.
	msgs := []client.ChatMessage{
		msg("user", "fix foo.go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_w0", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_w0", "wrote v1"),
		msg("assistant", "let me check"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r1", "content v1"), // ← should be elided (second write invalidates)
		msg("assistant", "need more changes"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_w2", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_w2", "wrote v2"),
	}

	result := dropSupersededReads(msgs)
	if result[5].Content.String() != elisionMark {
		t.Errorf("read between two writes should be elided: got %q", result[5].Content.String())
	}
}

func TestDropSupersededReads_TargetEditSupersedes(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r1", "content v1"),
		msg("assistant", "ok"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_e1", Function: client.FunctionCall{Name: "target_edit", Arguments: `{"file":"foo.go","search":"x","replace":"y"}`}},
		}},
		msgTool("call_e1", "edited foo.go"),
	}
	result := dropSupersededReads(msgs)
	if result[2].Content.String() != elisionMark {
		t.Errorf("read should be elided after target_edit: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_DifferentFile_Survives(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r1", "foo content"),
		msg("assistant", "ok"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_w1", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"bar.go"}`}},
		}},
		msgTool("call_w1", "wrote bar.go"),
	}
	result := dropSupersededReads(msgs)
	if result[2].Content.String() != "foo content" {
		t.Errorf("read should survive when DIFFERENT file mutated: got %q", result[2].Content.String())
	}
}

func TestDropSupersededReads_Idempotent(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read foo.go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_r1", "content v1"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_w1", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"foo.go"}`}},
		}},
		msgTool("call_w1", "wrote"),
	}
	first := dropSupersededReads(msgs)
	second := dropSupersededReads(first)
	for i := range first {
		if first[i].Content.String() != second[i].Content.String() {
			t.Fatalf("idempotency broken at index %d: %q vs %q", i, first[i].Content.String(), second[i].Content.String())
		}
	}
}

// ============================================================
// B. NormalizeMessages — Unit Tests
// ============================================================

func TestNormalize_Reorder_MatchesToolCallOrder(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "do two things"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_0", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
			{ID: "call_1", Function: client.FunctionCall{Name: "bash", Arguments: `{"command":"echo hi"}`}},
		}},
		msgTool("call_1", "hi"),           // reversed: bash result first
		msgTool("call_0", "a.go content"), // read result second
	}
	result := NormalizeMessages(msgs)
	if result[2].ToolCallID != "call_0" {
		t.Errorf("expected read_file result first, got %q", result[2].ToolCallID)
	}
	if result[3].ToolCallID != "call_1" {
		t.Errorf("expected bash result second, got %q", result[3].ToolCallID)
	}
}

func TestNormalize_OrphanTool_Dropped(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "hello"),
		msg("assistant", "ok"),
		msgTool("orphan_99", "dangling result"),
		msg("user", "next"),
	}
	result := NormalizeMessages(msgs)
	if len(result) != 3 {
		t.Errorf("expected 3 (orphan dropped), got %d", len(result))
	}
}

func TestNormalize_UnansweredCall_Backfilled(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "read and write"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "call_0", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
			{ID: "call_1", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"a.go"}`}},
		}},
		msgTool("call_0", "content"), // only one result arrived
	}
	result := NormalizeMessages(msgs)
	if len(result) != 4 {
		t.Fatalf("expected 4 (2 tool results), got %d", len(result))
	}
	if result[3].ToolCallID != "call_1" || result[3].Content.String() != interruptedToolResult {
		t.Errorf("unanswered call should be '%s', got %q", interruptedToolResult, result[3].Content.String())
	}
}

func TestNormalize_MultiTurn_IndependentNormalization(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "step 1"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
		}},
		msgTool("r1", "a.go v1"),
		msg("assistant", "edit time"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "w1", Function: client.FunctionCall{Name: "write_file", Arguments: `{"path":"a.go"}`}},
			{ID: "r2", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
		}},
		msgTool("r2", "a.go v2"), // reversed
		msgTool("w1", "wrote"),
	}
	result := NormalizeMessages(msgs)
	// Turn 1: r1 alone
	if result[2].ToolCallID != "r1" {
		t.Errorf("turn 1: expected r1, got %q", result[2].ToolCallID)
	}
	// Turn 2: w1 then r2 (reordered)
	if result[5].ToolCallID != "w1" {
		t.Errorf("turn 2: expected w1 first, got %q", result[5].ToolCallID)
	}
	if result[6].ToolCallID != "r2" {
		t.Errorf("turn 2: expected r2 second, got %q", result[6].ToolCallID)
	}
}

func TestNormalize_PassThrough_SystemAndUser(t *testing.T) {
	msgs := []client.ChatMessage{
		{Role: "system", Content: client.TextContent("You are helpful")},
		msg("user", "hello"),
		msg("assistant", "hi"),
	}
	result := NormalizeMessages(msgs)
	if len(result) != 3 {
		t.Fatalf("expected 3, got %d", len(result))
	}
	if result[0].Content.String() != "You are helpful" {
		t.Error("system message modified")
	}
}

func TestNormalize_Idempotent(t *testing.T) {
	msgs := []client.ChatMessage{
		msg("user", "go"),
		{Role: "assistant", ToolCalls: []client.ToolCall{
			{ID: "r1", Function: client.FunctionCall{Name: "read_file", Arguments: `{"path":"a.go"}`}},
			{ID: "b1", Function: client.FunctionCall{Name: "bash", Arguments: `{"command":"x"}`}},
		}},
		msgTool("r1", "content"),
		msgTool("b1", "output"),
	}
	first := NormalizeMessages(msgs)
	second := NormalizeMessages(first)
	for i := range first {
		if first[i].Content.String() != second[i].Content.String() {
			t.Fatalf("idempotency broken at index %d", i)
		}
	}
}

func TestNormalize_Empty_NilSafe(t *testing.T) {
	if r := NormalizeMessages(nil); r != nil {
		t.Error("nil should return nil")
	}
	if r := NormalizeMessages([]client.ChatMessage{}); len(r) != 0 {
		t.Errorf("empty should return empty, got %d", len(r))
	}
}

// ============================================================
// C. stripOldReasoning / batchElideToolResults — Unit Tests
// ============================================================

func TestStripOldReasoning_PreservesLastK(t *testing.T) {
	msgs := make([]client.ChatMessage, 20)
	for i := range msgs {
		msgs[i] = client.ChatMessage{
			Role:             "assistant",
			Content:          client.TextContent("ok"),
			ReasoningContent: fmt.Sprintf("thought %d", i),
		}
	}
	result := stripOldReasoning(msgs)

	// Last keepReasoning=12 must have reasoning.
	for i := 20 - keepReasoning; i < 20; i++ {
		if result[i].ReasoningContent == "" {
			t.Errorf("assistant %d reasoning cleared but should be preserved", i)
		}
	}
	// First 8 must be cleared.
	for i := 0; i < 20-keepReasoning; i++ {
		if result[i].ReasoningContent != "" {
			t.Errorf("assistant %d reasoning not cleared", i)
		}
	}
}

func TestStripOldReasoning_FewerThanKeep_AllSurvive(t *testing.T) {
	msgs := []client.ChatMessage{
		{Role: "assistant", Content: client.TextContent("hi"), ReasoningContent: "think"},
		{Role: "assistant", Content: client.TextContent("done"), ReasoningContent: "done thinking"},
	}
	result := stripOldReasoning(msgs)
	for i, m := range result {
		if m.ReasoningContent == "" {
			t.Errorf("assistant %d: reasoning lost with < keepReasoning", i)
		}
	}
}

func TestBatchElide_PreservesKeepTail(t *testing.T) {
	msgs := make([]client.ChatMessage, 30)
	for i := range msgs {
		msgs[i] = msgTool("id_"+fmt.Sprint(i), strings.Repeat("x", 100))
	}
	result := batchElideToolResults(msgs, 500, "", nil)
	tail := result[30-keepTail:]
	for i, m := range tail {
		if m.Content.String() == elisionMark {
			t.Errorf("tail[%d] should not be batch-elided", i)
		}
	}
}

// ============================================================
// D. Real Session Integration Tests
// ============================================================

var realSessionsDir = `C:\Users\qxm22\AppData\Roaming\late\sessions`

// testSessions lists the real session files to test against.
// Selected for diversity: small/medium/large, various tool patterns.
var testSessions = []struct {
	file string // basename relative to realSessionsDir
	desc string
}{
	{"session-20260520-082628.json", "121 msg search+fetch (80K tok)"},
	{"session-20260613-083144.json", "42 msg research+ppt (28K tok)"},
	{"session-20260612-125756.json", "53 msg coding+read (17K tok, 6 read_file)"},
}

func TestCompact_AllRealSessions_HugeLimit_NoCorruption(t *testing.T) {
	for _, ts := range testSessions {
		t.Run(ts.desc, func(t *testing.T) {
			path := filepath.Join(realSessionsDir, ts.file)
			msgs := loadSessionJSON(t, path)
			if len(msgs) == 0 {
				return
			}

			// Huge limit: only normalization + semantic elision run.
			result := CompactMessages(msgs, "", nil, 999999)

			if len(result) != len(msgs) {
				t.Errorf("message count changed: %d → %d", len(msgs), len(result))
			}

			// Every result message must JSON-roundtrip.
			for i, m := range result {
				b, _ := json.Marshal(m)
				var back client.ChatMessage
				if err := json.Unmarshal(b, &back); err != nil {
					t.Errorf("[%d] not JSON-roundtrippable: %v", i, err)
				}
			}
		})
	}
}

func TestCompact_AllRealSessions_UnderPressure_ReducesTokens(t *testing.T) {
	for _, ts := range testSessions {
		t.Run(ts.desc, func(t *testing.T) {
			path := filepath.Join(realSessionsDir, ts.file)
			msgs := loadSessionJSON(t, path)
			if len(msgs) == 0 {
				return
			}
			// Half the actual token count → force batch elision.
			total := common.CalculateHistoryTokens(msgs, "", nil)
			limit := total / 2
			assertTokenReduction(t, msgs, ts.desc, limit)
		})
	}
}

func TestCompact_BigSession_LayeredPressure(t *testing.T) {
	// Test the 1MB session at multiple pressure levels.
	msgs := loadSessionJSON(t, filepath.Join(realSessionsDir, "session-20260518-210235.json"))
	if len(msgs) == 0 {
		return
	}

	total := common.CalculateHistoryTokens(msgs, "", nil)
	t.Logf("Big session: %d messages, ~%d tokens", len(msgs), total)

	// Level 1: huge limit → no batch elision (only normalize+semantic).
	t.Run("huge-limit", func(t *testing.T) {
		result := CompactMessages(msgs, "", nil, 999999)
		if len(result) != len(msgs) {
			t.Errorf("msg count changed: %d → %d", len(msgs), len(result))
		}
	})

	// Level 2: tight limit → batch elision activates.
	t.Run("tight-limit", func(t *testing.T) {
		assertTokenReduction(t, msgs, "big-session", total/2)
	})
}

func TestCompact_ZeroLimit_Skip(t *testing.T) {
	msgs := []client.ChatMessage{msg("user", "hello"), msg("assistant", "hi")}
	result := CompactMessages(msgs, "", nil, 0)
	if len(result) != 2 {
		t.Error("contextLimit=0 should skip and return unchanged")
	}
}
