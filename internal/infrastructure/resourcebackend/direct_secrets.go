package resourcebackend

import (
	"context"
	"fmt"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	secretListTimeout     = 8 * time.Second
	secretTableAcceptType = tableListAcceptType
)

var secretGVR = schema.GroupVersionResource{Version: "v1", Resource: "secrets"}

func (d *Direct) ListSecrets(ctx context.Context, clusterID, namespace string) ([]domainresource.SecretView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, secretListTimeout)
	defer cancel()
	views, err := listSecretTable(queryCtx, bundle, namespace)
	if err == nil {
		return views, nil
	}
	if !metadataListUnsupported(err) {
		return nil, err
	}
	return listSecretMetadata(queryCtx, bundle, namespace)
}

func listSecretTable(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]domainresource.SecretView, error) {
	table, err := listTable(ctx, bundle, secretGVR, true, namespace)
	if err != nil {
		return nil, err
	}
	return mapSecretTable(table)
}

func listSecretMetadata(ctx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]domainresource.SecretView, error) {
	items, err := listPartialMetadata(ctx, bundle, secretGVR, true, namespace, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.SecretView, 0, len(items))
	for _, item := range items {
		views = append(views, domainresource.SecretView{
			Name:       item.Name,
			Namespace:  item.Namespace,
			AgeSeconds: secondsSince(item.CreationTimestamp.Time),
		})
	}
	return views, nil
}

func mapSecretTable(table metav1.Table) ([]domainresource.SecretView, error) {
	nameColumn := tableColumnIndex(table.ColumnDefinitions, "Name")
	typeColumn := tableColumnIndex(table.ColumnDefinitions, "Type")
	dataColumn := tableColumnIndex(table.ColumnDefinitions, "Data")
	if nameColumn < 0 || typeColumn < 0 || dataColumn < 0 {
		return nil, fmt.Errorf("secret table is missing required columns")
	}
	views := make([]domainresource.SecretView, 0, len(table.Rows))
	for _, row := range table.Rows {
		accessor, err := tableRowAccessor(row)
		if err != nil {
			return nil, fmt.Errorf("read secret table metadata: %w", err)
		}
		name, err := tableStringCell(row.Cells, nameColumn)
		if err != nil {
			return nil, err
		}
		secretType, err := tableStringCell(row.Cells, typeColumn)
		if err != nil {
			return nil, err
		}
		dataEntries, err := tableIntCell(row.Cells, dataColumn)
		if err != nil {
			return nil, err
		}
		views = append(views, domainresource.SecretView{
			Name:        name,
			Namespace:   accessor.GetNamespace(),
			Type:        secretType,
			DataEntries: dataEntries,
			AgeSeconds:  secondsSince(accessor.GetCreationTimestamp().Time),
		})
	}
	return views, nil
}
