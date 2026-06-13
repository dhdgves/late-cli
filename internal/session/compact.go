package session

import (
	"encoding/json"
	"late/internal/client"

	"late/internal/common"
)

const (
	// compactSemantic triggers batch elision when total tokens exceed this
	// fraction of the context limit.  Below this, only semantic elision
	// (dropSupersededReads) runs — which is always-on and lossless.
	compactSemantic = 0.70

	// keepTail preserves the last N messages from batch elision.
	keepTail = 24

	// keepReasoning preserves reasoning content on the last K assistant
	// messages.  Reasoning older than this was consumed in decisions
	// already made and is dead weight.
	keepReasoning = 12

	// Elision mark replaces superseded tool results.
	elisionMark = "[elided — see earlier in conversation]"
)

// CompactMessages applies ruthless context compaction to the message
// history, reducing token count while preserving essential information.
//
// Pipeline:
//  0. NormalizeMessages — repair tool-call pairing, reorder tool results,
//     drop orphans, backfill placeholders. Ensures deterministic prefix
//     for KV-cache reuse.
//  1. dropSupersededReads — ALWAYS runs. Elides read_file results whose
//     file was later mutated by write_file/target_edit. This is lossless.
//  2. If still over compactSemantic threshold:
//     a. stripOldReasoning — clear reasoning from assistant messages
//        older than keepReasoning from the end
//     b. batchElideToolResults — elide old tool-results from the head,
//        preserving the last keepTail messages
//
// Returns a new slice; the input is never mutated.
func CompactMessages(history []client.ChatMessage, systemPrompt string, tools []client.ToolDefinition, contextLimit int) []client.ChatMessage {
	if contextLimit <= 0 {
		return history // no context info, skip
	}

	// Stage 0: normalize message structure for deterministic KV-cache prefix.
	out := NormalizeMessages(history)

	// Stage 1: always run semantic elision — zero-cost, zero-risk.
	out = dropSupersededReads(out)

	// Stage 2: batch elision only when we're genuinely over threshold.
	total := common.CalculateHistoryTokens(out, systemPrompt, tools)
	if total < int(float64(contextLimit)*compactSemantic) {
		return out
	}

	// Strip old reasoning before batch tool-result elision — it's
	// the cheaper win (single field clear vs content replacement).
	out = stripOldReasoning(out)
	out = batchElideToolResults(out, contextLimit, systemPrompt, tools)

	return out
}

// dropSupersededReads elides read_file tool-results where the file was
// later mutated (write_file/target_edit) or re-read more recently.
// This is the most impactful single compression technique for coding agents.
func dropSupersededReads(messages []client.ChatMessage) []client.ChatMessage {
	// Phase A: collect the FIRST mutation index per path.
	// A read is only superseded if a mutation (write_file/target_edit)
	// happened AFTER that read. Boolean set loses temporal ordering.
	firstMutation := make(map[string]int)
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "write_file" && tc.Function.Name != "target_edit" {
				continue
			}
			p := extractFilePath(tc.Function.Name, tc.Function.Arguments)
			if p == "" {
				continue
			}
			if _, exists := firstMutation[p]; !exists {
				firstMutation[p] = i // first write/edit to this file
			}
		}
	}
	if len(firstMutation) == 0 {
		return messages // nothing mutated, no superseded reads
	}

	// Phase B: find last read_file index per path.
	lastReadIdx := make(map[string]int)
	toolIDToPath := make(map[string]string)
	for i, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "read_file" {
				continue
			}
			p := extractFilePath(tc.Function.Name, tc.Function.Arguments)
			if p == "" {
				continue
			}
			lastReadIdx[p] = i
			toolIDToPath[tc.ID] = p
		}
	}
	if len(lastReadIdx) == 0 && len(toolIDToPath) == 0 {
		return messages
	}

	// Phase C: elide tool-results that are superseded.
	// A read is superseded if a mutation happened AFTER it, OR if
	// there's a newer read of the same file.
	for i := range messages {
		m := &messages[i]
		if m.Role != "tool" || m.ToolCallID == "" {
			continue
		}
		path, ok := toolIDToPath[m.ToolCallID]
		if !ok {
			continue
		}
		// Elide if mutation index > read index (mutation happened AFTER this read)
		if mtIdx, exists := firstMutation[path]; exists && mtIdx > i {
			m.Content = client.TextContent(elisionMark)
			continue
		}
		// Elide if there's a newer read of the same file
		if lastReadIdx[path] > i {
			m.Content = client.TextContent(elisionMark)
		}
	}

	return messages
}

// batchElideToolResults elides old tool-results from the head of the
// conversation until the token count drops below compactSemantic.
// system messages and the last keepTail messages are never touched.
func batchElideToolResults(messages []client.ChatMessage, contextLimit int, systemPrompt string, tools []client.ToolDefinition) []client.ChatMessage {
	target := int(float64(contextLimit) * compactSemantic)

	stopIdx := len(messages) - keepTail
	if stopIdx < 0 {
		stopIdx = 0
	}

	for i := 0; i < stopIdx; i++ {
		m := &messages[i]
		if m.Role != "tool" || m.Content.String() == elisionMark {
			continue // already elided or not a tool result
		}

		// Save old content, apply elision, check if we're done.
		old := m.Content
		m.Content = client.TextContent(elisionMark)
		total := common.CalculateHistoryTokens(messages, systemPrompt, tools)
		if total < target {
			return messages
		}
		// Still over target — keep going. But if restoring would
		// barely change anything (very short tool result), just keep it.
		_ = old
	}

	return messages
}

// stripOldReasoning clears ReasoningContent from assistant messages older
// than keepReasoning from the end of the conversation.  The model only
// needs recent reasoning to maintain continuity; reasoning about decisions
// long since made is dead context.
func stripOldReasoning(messages []client.ChatMessage) []client.ChatMessage {
	// Count assistant messages from the end to find the cutoff.
	assistantCount := 0
	cutoff := 0 // default: clear nothing
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			assistantCount++
			if assistantCount == keepReasoning {
				cutoff = i // clear reasoning on everything before this
				break
			}
		}
	}
	// If we have fewer than keepReasoning assistants total,
	// cutoff stays at 0 — keep all reasoning.

	// Clear reasoning on all assistant messages before the cutoff.
	for i := 0; i < cutoff; i++ {
		m := &messages[i]
		if m.Role == "assistant" && m.ReasoningContent != "" {
			m.ReasoningContent = ""
		}
	}

	return messages
}

// extractFilePath attempts to extract the file path from a tool-call's
// JSON arguments. Returns empty string if the path cannot be determined.
func extractFilePath(toolName string, argsJSON string) string {
	switch toolName {
	case "read_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) == nil && args.Path != "" {
			return args.Path
		}
	case "write_file":
		var args struct {
			Path string `json:"path"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) == nil && args.Path != "" {
			return args.Path
		}
	case "target_edit":
		var args struct {
			File string `json:"file"`
		}
		if json.Unmarshal([]byte(argsJSON), &args) == nil && args.File != "" {
			return args.File
		}
	}
	return ""
}
