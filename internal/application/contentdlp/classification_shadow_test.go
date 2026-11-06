package contentdlp

import (
	"context"
	d "github.com/opensoha/soha/internal/domain/contentdlp"
	"strings"
	"testing"
)

func TestClassificationThresholdAndUnknown(t *testing.T) {
	c := d.Content{Enabled: true, Mode: "block", Detectors: []string{"private_key"}, Rules: []d.Rule{{ID: "internal", Expression: "INTERNAL", DataLevel: "internal"}}, MinimumDataLevel: "confidential"}
	cases := []struct {
		name, body, level string
		allowed           bool
	}{{"below threshold", "INTERNAL", "internal", true}, {"private key", "-----BEGIN PRIVATE KEY-----", "restricted", false}, {"unclassified", "ordinary text", "unclassified", true}, {"binary", "\x00", "unknown", false}}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := Inspect(context.Background(), c, []byte(tt.body))
			if got.DataLevel != tt.level || got.Allowed != tt.allowed {
				t.Fatalf("decision=%+v", got)
			}
		})
	}
	old := c
	old.MinimumDataLevel = ""
	if Inspect(context.Background(), old, []byte("INTERNAL")).Allowed {
		t.Fatal("legacy threshold must enforce all matches")
	}
	changed := c
	changed.MinimumDataLevel = "restricted"
	if RuleSetVersion(c) == RuleSetVersion(changed) {
		t.Fatal("threshold change must change the rule set version")
	}
	other := c
	other.Mode = "audit"
	if RuleSetVersion(c) != RuleSetVersion(other) {
		t.Fatal("mode is not detection input")
	}
	c.DetectorVersion = "unavailable-engine"
	if ValidatePolicy(c) == nil {
		t.Fatal("unknown engine accepted")
	}
}
func TestShadowComparesSavedDraftAndClearsSamples(t *testing.T) {
	draft := d.Content{Enabled: true, Mode: "block", Detectors: []string{"private_key"}, Rules: []d.Rule{}}
	published := draft
	published.Mode = "audit"
	sample := []byte("-----BEGIN PRIVATE KEY-----")
	got, err := Shadow(context.Background(), draft, &published, 3, 2, d.ShadowInput{ExpectedRevision: 3, Samples: [][]byte{sample}})
	if err != nil || got.WouldBlock != 1 || got.Changed != 1 || got.Samples[0].Published == nil || !got.Samples[0].Published.Allowed {
		t.Fatalf("shadow=%+v err=%v", got, err)
	}
	for _, b := range sample {
		if b != 0 {
			t.Fatal("sample retained")
		}
	}
	if _, err := Shadow(context.Background(), draft, nil, 3, 0, d.ShadowInput{ExpectedRevision: 2, Samples: [][]byte{[]byte("sample")}}); err == nil {
		t.Fatal("stale draft accepted")
	}
	if _, err := Shadow(context.Background(), draft, nil, 3, 0, d.ShadowInput{ExpectedRevision: 3, Samples: [][]byte{[]byte(strings.Repeat("x", 32769))}}); err == nil {
		t.Fatal("oversized sample accepted")
	}
}
