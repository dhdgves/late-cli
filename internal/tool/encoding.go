package tool

import (
	"bytes"
	"io"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// DetectAndConvert detects the encoding of raw shell output bytes and
// converts them to valid UTF-8.  This is necessary because shells on
// Chinese Windows default to GBK (CP936) console output, while the TUI
// renderer expects UTF-8.
//
// Strategy:
//  1. Already valid UTF-8 → return as-is (fast path)
//  2. Split by newline, process each line independently:
//     a. Line is valid UTF-8         → keep as-is
//     b. Line decodes as GBK (CP936) → convert to UTF-8
//     c. Neither works               → replace invalid bytes with U+FFFD
//
// Per-line processing is critical because real shell output can mix
// encodings across lines: a GBK-localized PS1 prompt followed by UTF-8
// piped output, or GBK error messages next to UTF-8 file listings.
func DetectAndConvert(raw []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			out = []byte(strings.ToValidUTF8(string(raw), "\ufffd"))
		}
	}()

	if utf8.Valid(raw) {
		return raw
	}

	// No newlines → process the whole thing as one chunk.
	if !bytes.Contains(raw, []byte{'\n'}) {
		return convertChunk(raw)
	}

	lines := splitLines(raw)
	var buf bytes.Buffer
	buf.Grow(len(raw) + len(raw)/4) // pre-allocate: GBK→UTF-8 may expand

	for i, line := range lines {
		if i > 0 {
			buf.WriteByte('\n')
		}
		buf.Write(convertLine(line))
	}

	return buf.Bytes()
}

// splitLines splits raw bytes by '\n', preserving empty trailing lines
// (important for faithfully reproducing shell output).
func splitLines(raw []byte) [][]byte {
	if len(raw) == 0 {
		return nil
	}
	// Drop trailing empty string from bytes.Split.
	// We manually re-add the trailing newline to match original behaviour.
	lines := bytes.Split(raw, []byte{'\n'})
	return lines
}

// convertChunk converts a single chunk (no newlines) to valid UTF-8.
func convertChunk(raw []byte) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	if decoded := decodeGBK(raw); decoded != nil {
		return decoded
	}
	return []byte(strings.ToValidUTF8(string(raw), "\ufffd"))
}

// convertLine converts a single line to valid UTF-8, trying GBK decode
// if the line is not already valid UTF-8.
func convertLine(raw []byte) []byte {
	if utf8.Valid(raw) {
		return raw
	}
	if decoded := decodeGBK(raw); decoded != nil {
		return decoded
	}
	return []byte(strings.ToValidUTF8(string(raw), "\ufffd"))
}

// decodeGBK attempts to decode raw bytes as GBK (CP936).
// Returns nil if the byte sequence is not valid GBK.
func decodeGBK(raw []byte) []byte {
	decoder := simplifiedchinese.GBK.NewDecoder()
	decoded, err := io.ReadAll(transform.NewReader(bytes.NewReader(raw), decoder))
	if err != nil {
		return nil
	}
	return decoded
}
