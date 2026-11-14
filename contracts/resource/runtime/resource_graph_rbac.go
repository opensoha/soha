package runtime

import (
	"context"
	"errors"
	"fmt"
	"iter"

	cr "github.com/opensoha/soha/contracts/resource"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/validate/content"
	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	kruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
)

const (
	graphRBACAPI                            = rbacv1.GroupName + "/v1"
	ResourceGraphRelationBindsRole          = "binds-role"
	ResourceGraphRelationBindsSubject       = "binds-subject"
	ResourceGraphRelationAggregatesRole     = "aggregates-role"
	ResourceGraphRelationUsesServiceAccount = "uses-service-account"
	graphCrossNamespaceAccountReadLimit     = 16
)

func graphRBACRootKind(kind string) bool {
	switch kind {
	case "ServiceAccount", "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
		return true
	}
	return false
}

func graphRBACClusterKind(kind string) bool {
	return kind == "ClusterRole" || kind == "ClusterRoleBinding"
}

func graphCollectRBACKind(rootKind, kind string) bool {
	switch rootKind {
	case "ServiceAccount":
		return kind == "Pod" || kind == "RoleBinding" || kind == "ClusterRoleBinding"
	case "Role":
		return kind == "RoleBinding"
	case "RoleBinding":
		return kind == "Role" || kind == "ClusterRole" || kind == "ServiceAccount"
	case "ClusterRole":
		return kind == "ClusterRole" || kind == "RoleBinding" || kind == "ClusterRoleBinding"
	case "ClusterRoleBinding":
		return kind == "ClusterRole" || kind == "ServiceAccount"
	}
	return false
}

func graphRBACProjection(obj kruntime.Object, target *unstructured.Unstructured) error {
	var declaration interface{}
	switch source := obj.(type) {
	case *rbacv1.RoleBinding:
		declaration = &rbacv1.RoleBinding{RoleRef: source.RoleRef, Subjects: source.Subjects}
	case *rbacv1.ClusterRoleBinding:
		declaration = &rbacv1.ClusterRoleBinding{RoleRef: source.RoleRef, Subjects: source.Subjects}
	case *rbacv1.ClusterRole:
		declaration = &rbacv1.ClusterRole{AggregationRule: source.AggregationRule}
	case *rbacv1.Role, *corev1.ServiceAccount, *metav1.PartialObjectMetadata:
		return nil
	default:
		return fmt.Errorf("unexpected RBAC graph projection type %T", obj)
	}
	fields, err := kruntime.DefaultUnstructuredConverter.ToUnstructured(declaration)
	if err != nil {
		return err
	}
	for _, key := range []string{"roleRef", "subjects", "aggregationRule"} {
		if value, ok := fields[key]; ok {
			target.Object[key] = value
		}
	}
	return nil
}

func collectGraphCrossNamespaceAccounts(ctx context.Context, client kubernetes.Interface, root *unstructured.Unstructured, objects *[]*unstructured.Unstructured, warnings *[]string) error {
	if root.GetAPIVersion() != graphRBACAPI || root.GetKind() != "RoleBinding" {
		return nil
	}
	reads := 0
	for _, target := range graphRBACServiceAccountSubjects(root, func() bool { return ctx.Err() == nil }) {
		if target.namespace == root.GetNamespace() {
			continue
		}
		if reads == graphCrossNamespaceAccountReadLimit {
			*warnings = append(*warnings, "ServiceAccount cross-namespace references truncated at 16 targets")
			break
		}
		reads++
		object, err := client.CoreV1().ServiceAccounts(target.namespace).Get(ctx, target.name, metav1.GetOptions{})
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return err
			}
			*warnings = append(*warnings, "ServiceAccount cross-namespace reference "+graphSourceErrorClass(err))
			continue
		}
		projected, err := graphObjectProjection(object, "v1", "ServiceAccount")
		if err != nil || projected.GetName() != target.name || projected.GetNamespace() != target.namespace {
			*warnings = append(*warnings, "ServiceAccount cross-namespace reference identity unverified")
			continue
		}
		*objects = append(*objects, projected)
	}
	return ctx.Err()
}

// Foreign ServiceAccounts are targets of the observed root declaration only.
// The caller keeps them out of the regular graph source and owner traversal.
func indexGraphCrossNamespaceAccounts(ctx context.Context, root *unstructured.Unstructured, objects []*unstructured.Unstructured, index map[string]*unstructured.Unstructured) error {
	if root.GetAPIVersion() != graphRBACAPI || root.GetKind() != "RoleBinding" {
		return nil
	}
	targets := map[graphNamespacedName]bool{}
	for _, target := range graphRBACServiceAccountSubjects(root, func() bool { return ctx.Err() == nil }) {
		if target.namespace != root.GetNamespace() {
			targets[target] = true
		}
	}
	if len(targets) == 0 {
		return ctx.Err()
	}
	for _, object := range objects {
		if err := ctx.Err(); err != nil {
			return err
		}
		if object == nil || object.GetAPIVersion() != "v1" || object.GetKind() != "ServiceAccount" {
			continue
		}
		target := graphNamespacedName{namespace: object.GetNamespace(), name: object.GetName()}
		if targets[target] {
			index[graphIdentity("v1", "ServiceAccount", target.namespace, target.name)] = object
			delete(targets, target)
		}
	}
	return ctx.Err()
}

