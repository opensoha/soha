package runner

import "testing"

func TestHermesChatReadinessKeepsFunctionalAndDiskPressureGuards(t *testing.T) {
	for _, tc := range []struct {
		name   string
		free   int64
		failed string
		want   bool
	}{
		{"capacity warning only", 18 << 30, "", true},
		{"low disk", 3 << 30, "", false},
		{"unknown disk", 0, "", false},
		{"model unavailable", 18 << 30, "model", false},
		{"database unavailable", 18 << 30, "state_db", false},
		{"unknown check", 18 << 30, "new_check", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := hermesHealth{Status: "degraded"}
			h.Readiness.Checks = map[string]hermesHealthCheck{"disk": {Status: "degraded", FreeBytes: tc.free}}
			for _, k := range []string{"state_db", "session_store", "config", "model", "gateway", "background_queues"} {
				h.Readiness.Checks[k] = hermesHealthCheck{Status: "ok"}
			}
			if tc.failed != "" {
				h.Readiness.Checks[tc.failed] = hermesHealthCheck{Status: "degraded"}
			}
			if h.readyForChat() != tc.want {
				t.Fatalf("ready=%v, want %v", h.readyForChat(), tc.want)
			}
		})
	}
	if (hermesHealth{Status: "degraded"}).readyForChat() {
		t.Fatal("missing details accepted")
	}
}
