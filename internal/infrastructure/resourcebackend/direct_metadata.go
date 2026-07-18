package resourcebackend

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"

	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const partialMetadataListAcceptType = "application/json;as=PartialObjectMetadataList;g=meta.k8s.io;v=v1"

const tableListAcceptType = "application/json;as=Table;g=meta.k8s.io;v=v1"

const (
	maxMetadataResponseBytes = 16 << 20
	tableListPageSize        = 500
)

func listPartialMetadata(ctx context.Context, bundle *k8sinfra.Bundle, gvr schema.GroupVersionResource, namespaced bool, namespace string, options metav1.ListOptions) ([]metav1.PartialObjectMetadata, error) {
	client, err := metadataRESTClient(bundle, gvr)
	if err != nil {
		return nil, err
	}
	if options.Limit == 0 {
		options.Limit = tableListPageSize
	}
	items := []metav1.PartialObjectMetadata{}
	for {
		raw, err := readMetadataResponse(ctx, client, gvr, namespaced, namespace, options, partialMetadataListAcceptType, false)
		if err != nil {
			return nil, err
		}
		var page metav1.PartialObjectMetadataList
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		items = append(items, page.Items...)
		if page.Continue == "" {
			return items, nil
		}
		if page.Continue == options.Continue {
			return nil, fmt.Errorf("metadata listing for %s returned a repeated continue token", gvr.Resource)
		}
		options.Continue = page.Continue
	}
}

func listTable(ctx context.Context, bundle *k8sinfra.Bundle, gvr schema.GroupVersionResource, namespaced bool, namespace string) (metav1.Table, error) {
	client, err := metadataRESTClient(bundle, gvr)
	if err != nil {
		return metav1.Table{}, err
	}
	table := metav1.Table{}
	options := metav1.ListOptions{Limit: tableListPageSize}
	for {
		raw, err := readMetadataResponse(ctx, client, gvr, namespaced, namespace, options, tableListAcceptType, true)
		if err != nil {
			return metav1.Table{}, err
		}
		var page metav1.Table
		if err := json.Unmarshal(raw, &page); err != nil {
			return metav1.Table{}, err
		}
		if table.ColumnDefinitions == nil {
			table.ColumnDefinitions = page.ColumnDefinitions
		}
		table.Rows = append(table.Rows, page.Rows...)
		if page.Continue == "" {
			return table, nil
		}
		if page.Continue == options.Continue {
			return metav1.Table{}, fmt.Errorf("table listing for %s returned a repeated continue token", gvr.Resource)
		}
		options.Continue = page.Continue
	}
}

func metadataRESTClient(bundle *k8sinfra.Bundle, gvr schema.GroupVersionResource) (rest.Interface, error) {
	config := dynamic.ConfigFor(bundle.RESTConfig)
	config.GroupVersion = &schema.GroupVersion{Group: gvr.Group, Version: gvr.Version}
	config.APIPath = "/apis"
	if gvr.Group == "" {
		config.APIPath = "/api"
	}
	return rest.RESTClientFor(config)
}

func readMetadataResponse(ctx context.Context, client rest.Interface, gvr schema.GroupVersionResource, namespaced bool, namespace string, options metav1.ListOptions, accept string, includeMetadata bool) ([]byte, error) {
	request := client.Get().
		NamespaceIfScoped(namespace, namespaced && namespace != "").
		Resource(gvr.Resource).
		VersionedParams(&options, metav1.ParameterCodec).
		SetHeader("Accept", accept)
	if includeMetadata {
		request.Param("includeObject", string(metav1.IncludeMetadata))
	}
	stream, err := request.Stream(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = stream.Close() }()
	raw, err := io.ReadAll(io.LimitReader(stream, maxMetadataResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxMetadataResponseBytes {
		return nil, fmt.Errorf("metadata response exceeds %d byte limit", maxMetadataResponseBytes)
	}
	return raw, nil
}

func metadataListUnsupported(err error) bool {
	return apierrors.IsNotAcceptable(err) || apierrors.IsUnsupportedMediaType(err)
}

func tableRowAccessor(row metav1.TableRow) (metav1.Object, error) {
	if row.Object.Object != nil {
		return meta.Accessor(row.Object.Object)
	}
	if len(row.Object.Raw) == 0 {
		return nil, fmt.Errorf("table row has no metadata")
	}
	var metadata metav1.PartialObjectMetadata
	if err := json.Unmarshal(row.Object.Raw, &metadata); err != nil {
		return nil, fmt.Errorf("decode table row metadata: %w", err)
	}
	return &metadata, nil
}

func tableViews[T any](table metav1.Table, mapRow func(metav1.TableRow, metav1.Object) (T, error)) ([]T, error) {
	views := make([]T, 0, len(table.Rows))
	for _, row := range table.Rows {
		accessor, err := tableRowAccessor(row)
		if err != nil {
			return nil, fmt.Errorf("read table metadata: %w", err)
		}
		view, err := mapRow(row, accessor)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func tableColumnIndex(columns []metav1.TableColumnDefinition, name string) int {
	for index, column := range columns {
		if column.Name == name {
			return index
		}
	}
	return -1
}

func tableStringCell(cells []any, index int) (string, error) {
	if index < 0 || index >= len(cells) {
		return "", fmt.Errorf("table row is missing column %d", index)
	}
	value, ok := cells[index].(string)
	if !ok {
		return "", fmt.Errorf("table column %d is not a string", index)
	}
	return value, nil
}

func tableIntCell(cells []any, index int) (int, error) {
	if index < 0 || index >= len(cells) {
		return 0, fmt.Errorf("table row is missing column %d", index)
	}
	switch value := cells[index].(type) {
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
	return 0, fmt.Errorf("table column %d is not an integer", index)
}

func tableBoolCell(cells []any, index int) (bool, error) {
	if index < 0 || index >= len(cells) {
		return false, fmt.Errorf("table row is missing column %d", index)
	}
	switch value := cells[index].(type) {
	case bool:
		return value, nil
	case string:
		if value == "" || value == "<unset>" || value == "<none>" {
			return false, nil
		}
		parsed, err := strconv.ParseBool(value)
		if err == nil {
			return parsed, nil
		}
	}
	return false, fmt.Errorf("table column %d is not a boolean", index)
}