func (b *graphRules) rbacRelations(o *unstructured.Unstructured, id string) {
	if !graphBuiltinSpec(o) {
		return
	}
	switch o.GetKind() {
	case "Pod":
		b.podServiceAccountReference(o, id, graphPodSpec(o))
	case "RoleBinding", "ClusterRoleBinding":
		if (o.GetNamespace() == "") == graphRBACClusterKind(o.GetKind()) {
			b.rbacBindingRelations(o, id)
		}
	case "ClusterRole":
		if o.GetNamespace() == "" {
			b.rbacAggregationRelations(o, id)
		}
	}
}

func (b *graphRules) podServiceAccountReference(o *unstructured.Unstructured, id string, spec map[string]interface{}) {
	namespace, name := o.GetNamespace(), graphString(spec, "serviceAccountName")
	if namespace != "" && len(validation.ValidateServiceAccountName(name, false)) == 0 {
		b.rbacReferenceEdge(id, "v1", "ServiceAccount", namespace, name, ResourceGraphRelationUsesServiceAccount, "spec.serviceAccountName")
	}
}

func (b *graphRules) rbacBindingRelations(o *unstructured.Unstructured, id string) {
	rawRole, _, _ := graphNestedMap(o.Object, "roleRef")
	var role rbacv1.RoleRef
	if err := kruntime.DefaultUnstructuredConverter.FromUnstructured(rawRole, &role); err == nil &&
		role.APIGroup == rbacv1.GroupName && role.Name != "" && len(content.IsPathSegmentName(role.Name)) == 0 {
		switch role.Kind {
		case "ClusterRole":
			b.rbacReferenceEdge(id, graphRBACAPI, role.Kind, "", role.Name, ResourceGraphRelationBindsRole, "roleRef")
		case "Role":
			if o.GetKind() == "RoleBinding" {
				b.rbacReferenceEdge(id, graphRBACAPI, role.Kind, o.GetNamespace(), role.Name, ResourceGraphRelationBindsRole, "roleRef")
			}
		}
	}
	for i, target := range graphRBACServiceAccountSubjects(o, b.active) {
		b.rbacReferenceEdge(id, "v1", "ServiceAccount", target.namespace, target.name, ResourceGraphRelationBindsSubject, fmt.Sprintf("subjects[%d]", i))
	}
}

func graphRBACServiceAccountSubjects(binding *unstructured.Unstructured, active func() bool) iter.Seq2[int, graphNamespacedName] {
	return func(yield func(int, graphNamespacedName) bool) {
		seen := map[graphNamespacedName]bool{}
		for i, raw := range graphSlice(binding.Object, "subjects") {
			if !active() {
				return
			}
			data, ok := raw.(map[string]interface{})
			var subject rbacv1.Subject
			if !ok || kruntime.DefaultUnstructuredConverter.FromUnstructured(data, &subject) != nil ||
				subject.Kind != rbacv1.ServiceAccountKind || subject.APIGroup != "" ||
				len(validation.ValidateServiceAccountName(subject.Name, false)) != 0 {
				continue
			}
			if subject.Namespace == "" {
				subject.Namespace = binding.GetNamespace()
			}
			if len(validation.ValidateNamespaceName(subject.Namespace, false)) != 0 {
				continue
			}
			target := graphNamespacedName{namespace: subject.Namespace, name: subject.Name}
			if seen[target] {
				continue
			}
			seen[target] = true
			if !yield(i, target) {
				return
			}
		}
	}
}

func (b *graphRules) rbacReferenceEdge(source, api, kind, namespace, name, relation, fieldPath string) {
	state := "unresolved"
	if b.objects[graphIdentity(api, kind, namespace, name)] != nil {
		state = "resolved"
	}
	b.addEdge(cr.ResourceGraphEdge{
		SourceID: source, TargetID: b.target(api, kind, namespace, name, ""),
		Relation: relation, ResolutionState: state, FieldPath: fieldPath,
	})
}

func (b *graphRules) rbacAggregationRelations(o *unstructured.Unstructured, id string) {
	raw, _, _ := graphNestedSlice(o.Object, "aggregationRule", "clusterRoleSelectors")
	if len(raw) == 0 {
		return
	}
	selectors := make([]labels.Selector, 0, len(raw))
	for _, entry := range raw {
		if !b.active() {
			return
		}
		data, ok := entry.(map[string]interface{})
		var selector metav1.LabelSelector
		if !ok || data == nil || kruntime.DefaultUnstructuredConverter.FromUnstructuredWithValidation(data, &selector, true) != nil {
			return
		}
		compiled, err := metav1.LabelSelectorAsSelector(&selector)
		if err != nil {
			return
		}
		selectors = append(selectors, compiled)
	}
	for _, candidate := range b.clusterRoles {
		if !b.active() {
			return
		}
		if candidate.GetName() == o.GetName() {
			continue
		}
		actual := labels.Set(candidate.GetLabels())
		for i, selector := range selectors {
			if !b.active() {
				return
			}
			if selector.Matches(actual) {
				b.addEdge(cr.ResourceGraphEdge{
					SourceID: id, TargetID: b.node(b.ref(candidate), graphStatus(candidate)),
					Relation: ResourceGraphRelationAggregatesRole, ResolutionState: "resolved",
					FieldPath: fmt.Sprintf("aggregationRule.clusterRoleSelectors[%d]", i),
					Note:      "ClusterRole labels match the declared selector",
				})
				break
			}
		}
	}
}
