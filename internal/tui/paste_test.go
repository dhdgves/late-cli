package tui

import (
	"context"
	"late/internal/client"
	"late/internal/common"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type mockOrchestrator struct {
	submittedText string
}

func (m *mockOrchestrator) ID() string { return "mock" }
func (m *mockOrchestrator) Submit(text string, images []string) error {
	m.submittedText = text
	return nil
}
func (m *mockOrchestrator) Execute(text string) (string, error) { return "", nil }
func (m *mockOrchestrator) Reset() error                         { return nil }
func (m *mockOrchestrator) Rewind(index int) error                { return nil }
func (m *mockOrchestrator) Cancel()                              {}
func (m *mockOrchestrator) IsStopRequested() bool                { return false }
func (m *mockOrchestrator) Events() <-chan common.Event          { return nil }
func (m *mockOrchestrator) History() []client.ChatMessage        { return nil }
func (m *mockOrchestrator) Context() context.Context             { return context.Background() }
func (m *mockOrchestrator) Middlewares() []common.ToolMiddleware { return nil }
func (m *mockOrchestrator) Registry() *common.ToolRegistry       { return nil }
func (m *mockOrchestrator) SystemPrompt() string                 { return "" }
func (m *mockOrchestrator) ToolDefinitions() []client.ToolDefinition { return nil }
func (m *mockOrchestrator) Children() []common.Orchestrator      { return nil }
func (m *mockOrchestrator) Parent() common.Orchestrator          { return nil }
func (m *mockOrchestrator) SetMaxTurns(int)                      {}
func (m *mockOrchestrator) RefreshContextSize(context.Context)   {}
func (m *mockOrchestrator) MaxTokens() int                       { return 100 }
func (m *mockOrchestrator) SupportsVision() bool                 { return false }
func (m *mockOrchestrator) QueuedMessages() []string             { return nil }
func (m *mockOrchestrator) CompactedTokens() int                 { return 0 }

type mockKey struct {
	code rune
	text string
}

func (k mockKey) String() string { return k.text }
func (k mockKey) Key() tea.Key {
	return tea.Key{Code: k.code, Text: k.text}
}

func TestPastePlaceholderReplacement(t *testing.T) {
	orch := &mockOrchestrator{}
	model := NewModel(orch, nil)

	// Simulate PasteMsg of 5 lines
	pasteText := "line1\nline2\nline3\nline4\nline5"
	msg := tea.PasteMsg{Content: pasteText}

	// Update
	res, _ := model.Update(msg)
	model = res.(Model)

	// Verify placeholder is inserted
	expectedPlaceholder := "[Pasted #5 lines]"
	inputVal := model.Input.Value()
	if !strings.Contains(inputVal, expectedPlaceholder) {
		t.Errorf("Expected input to contain %q, got %q", expectedPlaceholder, inputVal)
	}

	// Verify paste text is stored in mapping
	original, exists := model.Pastes[expectedPlaceholder]
	if !exists || original != pasteText {
		t.Errorf("Expected mapping from %q to %q, exists: %t, got %q", expectedPlaceholder, pasteText, exists, original)
	}

	// Simulate pressing enter/submitting
	msgEnter := mockKey{code: '\r', text: "enter"}
	res, _ = model.Update(msgEnter)
	model = res.(Model)

	// Verify original content was submitted
	if orch.submittedText != pasteText {
		t.Errorf("Expected submitted text to be %q, got %q", pasteText, orch.submittedText)
	}

	// Verify pastes mapping was cleared
	if len(model.Pastes) != 0 {
		t.Errorf("Expected model.Pastes to be empty after submission, got %d items", len(model.Pastes))
	}
}
