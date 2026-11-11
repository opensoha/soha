package delivery

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func Digest(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:])
}

// CanonicalJSON implements RFC 8785 for parsed JSON values. Integers outside the
// interoperable range are rejected by the document parser before reaching here.
func CanonicalJSON(value any) ([]byte, error) {
	var output bytes.Buffer
	if err := appendCanonical(&output, value, 0); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func appendCanonical(output *bytes.Buffer, value any, depth int) error {
	if depth > 64 {
		return fmt.Errorf("canonical value exceeds nesting limit")
	}
	switch value := value.(type) {
	case nil:
		output.WriteString("null")
	case bool:
		output.WriteString(strconv.FormatBool(value))
	case string:
		return appendJSONString(output, value)
	case float64:
		return appendJSONNumber(output, value)
	case []any:
		output.WriteByte('[')
		for index, child := range value {
			if index > 0 {
				output.WriteByte(',')
			}
			if err := appendCanonical(output, child, depth+1); err != nil {
				return err
			}
		}
		output.WriteByte(']')
	case map[string]any:
		return appendJSONObject(output, value, depth)
	default:
		return fmt.Errorf("canonical value must be a parsed JSON value")
	}
	return nil
}

func appendJSONObject(output *bytes.Buffer, value map[string]any, depth int) error {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b string) int {
		return slices.Compare(utf16.Encode([]rune(a)), utf16.Encode([]rune(b)))
	})
	output.WriteByte('{')
	for index, key := range keys {
		if index > 0 {
			output.WriteByte(',')
		}
		if err := appendJSONString(output, key); err != nil {
			return err
		}
		output.WriteByte(':')
		if err := appendCanonical(output, value[key], depth+1); err != nil {
			return err
		}
	}
	output.WriteByte('}')
	return nil
}

func appendJSONString(output *bytes.Buffer, value string) error {
	if !utf8.ValidString(value) {
		return fmt.Errorf("canonical strings must be valid Unicode")
	}
	output.WriteByte('"')
	for _, r := range value {
		switch r {
		case '"', '\\':
			output.WriteByte('\\')
			output.WriteRune(r)
		case '\b':
			output.WriteString(`\b`)
		case '\t':
			output.WriteString(`\t`)
		case '\n':
			output.WriteString(`\n`)
		case '\f':
			output.WriteString(`\f`)
		case '\r':
			output.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(output, `\u%04x`, r)
			} else {
				output.WriteRune(r)
			}
		}
	}
	output.WriteByte('"')
	return nil
}

func appendJSONNumber(output *bytes.Buffer, value float64) error {
	if math.IsInf(value, 0) || math.IsNaN(value) {
		return fmt.Errorf("canonical numbers must be finite")
	}
	if value == 0 {
		output.WriteByte('0')
		return nil
	}
	format := byte('f')
	if math.Abs(value) < 1e-6 || math.Abs(value) >= 1e21 {
		format = 'e'
	}
	text := strconv.FormatFloat(value, format, -1, 64)
	text = strings.ReplaceAll(strings.ReplaceAll(text, "e-0", "e-"), "e+0", "e+")
	output.WriteString(text)
	return nil
}
