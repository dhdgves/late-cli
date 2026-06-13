package session

import (
	"testing"
)

func TestExtractFilePath_ReadFile(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"normal path", `{"path":"foo.go"}`, "foo.go"},
		{"nested path", `{"path":"src/bar/main.go"}`, "src/bar/main.go"},
		{"windows path backslash", `{"path":"c:\\foo\\bar.go"}`, `c:\foo\bar.go`},
		{"unix path with unicode", `{"path":"/tmp/中文/文件.go"}`, "/tmp/中文/文件.go"},
		{"empty path field", `{"path":""}`, ""},
		{"missing path field", `{"other":"x"}`, ""},
		{"extra fields ignored", `{"path":"a.go","line":10}`, "a.go"},
	}

	for _, c := range cases {
		t.Run("read_"+c.name, func(t *testing.T) {
			got := extractFilePath("read_file", c.args)
			if got != c.want {
				t.Errorf("extractFilePath(read_file, %q) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestExtractFilePath_WriteFile(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"normal path", `{"path":"foo.go"}`, "foo.go"},
		{"missing path", `{"content":"x"}`, ""},
		{"empty path", `{"path":""}`, ""},
	}

	for _, c := range cases {
		t.Run("write_"+c.name, func(t *testing.T) {
			got := extractFilePath("write_file", c.args)
			if got != c.want {
				t.Errorf("extractFilePath(write_file, %q) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestExtractFilePath_TargetEdit(t *testing.T) {
	cases := []struct {
		name string
		args string
		want string
	}{
		{"file field", `{"file":"foo.go","search":"x","replace":"y"}`, "foo.go"},
		{"missing file", `{"search":"x","replace":"y"}`, ""},
		{"empty file", `{"file":""}`, ""},
		{"path field ignored by target_edit", `{"path":"wrong.go","file":"right.go"}`, "right.go"},
	}

	for _, c := range cases {
		t.Run("edit_"+c.name, func(t *testing.T) {
			got := extractFilePath("target_edit", c.args)
			if got != c.want {
				t.Errorf("extractFilePath(target_edit, %q) = %q, want %q", c.args, got, c.want)
			}
		})
	}
}

func TestExtractFilePath_EdgeCases(t *testing.T) {
	cases := []struct {
		tool string
		args string
		want string
	}{
		{"read_file", ``, ""},
		{"read_file", `not json`, ""},
		{"read_file", `null`, ""},
		{"read_file", `42`, ""}, // valid JSON but not object
		{"read_file", `[1,2]`, ""},
		{"bash", `{"path":"x.go"}`, ""},              // unknown tool name
		{"", `{"path":"x.go"}`, ""},                   // empty tool name
		{"write_file", `{"path":"a.go"}`, "a.go"},    // sanity check
	}

	for _, c := range cases {
		t.Run(c.tool+"_"+c.args[:min(len(c.args),12)], func(t *testing.T) {
			got := extractFilePath(c.tool, c.args)
			if got != c.want {
				t.Errorf("extractFilePath(%q, %q) = %q, want %q", c.tool, c.args, got, c.want)
			}
		})
	}
}

func TestExtractFilePath_LargePath(t *testing.T) {
	large := make([]byte, 10000)
	for i := range large {
		large[i] = 'x'
	}
	args := `{"path":"` + string(large) + `"}`
	got := extractFilePath("read_file", args)
	if got != string(large) {
		t.Errorf("large path not correctly extracted: len=%d want=%d", len(got), len(large))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
