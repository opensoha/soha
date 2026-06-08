package resource

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	k8sinfra "github.com/opensoha/soha/internal/infrastructure/kubernetes"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const allNamespaceListParallelism = 8

func (s *Service) directKubeQueryContext(ctx context.Context, clusterID string, timeout time.Duration) (*k8sinfra.Bundle, context.Context, context.CancelFunc, error) {
	bundle, err := s.directKubeBundle(ctx, clusterID)
	if err != nil {
		return nil, nil, nil, err
	}
	queryCtx, cancel := directKubeTimeout(ctx, timeout)
	return bundle, queryCtx, cancel, nil
}

func (s *Service) directKubeBundle(ctx context.Context, clusterID string) (*k8sinfra.Bundle, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	return bundle, nil
}

func directKubeTimeout(ctx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	queryCtx, cancel := context.WithTimeout(ctx, timeout)
	return queryCtx, cancel
}

func listAcrossNamespaces[T any](ctx context.Context, s *Service, clusterID string, listFn func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	bundle, err := s.directKubeBundle(ctx, clusterID)
	if err != nil {
		return nil, err
	}

	return listAcrossNamespacesWithFallback(ctx, bundle, func(ctx context.Context) ([]string, error) {
		namespaces, _, err := s.listDirectNamespaces(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(namespaces))
		for _, namespace := range namespaces {
			name := strings.TrimSpace(namespace.Name)
			if name == "" {
				continue
			}
			names = append(names, name)
		}
		return names, nil
	}, listFn)
}

func listAcrossNamespacesWithFallback[T any](ctx context.Context, bundle *k8sinfra.Bundle, namespaceNames func(context.Context) ([]string, error), listFn func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	queryCtx, cancel := directKubeTimeout(ctx, 4*time.Second)
	items, err := listFn(queryCtx, bundle, metav1.NamespaceAll)
	cancel()
	if err == nil {
		return items, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	names, nameErr := namespaceNames(ctx)
	if nameErr != nil {
		return nil, err
	}
	return listAcrossNamespaceNames(ctx, bundle, names, listFn)
}

func listAcrossNamespaceNames[T any](ctx context.Context, bundle *k8sinfra.Bundle, names []string, listFn func(context.Context, *k8sinfra.Bundle, string) ([]T, error)) ([]T, error) {
	if len(names) == 0 {
		return []T{}, nil
	}

	type namespaceResult struct {
		index int
		items []T
		err   error
	}

	workerCount := allNamespaceListParallelism
	if len(names) < workerCount {
		workerCount = len(names)
	}
	jobs := make(chan int)
	results := make(chan namespaceResult, len(names))

	var wg sync.WaitGroup
	wg.Add(workerCount)
	for worker := 0; worker < workerCount; worker++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				queryCtx, cancel := directKubeTimeout(ctx, 4*time.Second)
				result, listErr := listFn(queryCtx, bundle, names[index])
				cancel()
				results <- namespaceResult{index: index, items: result, err: listErr}
			}
		}()
	}
	for index := range names {
		if err := ctx.Err(); err != nil {
			close(jobs)
			wg.Wait()
			close(results)
			return nil, err
		}
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	close(results)

	ordered := make([][]T, len(names))
	for result := range results {
		if result.err != nil {
			return nil, result.err
		}
		ordered[result.index] = result.items
	}

	items := make([]T, 0)
	for _, result := range ordered {
		items = append(items, result...)
	}
	return items, nil
}
