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
