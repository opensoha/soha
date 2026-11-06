package cluster

import (
	"net"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var prometheusLabelPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidPrometheusMetricIdentity accepts URL-only dedicated sources and labels
// that can identify a cluster independently of the metric dimensions.
func ValidPrometheusMetricIdentity(label, value string) bool {
	if label == "" {
		return value == ""
	}
	if !prometheusLabelPattern.MatchString(label) || strings.HasPrefix(label, "__") || utf8.RuneCountInString(value) > 1024 {
		return false
	}
	switch label {
	case "namespace", "pod", "node", "uid", "container", "image", "instance", "job",
		"device", "interface", "cpu", "mode", "phase", "resource", "unit", "nodename", "id":
		return false
	}
	return true
}

// PrometheusSourceConflict identifies ambiguous configuration on the same source.
// The same loopback URL can identify separate sources on different Agents.
func PrometheusSourceConflict(left, right Connection) bool {
	if left.Summary.ID == right.Summary.ID {
		return false
	}
	leftURL := strings.TrimRight(prometheusMetadata(left, "prometheus_url"), "/")
	rightURL := strings.TrimRight(prometheusMetadata(right, "prometheus_url"), "/")
	if leftURL == "" || leftURL != rightURL || prometheusSourceLocation(left, leftURL) != prometheusSourceLocation(right, rightURL) {
		return false
	}
	leftLabel := prometheusMetadata(left, "prometheus_cluster_label")
	rightLabel := prometheusMetadata(right, "prometheus_cluster_label")
	if leftLabel == "" || leftLabel != rightLabel {
		return true
	}
	return prometheusSourceValue(left) == prometheusSourceValue(right)
}

func prometheusSourceLocation(connection Connection, baseURL string) string {
	endpoint, err := url.Parse(baseURL)
	if err != nil || !prometheusLoopbackHost(endpoint.Hostname()) {
		return "shared"
	}
	if prometheusMetadata(connection, "prometheus_transport") != "agent" {
		return "core"
	}
	if endpoint := prometheusMetadata(connection, "endpoint"); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return connection.Summary.ID
}

func prometheusLoopbackHost(host string) bool {
	return strings.EqualFold(strings.TrimSuffix(host, "."), "localhost") || net.ParseIP(host).IsLoopback()
}

func prometheusSourceValue(connection Connection) string {
	if value := prometheusMetadata(connection, "prometheus_cluster_label_value"); value != "" {
		return value
	}
	return connection.Summary.ID
}

func prometheusMetadata(connection Connection, key string) string {
	value, _ := connection.Metadata[key].(string)
	return strings.TrimSpace(value)
}
