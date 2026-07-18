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

func TestLightweightRBACTableMappings(t *testing.T) {
	t.Parallel()
	table := metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{{Name: "Name"}, {Name: "Role"}},
		Rows:              []metav1.TableRow{{Cells: []any{"binding-a", "ClusterRole/view"}, Object: tableTestMetadata("binding-a", "team-a")}},
	}
	bindings, err := mapRoleBindingTable(table)
	if err != nil || len(bindings) != 1 || bindings[0].Name != "binding-a" || bindings[0].Namespace != "team-a" || bindings[0].RoleRef != "ClusterRole/view" {
		t.Fatalf("mapRoleBindingTable() = %#v, %v", bindings, err)
	}
	roles, err := mapRoleTable(table)
	if err != nil || len(roles) != 1 || roles[0].Name != "binding-a" || roles[0].Namespace != "team-a" {
		t.Fatalf("mapRoleTable() = %#v, %v", roles, err)
	}
}

func TestReferencesServiceAccountMatchesExactSubject(t *testing.T) {
	t.Parallel()
	subjects := []rbacv1.Subject{{Kind: "ServiceAccount", Namespace: "team-a", Name: "builder"}}
	if !referencesServiceAccount(subjects, "team-a", "builder", "") {
		t.Fatal("referencesServiceAccount() = false, want true")
	}
	if referencesServiceAccount(subjects, "team-b", "builder", "") || referencesServiceAccount(subjects, "team-a", "other", "") {
		t.Fatal("referencesServiceAccount() matched a different service account")
	}
	implicitNamespace := []rbacv1.Subject{{Kind: "ServiceAccount", Name: "builder"}}
	if !referencesServiceAccount(implicitNamespace, "team-a", "builder", "team-a") {
		t.Fatal("referencesServiceAccount() did not use the RoleBinding namespace")
	}
	if referencesServiceAccount(implicitNamespace, "team-a", "builder", "") {
		t.Fatal("referencesServiceAccount() defaulted a ClusterRoleBinding subject namespace")
	}
}
