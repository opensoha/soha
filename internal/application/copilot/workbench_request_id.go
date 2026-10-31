package copilot

import "unicode/utf8"

// JSON Schema maxLength counts Unicode code points. Keep the opaque value
// unchanged; required/empty semantics belong to each caller.
func workbenchRequestIDWithinLimit(value string) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= 128
}
