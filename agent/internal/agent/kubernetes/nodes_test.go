package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestBuildNodeDetailIncludesTaints(t *testing.T) {
	detail := buildNodeDetail(corev1.Node{
		Spec: corev1.NodeSpec{Taints: []corev1.Taint{{
			Key: "dedicated", Value: "gpu", Effect: corev1.TaintEffectNoSchedule,
		}}},
	}, nil)

	if len(detail.Taints) != 1 || detail.Taints[0].Key != "dedicated" || detail.Taints[0].Effect != "NoSchedule" {
		t.Fatalf("unexpected taints: %#v", detail.Taints)
	}
}

func TestNodeViewsPreserveSchedulability(t *testing.T) {
	for _, cordoned := range []bool{true, false} {
		node := corev1.Node{Spec: corev1.NodeSpec{Unschedulable: cordoned}}
		if detail := buildNodeDetail(node, nil); detail.Unschedulable != cordoned {
			t.Fatalf("detail unschedulable=%v, want %v", detail.Unschedulable, cordoned)
		}
		if list := buildNodeViews([]corev1.Node{node}, nil); len(list) != 1 || list[0].Unschedulable != cordoned {
			t.Fatalf("list lost schedulability: %+v", list)
		}
	}
}

func TestNodeListPreservesReadySchedulingTaintsAndPressure(t *testing.T) {
	node := corev1.Node{
		Spec:   corev1.NodeSpec{Unschedulable: true, Taints: []corev1.Taint{{Key: "maintenance", Value: "task113", Effect: corev1.TaintEffectNoSchedule}}},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}, {Type: corev1.NodeDiskPressure, Status: corev1.ConditionTrue}}},
	}
	views := buildNodeViews([]corev1.Node{node}, nil)
	if len(views) != 1 || !views[0].Unschedulable || views[0].Status != "ready" || len(views[0].Taints) != 1 || views[0].Taints[0].Effect != "NoSchedule" || len(views[0].Conditions) != 2 {
		t.Fatalf("node list lost independent observed state: %+v", views)
	}
}
