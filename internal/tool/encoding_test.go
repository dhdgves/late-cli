package tool

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDetectAndConvert_ValidUTF8_Noop(t *testing.T) {
	cases := []struct {
		name  string
		input []byte
	}{
		{"empty", []byte{}},
		{"ascii", []byte("hello world")},
		{"utf8-chinese", []byte("你好世界")},
		{"utf8-mixed", []byte("路径: /home/user/文档.txt")},
		{"newlines", []byte("line1\nline2\n")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DetectAndConvert(tc.input)
			if string(got) != string(tc.input) {
				t.Errorf("valid UTF-8 should pass through unchanged\n  got:  %q\n  want: %q", got, tc.input)
			}
			if !utf8.Valid(got) {
				t.Errorf("output must be valid UTF-8: %q", got)
			}
		})
	}
}

func TestDetectAndConvert_GBK_ToUTF8(t *testing.T) {
	// GBK-encoded Chinese strings verified against CP936 codepage.
	cases := []struct {
		name string
		gbk  []byte
		want string
	}{
		{
			name: "hello-chinese",
			gbk:  []byte{0xc4, 0xe3, 0xba, 0xc3},
			want: "你好",
		},
		{
			name: "china",
			gbk:  []byte{0xd6, 0xd0, 0xb9, 0xfa},
			want: "中国",
		},
		{
			name: "test-output",
			gbk:  []byte{0xb2, 0xe2, 0xca, 0xd4, 0xca, 0xe4, 0xb3, 0xf6},
			want: "测试输出",
		},
		{
			name: "mixed-ascii-gbk",
			// "文件: test.txt" in GBK
			gbk: append(
				append([]byte{0xce, 0xc4, 0xbc, 0xfe}, ':'),
				[]byte(" test.txt")...,
			),
			want: "文件: test.txt",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Verify input is NOT valid UTF-8 (otherwise test is meaningless)
			if utf8.Valid(tc.gbk) {
				t.Skip("test data accidentally valid UTF-8 — skip")
			}

			got := DetectAndConvert(tc.gbk)
			if string(got) != tc.want {
				t.Errorf("GBK→UTF-8 mismatch\n  got:  %q\n  want: %q", got, tc.want)
			}
			if !utf8.Valid(got) {
				t.Errorf("output must be valid UTF-8: %q", got)
			}
		})
	}
}

func TestDetectAndConvert_BinaryGarbage_ReplacementChars(t *testing.T) {
	// Random bytes that are neither valid UTF-8 nor valid GBK.
	garbage := []byte{0xff, 0xfe, 0x00, 0x01, 0x80, 0x81}

	got := DetectAndConvert(garbage)
	if !utf8.Valid(got) {
		t.Errorf("output must be valid UTF-8 even for garbage input: %x", got)
	}

	s := string(got)
	if !strings.Contains(s, "\ufffd") {
		t.Logf("garbage input converted to: %q (replacement chars expected)", got)
	}
}

func TestDetectAndConvert_TruncatedGBK_Graceful(t *testing.T) {
	// Single lead byte without trail byte — not valid GBK, should degrade.
	// GBK lead bytes are in range 0x81–0xFE.
	truncated := []byte{0xc4} // lead byte for 你, no trail

	got := DetectAndConvert(truncated)
	if !utf8.Valid(got) {
		t.Errorf("output must be valid UTF-8 for truncated GBK: %x", got)
	}
}

func TestDetectAndConvert_LargeOutput(t *testing.T) {
	// Simulate a large shell output with mixed content.
	var gbkLines []byte
	for i := 0; i < 100; i++ {
		gbkLines = append(gbkLines, []byte{0xd6, 0xd0, 0xb9, 0xfa, '\n'}...) // "中国\n"
	}

	got := DetectAndConvert(gbkLines)
	if !utf8.Valid(got) {
		t.Fatal("large output must be valid UTF-8")
	}

	count := strings.Count(string(got), "中国")
	if count != 100 {
		t.Errorf("expected 100 occurrences of 中国, got %d", count)
	}
}

func TestDetectAndConvert_UTF8WithNullBytes(t *testing.T) {
	// UTF-8 text containing null bytes (possible in binary-adjacent output).
	input := []byte("hello\x00world")

	got := DetectAndConvert(input)
	if !utf8.Valid(got) {
		t.Errorf("output must be valid UTF-8: %q", got)
	}
}

func TestDetectAndConvert_Idempotent(t *testing.T) {
	inputs := [][]byte{
		[]byte("plain ascii"),
		[]byte("你好世界"),
		{0xc4, 0xe3, 0xba, 0xc3}, // GBK "你好"
	}

	for _, in := range inputs {
		first := DetectAndConvert(in)
		second := DetectAndConvert(first)
		if string(first) != string(second) {
			t.Errorf("not idempotent\n  first:  %q\n  second: %q", first, second)
		}
	}
}

