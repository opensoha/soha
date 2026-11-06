package cluster

import "testing"

func TestPrometheusSourceConflictRespectsTransportOnlyForLoopback(t *testing.T) {
	for _, tt := range []struct {
		name, source, leftTransport, rightTransport, leftAgent, rightAgent string
		conflict                                                           bool
	}{
		{"shared-on-different-agents", "http://prometheus.example:9090", "agent", "agent", "http://agent-a", "http://agent-b", true},
		{"shared-on-core-and-agent", "http://prometheus.example:9090", "direct", "agent", "", "http://agent-b", true},
		{"loopback-on-different-agents", "http://127.0.0.1:9090", "agent", "agent", "http://agent-a", "http://agent-b", false},
		{"loopback-on-core-and-agent", "http://localhost:9090", "direct", "agent", "", "http://agent-b", false},
		{"loopback-on-same-agent", "http://[::1]:9090", "agent", "agent", "http://agent-a", "http://agent-a", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			identity := func(id, transport, agent string) Connection {
				return Connection{Summary: Summary{ID: id}, Metadata: map[string]any{
					"prometheus_url": tt.source, "prometheus_transport": transport, "endpoint": agent,
					"prometheus_cluster_label": "cluster", "prometheus_cluster_label_value": "duplicate",
				}}
			}
			left, right := identity("left", tt.leftTransport, tt.leftAgent), identity("right", tt.rightTransport, tt.rightAgent)
			if got := PrometheusSourceConflict(left, right); got != tt.conflict {
				t.Fatalf("PrometheusSourceConflict() = %t, want %t", got, tt.conflict)
			}
		})
	}
}
