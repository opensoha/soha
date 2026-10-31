package sohacli

import (
	"bytes"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"strings"
	"testing"
	"unicode"
)

func TestHumanLogOutputCannotExecuteControlsOrForgeRows(t *testing.T) {
	entry := LogEntry{Message: "正常 log\x1b]52;c;clipboard\x07\rforged\nrow\u202e", Source: sohaapi.LogSource{Domain: "cluster", PodName: "pod\x1b[2J"}}
	var out bytes.Buffer
	if err := writeLogPage(&out, "text", LogPage{Entries: []LogEntry{entry}}); err != nil {
		t.Fatal(err)
	}
	value := out.String()
	if strings.Count(value, "\n") != 1 || !strings.Contains(value, "正常 log") || !strings.Contains(value, `\nrow\u202e`) {
		t.Fatalf("unexpected visible output: %q", value)
	}
	for _, r := range strings.TrimSuffix(value, "\n") {
		if unicode.IsControl(r) && r != '\t' || r == '\u202e' {
			t.Fatalf("terminal control survived: %U", r)
		}
	}
}
