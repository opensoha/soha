package resourcebackend

import (
	"context"
	"sync"
	"time"

	domainresource "github.com/opensoha/soha/internal/domain/resource"
	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const namespaceListParallelism = 8

func listAcrossNamespaces[T any](ctx context.Context, direct *Direct, clusterID string, timeout time.Duration, list func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	bundle, err := direct.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	return listAcrossNamespacesWithFallback(ctx, bundle, timeout, func(ctx context.Context) ([]domainresource.NamespaceView, error) {
		namespaces, _, err := direct.ListNamespaces(ctx, clusterID)
		return namespaces, err
	}, list)
}

func listAcrossNamespacesWithFallback[T any](ctx context.Context, bundle *k8sinfra.Bundle, timeout time.Duration, namespaceNames func(context.Context) ([]domainresource.NamespaceView, error), list func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	items, allErr := list(queryCtx, bundle, metav1.NamespaceAll)
	cancel()
	if allErr == nil {
		return items, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	namespaces, err := namespaceNames(ctx)
	if err != nil {
		return nil, allErr
	}
	return listNamespaceNames(ctx, bundle, namespaces, timeout, list)
}

func listNamespaceNames[T any](ctx context.Context, bundle *k8sinfra.Bundle, namespaces []domainresource.NamespaceView, timeout time.Duration, list func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	if len(namespaces) == 0 {
		return []T{}, nil
	}
	type result struct {
		index int
		items []T
		err   error
	}
	workers := namespaceListParallelism
	if len(namespaces) < workers {
		workers = len(namespaces)
	}
	jobs := make(chan int)
	results := make(chan result, len(namespaces))
	var wg sync.WaitGroup
	wg.Add(workers)
	for range workers {
		go func() {
			defer wg.Done()
			for index := range jobs {
				queryCtx, cancel := context.WithTimeout(ctx, timeout)
				items, err := list(queryCtx, bundle, namespaces[index].Name)
				cancel()
				results <- result{index: index, items: items, err: err}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for index := range namespaces {
			select {
			case jobs <- index:
			case <-ctx.Done():
				return
			}
		}
	}()
	wg.Wait()
	close(results)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ordered := make([][]T, len(namespaces))
	for value := range results {
		if value.err != nil {
			return nil, value.err
		}
		ordered[value.index] = value.items
	}
	var out []T
	for _, items := range ordered {
		out = append(out, items...)
	}
	return out, nil
}
