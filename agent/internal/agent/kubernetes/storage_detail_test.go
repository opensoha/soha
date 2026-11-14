package kubernetes

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestStorageDetailRelationships(t *testing.T) {
	pvc := mapPersistentVolumeClaimDetail(corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "team-a"},
	}, []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "team-a"},
		Spec:       corev1.PodSpec{Volumes: []corev1.Volume{{VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}},
	}})
	if len(pvc.Pods) != 1 || pvc.Pods[0].Name != "api-0" {
		t.Fatalf("PVC pods = %#v", pvc.Pods)
	}

	pv := mapPersistentVolumeDetail(corev1.PersistentVolume{Spec: corev1.PersistentVolumeSpec{
		ClaimRef: &corev1.ObjectReference{Namespace: "team-a", Name: "data"},
	}})
	if pv.ClaimNamespace != "team-a" || pv.ClaimName != "data" {
		t.Fatalf("PV claim = %#v", pv)
	}
}
