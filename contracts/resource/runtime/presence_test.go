package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type presenceListFunc func(context.Context, metav1.ListOptions) (*metav1.PartialObjectMetadataList, error)

func (f presenceListFunc) List(ctx context.Context, options metav1.ListOptions) (*metav1.PartialObjectMetadataList, error) {
	return f(ctx, options)
}

func TestPresenceRequiresCompleteCurrentMetadataCollection(t *testing.T) {
	for _, scenario := range []string{"absent", "present", "deleting", "http404", "rbac", "missing-version", "continued", "remaining", "foreign-name", "foreign-namespace", "missing-uid", "missing-object-version", "multiple", "nil-list"} {
		t.Run(scenario, func(t *testing.T) {
			items := &metav1.PartialObjectMetadataList{TypeMeta: metav1.TypeMeta{APIVersion: "meta.k8s.io/v1", Kind: "PartialObjectMetadataList"}, ListMeta: metav1.ListMeta{ResourceVersion: "opaque-collection-v"}}
			item := metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Name: "app", Namespace: "team-a", UID: types.UID("opaque-uid"), ResourceVersion: "opaque-object-v"}}
			if scenario != "absent" {
				items.Items = []metav1.PartialObjectMetadata{item}
			}
			switch scenario {
			case "deleting":
				now := metav1.Now()
				items.Items[0].DeletionTimestamp = &now
			case "missing-version":
				items.ResourceVersion = ""
			case "continued":
				items.Continue = "next"
			case "remaining":
				n := int64(1)
				items.RemainingItemCount = &n
			case "foreign-name":
				items.Items[0].Name = "other"
			case "foreign-namespace":
				items.Items[0].Namespace = "other"
			case "missing-uid":
				items.Items[0].UID = ""
			case "missing-object-version":
				items.Items[0].ResourceVersion = ""
			case "multiple":
				items.Items = append(items.Items, item)
			case "nil-list":
				items = nil
			}
			calls := 0
			client := presenceListFunc(func(ctx context.Context, options metav1.ListOptions) (*metav1.PartialObjectMetadataList, error) {
				calls++
				deadline, ok := ctx.Deadline()
				if options.FieldSelector != "metadata.name=app" || options.Limit != 2 || options.ResourceVersion != "" || options.Continue != "" || options.Watch || !ok || time.Until(deadline) > 5*time.Second {
					t.Fatal("metadata observation expanded its target, consistency or budget")
				}
				if scenario == "http404" || scenario == "rbac" {
					return nil, errors.New(scenario)
				}
				return items, nil
			})
			result, err := ObserveResourcePresence(t.Context(), client, "ConfigMap", "team-a", "app")
			valid := scenario == "absent" || scenario == "present" || scenario == "deleting"
			if calls != 1 || (err == nil) != valid {
				t.Fatal("incomplete/error collection became absence or expanded queries", err)
			}
			if valid && (result.ValidateFor("ConfigMap", "team-a", "app") != nil || (result.State == "present") != (scenario != "absent") || (result.DeletionTimestamp != nil) != (scenario == "deleting")) {
				t.Fatal("metadata identity was lost")
			}
		})
	}
}

func TestPresenceRejectsUnsupportedTargetBeforeRead(t *testing.T) {
	client := presenceListFunc(func(context.Context, metav1.ListOptions) (*metav1.PartialObjectMetadataList, error) {
		t.Fatal("invalid target caused Kubernetes read")
		return nil, nil
	})
	for _, target := range [][3]string{{"ConfigMap", "", "app"}, {"Node", "team-a", "app"}, {"Unknown", "", "app"}, {"CustomResourceDefinition", "", "app"}, {"ConfigMap", "team-a", ""}} {
		if _, err := ObserveResourcePresence(t.Context(), client, target[0], target[1], target[2]); err == nil {
			t.Fatal("unsupported target accepted")
		}
	}
}
