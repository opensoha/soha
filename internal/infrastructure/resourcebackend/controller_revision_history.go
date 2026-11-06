package resourcebackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
	"sort"
	"strconv"
	"time"
)

type revisionOwner struct {
	kind, name, namespace string
	uid                   types.UID
	templatePatch         []byte
}

func (o revisionOwner) controls(namespace string, owners []metav1.OwnerReference) bool {
	if o.uid == "" || namespace != o.namespace {
		return false
	}
	for _, owner := range owners {
		if owner.Controller != nil && *owner.Controller && owner.APIVersion == "apps/v1" && owner.Kind == o.kind && owner.Name == o.name && owner.UID == o.uid {
			return true
		}
	}
	return false
}

func listControllerHistory(ctx context.Context, typed kubernetes.Interface, owner revisionOwner) ([]domainresource.RolloutHistoryView, error) {
	revisions, err := typed.AppsV1().ControllerRevisions(owner.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	pods, err := typed.CoreV1().Pods(owner.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	items := make([]domainresource.RolloutHistoryView, 0)
	for _, revision := range revisions.Items {
		if !owner.controls(revision.Namespace, revision.OwnerReferences) {
			continue
		}
		item, err := controllerRevisionView(revision, owner)
		if err != nil {
			return nil, err
		}
		hash := revision.Name
		if owner.kind == "DaemonSet" {
			hash = item.PodRevisionHash
		}
		countRevisionPods(&item, pods.Items, owner, hash)
		items = append(items, item)
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, _ := strconv.ParseInt(items[i].Revision, 10, 64)
		b, _ := strconv.ParseInt(items[j].Revision, 10, 64)
		return a > b
	})
	return items, nil
}

func controllerRevisionView(revision appsv1.ControllerRevision, owner revisionOwner) (domainresource.RolloutHistoryView, error) {
	var patch struct {
		Spec struct {
			Template corev1.PodTemplateSpec `json:"template"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(revision.Data.Raw, &patch); err != nil {
		return domainresource.RolloutHistoryView{}, fmt.Errorf("decode controller revision: %w", err)
	}
	item := domainresource.RolloutHistoryView{Name: revision.Name, Namespace: revision.Namespace, Revision: strconv.FormatInt(revision.Revision, 10), CreatedAt: revision.CreationTimestamp.Format(time.RFC3339)}
	for _, container := range patch.Spec.Template.Spec.InitContainers {
		item.Images = append(item.Images, container.Name+"="+container.Image)
	}
	for _, container := range patch.Spec.Template.Spec.Containers {
		item.Images = append(item.Images, container.Name+"="+container.Image)
	}
	if owner.kind == "DaemonSet" {
		item.PodRevisionHash = revision.Labels[appsv1.DefaultDaemonSetUniqueLabelKey]
		matches := bytes.Equal(owner.templatePatch, revision.Data.Raw)
		item.MatchesCurrentTemplate = &matches
	}
	return item, nil
}

func countRevisionPods(item *domainresource.RolloutHistoryView, pods []corev1.Pod, owner revisionOwner, hash string) {
	if hash == "" {
		return
	}
	for _, pod := range pods {
		if !owner.controls(pod.Namespace, pod.OwnerReferences) || pod.Labels[appsv1.ControllerRevisionHashLabelKey] != hash {
			continue
		}
		item.Replicas++
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				item.ReadyReplicas++
				break
			}
		}
	}
}

func controllerRevisionYAML(ctx context.Context, typed kubernetes.Interface, owner revisionOwner, revisionName string) (domainresource.ResourceYAMLView, error) {
	revision, err := typed.AppsV1().ControllerRevisions(owner.namespace).Get(ctx, revisionName, metav1.GetOptions{})
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	if !owner.controls(revision.Namespace, revision.OwnerReferences) {
		return domainresource.ResourceYAMLView{}, apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "controllerrevisions"}, revisionName)
	}
	copy := revision.DeepCopy()
	copy.ManagedFields = nil
	copy.TypeMeta = metav1.TypeMeta{APIVersion: "apps/v1", Kind: "ControllerRevision"}
	content, err := yaml.Marshal(copy)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return domainresource.ResourceYAMLView{Kind: "ControllerRevision", Name: revisionName, Namespace: owner.namespace, Content: string(content)}, nil
}

// Match the native DaemonSet controller's spec.template strategic merge patch.
func daemonSetTemplatePatch(template corev1.PodTemplateSpec) ([]byte, error) {
	encoded, err := json.Marshal(template)
	if err != nil {
		return nil, err
	}
	var value map[string]interface{}
	if err := json.Unmarshal(encoded, &value); err != nil {
		return nil, err
	}
	value["$patch"] = "replace"
	return json.Marshal(map[string]interface{}{"spec": map[string]interface{}{"template": value}})
}

func daemonSetControlsPod(owners []metav1.OwnerReference, uid types.UID) bool {
	if uid == "" {
		return false
	}
	for _, owner := range owners {
		if owner.UID == uid && owner.Kind == "DaemonSet" && owner.APIVersion == "apps/v1" && owner.Controller != nil && *owner.Controller {
			return true
		}
	}
	return false
}
