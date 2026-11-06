package resource

import (
	"encoding/json"
	"testing"
)

func TestRolloutTemplateMatchDistinguishesUnknownFromMismatch(t *testing.T) {
	var legacy RolloutHistoryView
	if err := json.Unmarshal([]byte(`{"name":"revision-a","namespace":"team","revision":"1","replicas":0,"readyReplicas":0}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.MatchesCurrentTemplate != nil || legacy.PodRevisionHash != "" {
		t.Fatal("legacy history must not invent template match or Pod hash")
	}
	mismatch := false
	legacy.MatchesCurrentTemplate = &mismatch
	legacy.PodRevisionHash = "observed-hash"
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(data, &wire); err != nil {
		t.Fatal(err)
	}
	if value, ok := wire["matchesCurrentTemplate"]; !ok || value != false {
		t.Fatal("confirmed mismatch must survive JSON encoding")
	}
	if wire["podRevisionHash"] != "observed-hash" {
		t.Fatal("observed hash must survive JSON encoding")
	}
}
