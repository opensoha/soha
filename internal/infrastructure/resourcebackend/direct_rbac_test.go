package resourcebackend

import (
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMapRoleDetailIncludesRuleSummaries(t *testing.T) {
	t.Parallel()

	view := mapRoleDetail(rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "viewer",
			Namespace:         "team-a",
			CreationTimestamp: metav1.NewTime(time.Unix(1_713_225_600, 0)),
		},
		Rules: []rbacv1.PolicyRule{{
			Verbs:     []string{"get", "list"},
			Resources: []string{"pods"},
		}},
	})

	if view.Rules != 1 {
		t.Fatalf("Rules = %d, want 1", view.Rules)
	}
	if len(view.RuleSummaries) != 1 {
		t.Fatalf("len(RuleSummaries) = %d, want 1", len(view.RuleSummaries))
	}
	if view.RuleSummaries[0] != "get, list -> pods" {
		t.Fatalf("RuleSummaries[0] = %q, want \"get, list -> pods\"", view.RuleSummaries[0])
	}
}

func TestMapServiceAccountDetailCollectsSecretNames(t *testing.T) {
	t.Parallel()

	view := mapServiceAccountDetail(corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "builder",
			Namespace:         "team-a",
			CreationTimestamp: metav1.NewTime(time.Unix(1_713_225_600, 0)),
		},
		Secrets: []corev1.ObjectReference{
			{Name: "token-a"},
			{Name: "dockercfg"},
		},
		ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-creds"}},
	})

	if len(view.Secrets) != 2 {
		t.Fatalf("len(Secrets) = %d, want 2", len(view.Secrets))
	}
	if len(view.ImagePullSecrets) != 1 || view.ImagePullSecrets[0] != "registry-creds" {
		t.Fatalf("ImagePullSecrets = %#v, want [registry-creds]", view.ImagePullSecrets)
	}
}
