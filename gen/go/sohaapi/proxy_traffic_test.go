package sohaapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Existing SDK consumers can still assign both numeric rate fields to float32.
var (
	_ float32 = NetworkProxyTrafficSample{}.UploadBytesPerSecond
	_ float32 = NetworkProxyTrafficSample{}.DownloadBytesPerSecond
)

func TestProxyTrafficRateWireCompatibility(t *testing.T) {
	for _, tt := range []struct {
		name  string
		state NetworkProxyTrafficSampleRateState
		up    float32
	}{
		{"legacy-numeric.json", "", 1.25},
		{"unknown-rate.json", NetworkProxyTrafficSampleRateStateUnknown, 0},
		{"gap-rate.json", NetworkProxyTrafficSampleRateStateGap, 0},
		{"known-zero.json", NetworkProxyTrafficSampleRateStateKnown, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "..", "fixtures", "openapi", "NetworkProxyTrafficSample", "valid", tt.name))
			if err != nil {
				t.Fatal(err)
			}
			var sample NetworkProxyTrafficSample
			if err := json.Unmarshal(raw, &sample); err != nil {
				t.Fatal(err)
			}
			if sample.RateState != tt.state || sample.UploadBytesPerSecond != tt.up || sample.DownloadBytesPerSecond != 0 {
				t.Fatalf("sample = %+v", sample)
			}
		})
	}
}
