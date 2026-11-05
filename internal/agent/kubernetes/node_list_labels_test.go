package kubernetes

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestNodeListLabelsAreCopied(t *testing.T) {
	node := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "worker", Labels: map[string]string{"pool": "gpu"}}}
	views := buildNodeViews([]corev1.Node{node}, nil)
	if len(views) != 1 || views[0].Labels["pool"] != "gpu" {
		t.Fatal("node list lost labels")
	}
	views[0].Labels["pool"] = "changed"
	if node.Labels["pool"] != "gpu" {
		t.Fatal("view aliases cached node labels")
	}
}
