package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

const (
	agentTableAcceptType = "application/json;as=Table;v=v1;g=meta.k8s.io"
	agentTablePageSize   = 500
)

func (c *Client) listConfigMapSummaries(ctx context.Context, namespace string) ([]domainresource.ConfigMapView, error) {
	if items, cached := cachedResourceItems[corev1.ConfigMap](ctx, c, "ConfigMap", namespace); cached {
		views := make([]domainresource.ConfigMapView, 0, len(items))
		for _, item := range items {
			entries, _ := strconv.Atoi(item.Annotations[cacheEntryCountAnnotation])
			views = append(views, domainresource.ConfigMapView{Name: item.Name, Namespace: item.Namespace, DataEntries: entries, AgeSeconds: secondsSince(item.CreationTimestamp.Time)})
		}
		return views, nil
	}
	table, err := c.listCoreTable(ctx, namespace, "configmaps", "")
	if err != nil {
		return nil, err
	}
	nameColumn, dataColumn := tableColumn(table, "Name"), tableColumn(table, "Data")
	if nameColumn < 0 || dataColumn < 0 {
		return nil, fmt.Errorf("configmap table is missing required columns")
	}
	views := make([]domainresource.ConfigMapView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		name, err := tableString(row, nameColumn)
		if err != nil {
			return nil, err
		}
		entries, err := tableInteger(row, dataColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.ConfigMapView{
			Name: name, Namespace: metadata.GetNamespace(), DataEntries: entries,
			AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listSecretSummaries(ctx context.Context, namespace string) ([]domainresource.SecretView, error) {
	if items, cached := cachedResourceItems[corev1.Secret](ctx, c, "Secret", namespace); cached {
		views := make([]domainresource.SecretView, 0, len(items))
		for _, item := range items {
			entries, _ := strconv.Atoi(item.Annotations[cacheEntryCountAnnotation])
			views = append(views, domainresource.SecretView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Type), DataEntries: entries, AgeSeconds: secondsSince(item.CreationTimestamp.Time)})
		}
		return views, nil
	}
	table, err := c.listCoreTable(ctx, namespace, "secrets", "")
	if err != nil {
		return nil, err
	}
	nameColumn, typeColumn, dataColumn := tableColumn(table, "Name"), tableColumn(table, "Type"), tableColumn(table, "Data")
	if nameColumn < 0 || typeColumn < 0 || dataColumn < 0 {
		return nil, fmt.Errorf("secret table is missing required columns")
	}
	views := make([]domainresource.SecretView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		name, err := tableString(row, nameColumn)
		if err != nil {
			return nil, err
		}
		secretType, err := tableString(row, typeColumn)
		if err != nil {
			return nil, err
		}
		entries, err := tableInteger(row, dataColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.SecretView{
			Name: name, Namespace: metadata.GetNamespace(), Type: secretType,
			DataEntries: entries, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listHelmReleaseSummaries(ctx context.Context, namespace string) ([]domainresource.HelmReleaseView, error) {
	table, err := c.listCoreTable(ctx, namespace, "secrets", "owner=helm")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HelmReleaseView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		views = append(views, mapHelmRelease(metadata.GetName(), metadata.GetNamespace(), metadata.GetLabels(), metadata.GetCreationTimestamp().Time, "secret"))
	}
	sort.SliceStable(views, func(i, j int) bool {
		if views[i].Namespace != views[j].Namespace {
			return views[i].Namespace < views[j].Namespace
		}
		if views[i].Name != views[j].Name {
			return views[i].Name < views[j].Name
		}
		return views[i].Revision > views[j].Revision
	})
	return dedupeHelmReleases(views), nil
}

func (c *Client) listCoreTable(ctx context.Context, namespace, resource, selector string) (metav1.Table, error) {
	return listTable(ctx, c.typed.CoreV1().RESTClient(), namespace, resource, selector)
}

func listTable(ctx context.Context, client rest.Interface, namespace, resource, selector string) (metav1.Table, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	result := metav1.Table{Rows: []metav1.TableRow{}}
	continueToken := ""
	for {
		request := client.Get().
			NamespaceIfScoped(namespace, strings.TrimSpace(namespace) != "").
			Resource(resource).
			Param("includeObject", string(metav1.IncludeMetadata)).
			Param("limit", strconv.Itoa(agentTablePageSize)).
			SetHeader("Accept", agentTableAcceptType)
		if selector != "" {
			request.Param("labelSelector", selector)
		}
		if continueToken != "" {
			request.Param("continue", continueToken)
		}
		var page metav1.Table
		if err := request.Do(queryCtx).Into(&page); err != nil {
			if apierrors.IsNotAcceptable(err) || apierrors.IsUnsupportedMediaType(err) {
				return metav1.Table{}, fmt.Errorf("cluster does not support metadata-only table listing for %s: %w", resource, err)
			}
			return metav1.Table{}, err
		}
		if len(result.ColumnDefinitions) == 0 {
			result.ColumnDefinitions = page.ColumnDefinitions
		}
		result.Rows = append(result.Rows, page.Rows...)
		if page.Continue == "" {
			return result, nil
		}
		if page.Continue == continueToken {
			return metav1.Table{}, fmt.Errorf("table listing for %s returned a repeated continue token", resource)
		}
		continueToken = page.Continue
	}
}

func (c *Client) listDeploymentSummaries(ctx context.Context, namespace string) ([]domainresource.DeploymentView, error) {
	if items, cached := cachedResourceItems[appsv1.Deployment](ctx, c, "Deployment", namespace); cached {
		views := make([]domainresource.DeploymentView, 0, len(items))
		for _, item := range items {
			desired := int32(1)
			if item.Spec.Replicas != nil {
				desired = *item.Spec.Replicas
			}
			views = append(views, domainresource.DeploymentView{Name: item.Name, Namespace: item.Namespace, UID: string(item.UID), Labels: item.Labels,
				DesiredReplicas: desired, ReadyReplicas: item.Status.ReadyReplicas, UpdatedReplicas: item.Status.UpdatedReplicas,
				Available: item.Status.AvailableReplicas, AgeSeconds: secondsSince(item.CreationTimestamp.Time)})
		}
		return views, nil
	}
	table, err := listTable(ctx, c.typed.AppsV1().RESTClient(), namespace, "deployments", "")
	if err != nil {
		return nil, err
	}
	readyColumn := tableColumn(table, "Ready")
	updatedColumn := tableColumn(table, "Up-to-date")
	availableColumn := tableColumn(table, "Available")
	if readyColumn < 0 || updatedColumn < 0 || availableColumn < 0 {
		return nil, fmt.Errorf("deployment table is missing required columns")
	}
	views := make([]domainresource.DeploymentView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		ready, desired, err := tableRatio(row, readyColumn)
		if err != nil {
			return nil, err
		}
		updated, err := tableInt32(row, updatedColumn)
		if err != nil {
			return nil, err
		}
		available, err := tableInt32(row, availableColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.DeploymentView{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), UID: string(metadata.GetUID()), Labels: metadata.GetLabels(),
			DesiredReplicas: desired, ReadyReplicas: ready, UpdatedReplicas: updated,
			Available: available, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listDaemonSetSummaries(ctx context.Context, namespace string) ([]domainresource.DaemonSetView, error) {
	if items, cached := cachedResourceItems[appsv1.DaemonSet](ctx, c, "DaemonSet", namespace); cached {
		views := make([]domainresource.DaemonSetView, 0, len(items))
		for _, item := range items {
			views = append(views, domainresource.DaemonSetView{Name: item.Name, Namespace: item.Namespace,
				DesiredNumber: item.Status.DesiredNumberScheduled, CurrentNumber: item.Status.CurrentNumberScheduled,
				ReadyNumber: item.Status.NumberReady, UpdatedNumber: item.Status.UpdatedNumberScheduled,
				AvailableNumber: item.Status.NumberAvailable, AgeSeconds: secondsSince(item.CreationTimestamp.Time)})
		}
		return views, nil
	}
	table, err := listTable(ctx, c.typed.AppsV1().RESTClient(), namespace, "daemonsets", "")
	if err != nil {
		return nil, err
	}
	columns := map[string]int{
		"desired": tableColumn(table, "Desired"), "current": tableColumn(table, "Current"),
		"ready":   tableColumn(table, "Ready"),
		"updated": tableColumn(table, "Up-to-date"), "available": tableColumn(table, "Available"),
	}
	for _, column := range columns {
		if column < 0 {
			return nil, fmt.Errorf("daemonset table is missing required columns")
		}
	}
	views := make([]domainresource.DaemonSetView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		values := make(map[string]int32, len(columns))
		for _, field := range []string{"desired", "current", "ready", "updated", "available"} {
			values[field], err = tableInt32(row, columns[field])
			if err != nil {
				return nil, err
			}
		}
		views = append(views, domainresource.DaemonSetView{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), DesiredNumber: values["desired"],
			CurrentNumber: values["current"], ReadyNumber: values["ready"],
			UpdatedNumber: values["updated"], AvailableNumber: values["available"],
			AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listStorageClassSummaries(ctx context.Context) ([]domainresource.StorageClassView, error) {
	table, err := listTable(ctx, c.typed.StorageV1().RESTClient(), "", "storageclasses", "")
	if err != nil {
		return nil, err
	}
	provisionerColumn := tableColumn(table, "Provisioner")
	reclaimColumn := tableColumn(table, "ReclaimPolicy")
	bindingColumn := tableColumn(table, "VolumeBindingMode")
	expansionColumn := tableColumn(table, "AllowVolumeExpansion")
	if provisionerColumn < 0 || reclaimColumn < 0 || bindingColumn < 0 || expansionColumn < 0 {
		return nil, fmt.Errorf("storageclass table is missing required columns")
	}
	views := make([]domainresource.StorageClassView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		provisioner, err := tableString(row, provisionerColumn)
		if err != nil {
			return nil, err
		}
		reclaimPolicy, err := tableString(row, reclaimColumn)
		if err != nil {
			return nil, err
		}
		bindingMode, err := tableString(row, bindingColumn)
		if err != nil {
			return nil, err
		}
		allowExpansion, err := tableBoolean(row, expansionColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.StorageClassView{
			Name: metadata.GetName(), Provisioner: provisioner, ReclaimPolicy: reclaimPolicy,
			VolumeBindingMode: bindingMode, AllowVolumeExpansion: allowExpansion,
			AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listWebhookSummaries(ctx context.Context, resource string) ([]domainresource.MutatingWebhookConfigurationView, error) {
	table, err := listTable(ctx, c.typed.AdmissionregistrationV1().RESTClient(), "", resource, "")
	if err != nil {
		return nil, err
	}
	webhooksColumn := tableColumn(table, "Webhooks")
	if webhooksColumn < 0 {
		return nil, fmt.Errorf("webhook table is missing required columns")
	}
	views := make([]domainresource.MutatingWebhookConfigurationView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		webhooks, err := tableInteger(row, webhooksColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.MutatingWebhookConfigurationView{
			Name: metadata.GetName(), Webhooks: webhooks, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listServiceAccountSummaries(ctx context.Context, namespace string) ([]domainresource.ServiceAccountView, error) {
	table, err := c.listCoreTable(ctx, namespace, "serviceaccounts", "")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ServiceAccountView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.ServiceAccountView{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listRoleSummaries(ctx context.Context, namespace string) ([]domainresource.RoleView, error) {
	table, err := listTable(ctx, c.typed.RbacV1().RESTClient(), namespace, "roles", "")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.RoleView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.RoleView{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listRoleBindingSummaries(ctx context.Context, namespace string) ([]domainresource.RoleBindingView, error) {
	table, err := listTable(ctx, c.typed.RbacV1().RESTClient(), namespace, "rolebindings", "")
	if err != nil {
		return nil, err
	}
	roleColumn := tableColumn(table, "Role")
	if roleColumn < 0 {
		return nil, fmt.Errorf("rolebinding table is missing required columns")
	}
	views := make([]domainresource.RoleBindingView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		role, err := tableString(row, roleColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.RoleBindingView{
			Name: metadata.GetName(), Namespace: metadata.GetNamespace(), RoleRef: role,
			AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listClusterRoleSummaries(ctx context.Context) ([]domainresource.ClusterRoleView, error) {
	table, err := listTable(ctx, c.typed.RbacV1().RESTClient(), "", "clusterroles", "")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ClusterRoleView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.ClusterRoleView{
			Name: metadata.GetName(), AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func (c *Client) listClusterRoleBindingSummaries(ctx context.Context) ([]domainresource.ClusterRoleBindingView, error) {
	table, err := listTable(ctx, c.typed.RbacV1().RESTClient(), "", "clusterrolebindings", "")
	if err != nil {
		return nil, err
	}
	roleColumn := tableColumn(table, "Role")
	if roleColumn < 0 {
		return nil, fmt.Errorf("clusterrolebinding table is missing required columns")
	}
	views := make([]domainresource.ClusterRoleBindingView, 0, len(table.Rows))
	for _, row := range table.Rows {
		metadata, err := tableRowMetadata(row)
		if err != nil {
			return nil, err
		}
		role, err := tableString(row, roleColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.ClusterRoleBindingView{
			Name: metadata.GetName(), RoleRef: role, AgeSeconds: secondsSince(metadata.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}

func tableColumn(table metav1.Table, name string) int {
	for index, column := range table.ColumnDefinitions {
		if column.Name == name {
			return index
		}
	}
	return -1
}

func tableRowMetadata(row metav1.TableRow) (metav1.Object, error) {
	if row.Object.Object != nil {
		return meta.Accessor(row.Object.Object)
	}
	if len(row.Object.Raw) == 0 {
		return nil, fmt.Errorf("table row has no metadata")
	}
	var object metav1.PartialObjectMetadata
	if err := json.Unmarshal(row.Object.Raw, &object); err != nil {
		return nil, fmt.Errorf("decode table row metadata: %w", err)
	}
	return &object, nil
}

func tableString(row metav1.TableRow, column int) (string, error) {
	if column >= len(row.Cells) {
		return "", fmt.Errorf("table row is missing column %d", column)
	}
	value, ok := row.Cells[column].(string)
	if !ok {
		return "", fmt.Errorf("table column %d is not a string", column)
	}
	return value, nil
}

func tableInteger(row metav1.TableRow, column int) (int, error) {
	if column >= len(row.Cells) {
		return 0, fmt.Errorf("table row is missing column %d", column)
	}
	switch value := row.Cells[column].(type) {
	case int:
		return value, nil
	case int64:
		return int(value), nil
	case float64:
		return int(value), nil
	case string:
		parsed, err := strconv.Atoi(value)
		if err == nil {
			return parsed, nil
		}
	}
	return 0, fmt.Errorf("table column %d is not an integer", column)
}

func tableRatio(row metav1.TableRow, column int) (int32, int32, error) {
	value, err := tableString(row, column)
	if err != nil {
		return 0, 0, err
	}
	left, right, ok := strings.Cut(value, "/")
	if !ok {
		return 0, 0, fmt.Errorf("table column %d is not a ratio", column)
	}
	readyValue, readyErr := strconv.ParseInt(left, 10, 32)
	desiredValue, desiredErr := strconv.ParseInt(right, 10, 32)
	if readyErr != nil || desiredErr != nil {
		return 0, 0, fmt.Errorf("table column %d is not a ratio", column)
	}
	// ParseInt with bitSize 32 guarantees these conversions cannot overflow.
	return int32(readyValue), int32(desiredValue), nil //nolint:gosec
}

func tableInt32(row metav1.TableRow, column int) (int32, error) {
	value, err := tableInteger(row, column)
	if err != nil {
		return 0, err
	}
	if value < -1<<31 || value > 1<<31-1 {
		return 0, fmt.Errorf("table column %d exceeds int32 range", column)
	}
	return int32(value), nil //nolint:gosec // The range check above makes the conversion safe.
}

func tableBoolean(row metav1.TableRow, column int) (bool, error) {
	if column >= len(row.Cells) {
		return false, fmt.Errorf("table row is missing column %d", column)
	}
	switch value := row.Cells[column].(type) {
	case bool:
		return value, nil
	case string:
		if value == "<unset>" {
			return false, nil
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed, nil
		}
	}
	return false, fmt.Errorf("table column %d is not a boolean", column)
}

type discoveredCRD struct {
	kind       string
	namespaced bool
	versions   []string
	preferred  string
}

func (c *Client) listCRDSummaries(ctx context.Context) ([]domainresource.CRDView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	gvr := schema.GroupVersionResource{Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions"}
	items, err := c.metadata.Resource(gvr).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	discoveryClient, err := c.discoveryForContext(queryCtx)
	if err != nil {
		return nil, err
	}
	groups, resources, discoveryErr := discoveryClient.ServerGroupsAndResourcesWithContext(queryCtx)
	if queryCtx.Err() != nil {
		return nil, queryCtx.Err()
	}
	if discoveryErr != nil && len(resources) == 0 {
		return nil, discoveryErr
	}
	discovered := indexDiscoveredCRDs(groups, resources)
	views := make([]domainresource.CRDView, 0, len(items.Items))
	for _, item := range items.Items {
		info, ok := discovered[item.Name]
		if !ok || len(info.versions) == 0 {
			continue
		}
		plural, group, ok := strings.Cut(item.Name, ".")
		if !ok {
			continue
		}
		scope := "Cluster"
		if info.namespaced {
			scope = "Namespaced"
		}
		views = append(views, domainresource.CRDView{
			UID: string(item.UID), DeletingAt: customResourceDeletingAt(item.DeletionTimestamp),
			Name: item.Name, Group: group, Scope: scope, Kind: info.kind, Plural: plural,
			Version: info.preferred, Versions: info.versions,
			CreatedAt:  item.CreationTimestamp.Time.UTC().Format(time.RFC3339),
			AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		})
	}
	return views, nil
}

func indexDiscoveredCRDs(groups []*metav1.APIGroup, resourceLists []*metav1.APIResourceList) map[string]discoveredCRD {
	preferred := make(map[string]string, len(groups))
	for _, group := range groups {
		if group != nil {
			preferred[group.Name] = group.PreferredVersion.Version
		}
	}
	result := make(map[string]discoveredCRD)
	for _, list := range resourceLists {
		if list == nil {
			continue
		}
		groupVersion, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil || groupVersion.Group == "" {
			continue
		}
		for _, resource := range list.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			name := resource.Name + "." + groupVersion.Group
			info := result[name]
			info.kind = resource.Kind
			info.namespaced = resource.Namespaced
			if !containsString(info.versions, groupVersion.Version) {
				info.versions = append(info.versions, groupVersion.Version)
			}
			info.preferred = preferred[groupVersion.Group]
			result[name] = info
		}
	}
	for name, info := range result {
		if info.preferred == "" || !containsString(info.versions, info.preferred) {
			info.preferred = info.versions[0]
		}
		result[name] = info
	}
	return result
}

func containsString(values []string, desired string) bool {
	for _, value := range values {
		if value == desired {
			return true
		}
	}
	return false
}
