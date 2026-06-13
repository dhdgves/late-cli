package session

import (
	"encoding/json"
	"late/internal/client"

	"late/internal/common"
)

const (
	// Thresholds as fraction of context limit.
	compactNone     = 0.55 // below this: no compaction
	compactSemantic = 0.70 // below this: semantic elision only; above: batch elision too

	// keepTail preserves the last N messages from batch elision.
	keepTail = 24

	// Elision mark replaces superseded tool results.
	elisionMark = "[elided — see earlier in conversation]"
)

// CompactMessages applies a ruthless compaction pipeline to the message
// history, reducing token count while preserving essential information.
//
// Pipeline:
//  1. Token estimate → skip if under compactNone threshold
//  2. dropSupersededReads — elide read_file results whose file was later
//     mutated by write_file/target_edit, or re-read more recently
//  3. If still over compactSemantic threshold, batch-elide old tool-results
//     from the head, preserving the last keepTail messages
//
// Returns a new slice; the input is never mutated.
func CompactMessages(history []client.ChatMessage, systemPrompt string, tools []client.ToolDefinition, contextLimit int) []client.ChatMessage {
	if contextLimit <= 0 {
		return history // no context info, skip
	}

	total := common.CalculateHistoryTokens(history, systemPrompt, tools)
	threshold := int(float64(contextLimit) * compactNone)
	if total < threshold {
		return history
	}

	// Work on a copy.
	out := make([]client.ChatMessage, len(history))
	copy(out, history)

	// Stage 1: semantic elision — kill superseded reads.
	if total < int(float64(contextLimit)*compactSemantic) {
		out = dropSupersededReads(out)
		return out
	}

	// Stage 2: semantic + batch elision.
	out = dropSupersededReads(out)
	out = batchElideToolResults(out, contextLimit, systemPrompt, tools)

	return out
}

// dropSupersededReads elides read_file tool-results where the file was
// later mutated (write_file/target_edit) or re-read more recently.
// This is the most impactful single compression technique for coding agents.
func dropSupersededReads(messages []client.ChatMessage) []client.ChatMessage {
	// Phase A: collect paths mutated by write_file or target_edit.
	mutated := make(map[string]bool)
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name != "write_file" && tc.Function.Name != "target_edit" {
				continue
			}
			if p := extractFilePath(tc.Function.Name, tc.Function.Arguments); p != "" {
				mutated[p] = true
			}
		}
	}
	if len(mutated) == 0 {
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
	for i := range messages {
		m := &messages[i]
		if m.Role != "tool" || m.ToolCallID == "" {
			continue
		}
		path, ok := toolIDToPath[m.ToolCallID]
		if !ok {
			continue
		}
		// Elide if file was later mutated OR there's a newer read of same file.
		if mutated[path] || lastReadIdx[path] > i {
			m.Content = client.TextContent(elisionMark)
		}
	}

	return messages
}

// batchElideToolResults elides old tool-results from the head of the
// conversation until the token count drops below compactSemantic.
// system messages and the last keepTail messages are never touched.
func batchElideToolResults(messages []client.ChatMessage, contextLimit int, systemPrompt string, tools []client.ToolDefinition) []client.ChatMessage {
	target := int(float64(contextLimit) * compactNone) // aim to get below 55%

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
