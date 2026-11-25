package sourceignore

import (
	"strings"
	"testing"
)

func TestProjectIgnorePrecedenceAndPatternForms(t *testing.T) {
	var rules Rules
	// Archive enumeration can place child control files before their parents.
	if err := rules.Add("nested", []byte("!keep.tmp\n/local.py\n")); err != nil {
		t.Fatal(err)
	}
	if err := rules.Add("excluded", []byte("!*.py\n")); err != nil {
		t.Fatal(err)
	}
	root := "# comment\r\n*.tmp\r\n!allow.tmp\nallow.tmp\nexcluded/\n!excluded/app.py\n" +
		"/root.txt\ncache/\napp/**/secret.txt\noutput/**\n!output/keep.py\n" +
		"private\\/notes.txt\nroot\\/\n\\/top.txt\n\\#name\n\\!name\nwith-space\\ \ntrail   \npart[!x]?.[[:digit:]]\n"
	if err := rules.Add("", []byte(root)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path       string
		isDir, yes bool
	}{
		{path: "allow.tmp", yes: true},
		{path: "nested/keep.tmp"},
		{path: "nested/drop.tmp", yes: true},
		{path: "nested/local.py", yes: true},
		{path: "nested/sub/local.py"},
		{path: "excluded/app.py", yes: true},
		{path: "root.txt", yes: true},
		{path: "private/notes.txt", yes: true},
		{path: "root/file.py"},
		{path: "top.txt"},
		{path: "nested/root.txt"},
		{path: "cache"},
		{path: "cache", isDir: true, yes: true},
		{path: "src/cache/file.py", yes: true},
		{path: "app/secret.txt", yes: true},
		{path: "app/deep/inner/secret.txt", yes: true},
		{path: "output", isDir: true},
		{path: "output/keep.py"},
		{path: "output/deep/file.py", yes: true},
		{path: "#name", yes: true},
		{path: "!name", yes: true},
		{path: "with-space ", yes: true},
		{path: "with-space"},
		{path: "trail", yes: true},
		{path: "partab.4", yes: true},
		{path: "partxb.4"},
	}
	for _, tc := range cases {
		if got := rules.Ignored(tc.path, tc.isDir); got != tc.yes {
			t.Errorf("Ignored(%q, %v) = %v, want %v", tc.path, tc.isDir, got, tc.yes)
		}
	}
}

func TestIgnoreLimitsLeaveExistingRulesUsable(t *testing.T) {
	var rules Rules
	if err := rules.Add("", []byte("secret.txt\n")); err != nil {
		t.Fatal(err)
	}
	for _, pattern := range []string{strings.Repeat("x", maxRuleBytes+1), strings.Repeat("x\n", maxRules+1), "nul\x00byte"} {
		if err := rules.Add("", []byte(pattern)); err == nil {
			t.Fatal("accepted invalid pattern input")
		}
	}
	if !rules.Ignored("secret.txt", false) {
		t.Fatal("rejected input replaced valid rules")
	}
}
