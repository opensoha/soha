package dockerruntime

import "testing"

func TestBoundedUTF8DoesNotSplitRunesOrRewriteInvalidBytes(t *testing.T) {
	for _, tc := range []struct {
		value     string
		limit     int
		truncated bool
		want      string
		cut       bool
		invalid   bool
	}{
		{"hello", 8, false, "hello", false, false},
		{"hello", 3, false, "hel", true, false},
		{"中文", 4, false, "中", true, false},
		{"中文", 3, false, "中", true, false},
		{string([]byte{0xe4, 0xb8}), 3, true, "", true, false},
		{string([]byte{0xff, 'a'}), 2, true, "", false, true},
		{"a\x00b", 3, false, "", false, true},
	} {
		got, cut, err := boundedUTF8(tc.value, tc.limit, tc.truncated)
		if (err != nil) != tc.invalid || got != tc.want || cut != tc.cut {
			t.Fatalf("UTF-8 bound: got %q cut=%v err=%v, want %q cut=%v invalid=%v", got, cut, err, tc.want, tc.cut, tc.invalid)
		}
	}
}
