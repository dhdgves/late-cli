package common

import (
	"late/internal/client"
	"testing"
)

func TestReplacePlaceholders(t *testing.T) {
	tests := []struct {
		text         string
		placeholders map[string]string
		expected     string
	}{
		{
			text:         "Hello ${{CWD}}",
			placeholders: map[string]string{"${{CWD}}": "/tmp"},
			expected:     "Hello /tmp",
		},
		{
			text:         "No placeholder here",
			placeholders: map[string]string{"${{CWD}}": "/tmp"},
			expected:     "No placeholder here",
		},
		{
			text:         "Multiple ${{CWD}} in ${{CWD}}",
			placeholders: map[string]string{"${{CWD}}": "/home"},
			expected:     "Multiple /home in /home",
		},
	}

	for _, tt := range tests {
		result := ReplacePlaceholders(tt.text, tt.placeholders)
		if result != tt.expected {
			t.Errorf("ReplacePlaceholders(%q, %v) = %q; want %q", tt.text, tt.placeholders, result, tt.expected)
		}
	}
}

func TestEstimateTokenCount(t *testing.T) {
	tests := []struct {
		text     string
		expected int
	}{
		{"", 0},
		{"a", 0},          // 1/2.0 = 0
		{"abcd", 2},       // 4/2.0 = 2
		{"abcde", 2},      // 5/2.0 = 2
		{"12345678", 4},   // 8/2.0 = 4
		{"123456789", 4},  // 9/2.0 = 4
		{"1234567890", 5}, // 10/2.0 = 5
		{"this is a test", 7}, // 14/2.0 = 7
	}

	for _, tt := range tests {
		result := EstimateTokenCount(tt.text)
		if result != tt.expected {
			t.Errorf("EstimateTokenCount(%q) = %d; want %d", tt.text, result, tt.expected)
		}
	}
}

func TestEstimateMessageTokens(t *testing.T) {
	msg := client.ChatMessage{
		Role:             "assistant",
		Content:          client.TextContent("Hello"),
		ReasoningContent: "Thinking...",
		ToolCalls: []client.ToolCall{
			{
				Function: client.FunctionCall{
					Name:      "test_tool",
					Arguments: `{"arg1": "val1"}`,
				},
			},
		},
	}

	// "Hello" = 5/2.0 = 2
	// "Thinking..." = 11/2.0 = 5
	// "test_tool" = 9/2.0 = 4
	// `{"arg1": "val1"}` = 16/2.0 = 8
	// Message overhead = 4
	// Total = 2 + 5 + 4 + 8 + 4 = 23
	expected := 23
	result := EstimateMessageTokens(msg)
	if result != expected {
		t.Errorf("EstimateMessageTokens() = %d; want %d", result, expected)
	}
}

func TestEstimateEventTokens(t *testing.T) {
	event := ContentEvent{
		Content:          "Part1",
		ReasoningContent: "Reason",
	}

	// "Part1" = 5/2.0 = 2
	// "Reason" = 6/2.0 = 3
	expected := 5
	result := EstimateEventTokens(event)
	if result != expected {
		t.Errorf("EstimateEventTokens() = %d; want %d", result, expected)
	}
}

func TestCalculateHistoryTokens(t *testing.T) {
	tests := []struct {
		name         string
		history      []client.ChatMessage
		systemPrompt string
		tools        []client.ToolDefinition
		expected     int
	}{
		{
			name:         "empty history with system prompt",
			history:      []client.ChatMessage{},
			systemPrompt: "You are an assistant",
			tools:        nil,
			expected:     20, // "You are an assistant" = 20/2.0 = 10 + 10 overhead = 20
		},
		{
			name: "single user message",
			history: []client.ChatMessage{
				{
					Role:    "user",
					Content: client.TextContent("Hello"),
				},
			},
			systemPrompt: "",
			tools:        nil,
			expected:     16, // System overhead (10) + msg content (2) + msg overhead (4) = 16
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CalculateHistoryTokens(tt.history, tt.systemPrompt, tt.tools)
			if result != tt.expected {
				t.Errorf("CalculateHistoryTokens() = %d; want %d", result, tt.expected)
			}
		})
	}
}
