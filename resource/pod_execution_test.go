package resource

import (
	"encoding/base64"
	"reflect"
	"strings"
	"testing"
)

func TestPodArgvNormalizationPreservesLiteralArgumentsAndBoundsSecrets(t *testing.T) {
	input := PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"printf", "; $(literal)\n", ""}, StdinBase64: base64.StdEncoding.EncodeToString([]byte{0, 1, 255})}
	normal, err := input.Normalized()
	if err != nil || !reflect.DeepEqual(normal.Argv, input.Argv) || normal.TimeoutSeconds != 10 || normal.MaxOutputBytes != 65536 || normal.StdinBase64 != input.StdinBase64 {
		t.Fatalf("normal=%+v err=%v", normal, err)
	}
	normal.Argv[0] = "changed"
	if input.Argv[0] != "printf" {
		t.Fatal("normalization changed caller arguments")
	}
	for name, mutate := range map[string]func(*PodArgvInput){
		"uid":           func(p *PodArgvInput) { p.ExpectedUID = "bad uid" },
		"container":     func(p *PodArgvInput) { p.Container = "../other" },
		"empty":         func(p *PodArgvInput) { p.Argv = nil },
		"executable":    func(p *PodArgvInput) { p.Argv = []string{" "} },
		"nul":           func(p *PodArgvInput) { p.Argv = []string{"printf", "PRIVATE_VALUE\x00"} },
		"argument-size": func(p *PodArgvInput) { p.Argv = []string{strings.Repeat("x", 8193)} },
		"argument-total": func(p *PodArgvInput) {
			p.Argv = []string{strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), strings.Repeat("x", 8192), "x"}
		},
		"timeout":          func(p *PodArgvInput) { p.TimeoutSeconds = 301 },
		"negative-timeout": func(p *PodArgvInput) { p.TimeoutSeconds = -1 },
		"output":           func(p *PodArgvInput) { p.MaxOutputBytes = PodArgvMaxOutputBytes + 1 },
		"negative-output":  func(p *PodArgvInput) { p.MaxOutputBytes = -1 },
		"stdin-encoding":   func(p *PodArgvInput) { p.StdinBase64 = "PRIVATE_VALUE!" },
		"stdin-size": func(p *PodArgvInput) {
			p.StdinBase64 = base64.StdEncoding.EncodeToString(make([]byte, PodExecMaxStdinBytes+1))
		},
	} {
		t.Run(name, func(t *testing.T) {
			value := input
			mutate(&value)
			_, err := value.Normalized()
			if err == nil || strings.Contains(err.Error(), "PRIVATE_VALUE") {
				t.Fatalf("unsafe validation: %v", err)
			}
		})
	}
}
