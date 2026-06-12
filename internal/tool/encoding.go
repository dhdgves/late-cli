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
// Chinese Windows (PowerShell and cmd.exe) default to GBK (CP936)
// console output, while the TUI renderer expects UTF-8.
//
// Strategy (try each, stop on first success):
//  1. Already valid UTF-8 → return as-is
//  2. Decode as GBK (CP936)    → if successful, return decoded bytes
//  3. Replace invalid bytes     → U+FFFD replacement character
func DetectAndConvert(raw []byte) (out []byte) {
	defer func() {
		if r := recover(); r != nil {
			out = []byte(strings.ToValidUTF8(string(raw), "\ufffd"))
		}
	}()

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
