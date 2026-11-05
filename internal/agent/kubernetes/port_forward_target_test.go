package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func TestResolveServicePortForwardTarget(t *testing.T) {
	for _, tc := range []struct {
		name      string
		port      int
		target    intstr.IntOrString
		protocol  corev1.Protocol
		podPort   corev1.Protocol
		named     bool
		want      int
		wantError bool
	}{
		{name: "numeric target", port: 80, target: intstr.FromInt(8080), want: 8080},
		{name: "named target", port: 80, target: intstr.FromString("http"), named: true, want: 8080},
		{name: "UDP Pod port cannot satisfy named TCP target", port: 80, target: intstr.FromString("http"), named: true, podPort: corev1.ProtocolUDP, wantError: true},
		{name: "missing named target", port: 80, target: intstr.FromString("missing"), wantError: true},
		{name: "undeclared service port", port: 81, target: intstr.FromInt(8080), wantError: true},
		{name: "UDP unsupported", port: 80, target: intstr.FromInt(8080), protocol: corev1.ProtocolUDP, wantError: true},
		{name: "omitted target defaults to service port", port: 80, want: 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team"}, Spec: corev1.ServiceSpec{Selector: map[string]string{"app": "api"}, Ports: []corev1.ServicePort{{Port: 80, TargetPort: tc.target, Protocol: tc.protocol}}}}
			pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-worker", Namespace: "team", Labels: map[string]string{"app": "api"}}, Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
			if tc.named {
				pod.Spec.Containers = []corev1.Container{{Name: "app", Ports: []corev1.ContainerPort{{Name: "http", ContainerPort: 8080, Protocol: tc.podPort}}}}
			}
			// A UDP service port may share the same number; TCP forwarding must select TCP.
			if !tc.wantError {
				service.Spec.Ports = append([]corev1.ServicePort{{Port: 80, Protocol: corev1.ProtocolUDP, TargetPort: intstr.FromInt(5353)}}, service.Spec.Ports...)
			}
			typed := fake.NewClientset(service, pod)
			if tc.named {
				older := pod.DeepCopy()
				older.Name = "aaa-old-replica"
				older.Spec.Containers[0].Ports[0].Protocol = corev1.ProtocolUDP
				older.Spec.Containers[0].Ports[0].ContainerPort = 5353
				if _, err := typed.CoreV1().Pods("team").Create(t.Context(), older, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			name, port, err := (&Client{typed: typed}).resolvePortForwardTarget(t.Context(), "team", "service", "api", tc.port)
			if tc.wantError {
				if err == nil {
					t.Fatal("expected target resolution to fail")
				}
				return
			}
			if err != nil || name != "api-worker" || port != tc.want {
				t.Fatalf("got (%s,%d,%v), want api-worker:%d", name, port, err, tc.want)
			}
		})
	}
}