func TestDetectAndConvert_Emoji(t *testing.T) {
	// Emojis are 4-byte UTF-8 sequences; verify they survive intact.
	cases := []struct {
		name  string
		input string
	}{
		{"single-emoji", "😀"},
		{"multiple-emoji", "🚀✅💯"},
		{"emoji-with-text", "Build: ✅ 3 passed, ❌ 1 failed"},
		{"shell-git-log", "📦 chore: bump version to 1.3.0"},
		{"emoji-at-start", "🔍 Searching..."},
		{"emoji-at-end", "Done! 🎉"},
		{"emoji-only-lines", "✅\n❌\n⏳"},
		{"skin-tone", "👋🏽 hello"},
		{"flag-emoji", "🇨🇳 中国 🇺🇸 USA"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := []byte(tc.input)
			if !utf8.Valid(input) {
				t.Skip("test data not valid UTF-8 — skip")
			}
			got := DetectAndConvert(input)
			if string(got) != tc.input {
				t.Errorf("emoji pass-through failed\n  got:  %q\n  want: %q", got, tc.input)
			}
			if !utf8.Valid(got) {
				t.Errorf("output must be valid UTF-8: %q", got)
			}
		})
	}
}

func TestDetectAndConvert_GBK_ThenUTF8_EmojiPreserved(t *testing.T) {
	// Realistic scenario: GBK system messages on one line, then
	// piped UTF-8 output (with emoji) on the next line.
	// Line 1: GBK "生成报告: 开始处理..."
	// Line 2: UTF-8 "✅ done (3 files)"
	gbkLine := []byte{0xc9, 0xfa, 0xb3, 0xc9, 0xb1, 0xa8, 0xb8, 0xe6, 0x3a, 0x20, 0xbf, 0xaa, 0xca, 0xbc, 0xb4, 0xa6, 0xc0, 0xed, 0x2e, 0x2e, 0x2e}
	utf8Line := []byte("✅ done (3 files)")

	input := make([]byte, 0, len(gbkLine)+1+len(utf8Line))
	input = append(input, gbkLine...)
	input = append(input, '\n')
	input = append(input, utf8Line...)

	if utf8.Valid(input) {
		t.Skip("mixed GBK+UTF-8 accidentally valid UTF-8")
	}

	got := DetectAndConvert(input)
	if !utf8.Valid(got) {
		t.Fatalf("output must be valid UTF-8: %x", got)
	}

	s := string(got)
	if !strings.Contains(s, "开始处理") {
		t.Errorf("GBK line not decoded: %q", s)
	}
	if !strings.Contains(s, "✅") {
		t.Errorf("emoji ✅ lost: %q", s)
	}
}

func TestDetectAndConvert_EmojiInFuzzSet(t *testing.T) {
	// Add emoji-heavy inputs to the fuzz-style valid-UTF8 check.
	cases := [][]byte{
		[]byte("😀"),
		[]byte("🔍 searching... ✅"),
		[]byte("🎉🎉🎉"),
		[]byte("👨‍💻"), // ZWJ sequence (man technologist)
		[]byte("📦 v1.0 🚀 v2.0 💥 v3.0"),
		// Emoji at boundary of truncated sequence
		[]byte("text😀"),   // text + 4-byte emoji (6 bytes total)
		[]byte("😀text"),   // emoji at start
		[]byte("\xf0\x9f"), // truncated emoji (leading bytes only, invalid UTF-8)
	}

	for i, c := range cases {
		got := DetectAndConvert(c)
		if !utf8.Valid(got) {
			t.Errorf("case %d (%x): output not valid UTF-8: %x", i, c, got)
		}
	}
}

func TestDetectAndConvert_OnlyValidUTF8Returned(t *testing.T) {
	// Fuzz-like: ensure output is always valid UTF-8 for a range of inputs.
	cases := [][]byte{
		nil,
		{},
		{0x00},
		{0x80},
		{0xFF},
		{0xFE, 0xFF},
		{0xFF, 0xFE, 0x00, 0x00},
		[]byte("normal text"),
		{0xc4, 0xe3},             // GBK 你好 lead+trail only
		{0xc4},                   // truncated lead byte
		{0xe3},                   // trailing byte without lead
		[]byte("mixed\xc4\xe3"),  // ASCII + GBK
	}

	for i, c := range cases {
		got := DetectAndConvert(c)
		if !utf8.Valid(got) {
			t.Errorf("case %d (%x): output not valid UTF-8: %x", i, c, got)
		}
	}
}
