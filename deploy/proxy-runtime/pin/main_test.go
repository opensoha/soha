package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestLivePin(t *testing.T) {
	input := `{"kind":"StatefulSet","metadata":{"name":"proxy","namespace":"isolated","uid":"live-uid"},"spec":{"template":{"spec":{"containers":[{"name":"runtime","image":"approved"}]}}}}`
	var output bytes.Buffer
	if err := run(strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	var pin map[string]string
	if err := json.Unmarshal(output.Bytes(), &pin); err != nil {
		t.Fatal(err)
	}
	if pin["uid"] != "live-uid" || pin["workloadKind"] != "StatefulSet" || !strings.HasPrefix(pin["templateDigest"], "sha256:") || len(pin["templateDigest"]) != 71 {
		t.Fatal("invalid live pin", pin)
	}
	for _, invalid := range []string{`{}`, strings.Replace(input, `"uid":"live-uid"`, `"uid":""`, 1), strings.Replace(input, `"StatefulSet"`, `"Pod"`, 1)} {
		if err := run(strings.NewReader(invalid), &bytes.Buffer{}); err == nil {
			t.Fatal("accepted unpinnable workload")
		}
	}
}
