package resource

import (
	"context"
	"fmt"
	"io"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type podReadRoute interface {
	ListPods(context.Context, string) ([]domainresource.PodView, string, error)
	ListPodsBySelector(context.Context, string, map[string]string) ([]domainresource.PodView, error)
	GetPodDetail(context.Context, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	GetPodYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
}

type podInteractiveRoute interface {
	StreamPodLogs(context.Context, string, string, string, int64, int64, io.Writer) error
	ExecPod(context.Context, string, string, string, string, int64) (domainresource.PodExecView, error)
	StreamPodTerminal(context.Context, string, string, string, string, io.Reader, io.Writer, io.Writer, domainresource.TerminalSizeQueue) error
}

type podMutationRoute interface {
	DeletePod(context.Context, string, string) (bool, error)
}

type podRoute interface {
	podReadRoute
	podInteractiveRoute
	podMutationRoute
	Source() string
	AuditClusterID() string
	SupportsUsageMetrics() bool
	RuntimeError(error) error
}

type agentPodRoute struct {
	client    PodAgent
	clusterID string
}

func (r agentPodRoute) ListPods(ctx context.Context, namespace string) ([]domainresource.PodView, string, error) {
	items, err := r.client.ListPods(ctx, namespace)
	return items, r.Source(), err
}

func (r agentPodRoute) ListPodsBySelector(ctx context.Context, namespace string, selector map[string]string) ([]domainresource.PodView, error) {
	items, err := r.client.ListPods(ctx, namespace)
	if err != nil {
		return nil, err
	}
	return filterPodsBySelector(items, selector), nil
}

func (r agentPodRoute) GetPodDetail(ctx context.Context, namespace, name string) (domainresource.PodDetailView, error) {
	return r.client.GetPodDetail(ctx, namespace, name)
}

func (agentPodRoute) DeletePod(context.Context, string, string) (bool, error) {
	return false, unsupportedAgentOperation("pod deletion is not supported for agent-connected clusters yet")
}

func (r agentPodRoute) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	return r.client.GetPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, previous)
}

func (r agentPodRoute) StreamPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	return r.client.StreamPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, stdout)
}

func (r agentPodRoute) ExecPod(ctx context.Context, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	return r.client.ExecPod(ctx, namespace, name, container, command, timeoutSeconds)
}

func (r agentPodRoute) StreamPodTerminal(ctx context.Context, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue domainresource.TerminalSizeQueue) error {
	return r.client.StreamPodTerminal(ctx, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue)
}

func (r agentPodRoute) GetPodYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return r.client.GetPodYAML(ctx, namespace, name)
}

func (agentPodRoute) Source() string {
	return "agent"
}

func (r agentPodRoute) AuditClusterID() string {
	return r.clusterID
}

func (agentPodRoute) SupportsUsageMetrics() bool {
	return false
}

func (agentPodRoute) RuntimeError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
}

type directPodRoute struct {
	backend   DirectPods
	clusterID string
}

func (r directPodRoute) ListPods(ctx context.Context, namespace string) ([]domainresource.PodView, string, error) {
	return r.backend.ListPods(ctx, r.clusterID, namespace)
}

func (r directPodRoute) ListPodsBySelector(ctx context.Context, namespace string, selector map[string]string) ([]domainresource.PodView, error) {
	if lister, ok := r.backend.(interface {
		ListPodsBySelector(context.Context, string, string, map[string]string) ([]domainresource.PodView, error)
	}); ok {
		return lister.ListPodsBySelector(ctx, r.clusterID, namespace, selector)
	}
	items, _, err := r.backend.ListPods(ctx, r.clusterID, namespace)
	if err != nil {
		return nil, err
	}
	return filterPodsBySelector(items, selector), nil
}

func (r directPodRoute) GetPodDetail(ctx context.Context, namespace, name string) (domainresource.PodDetailView, error) {
	return r.backend.GetPodDetail(ctx, r.clusterID, namespace, name)
}

func (r directPodRoute) DeletePod(ctx context.Context, namespace, name string) (bool, error) {
	return true, r.backend.DeletePod(ctx, r.clusterID, namespace, name)
}

func (r directPodRoute) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	return r.backend.GetPodLogs(ctx, r.clusterID, namespace, name, container, tailLines, sinceSeconds, previous)
}

func (r directPodRoute) StreamPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	return r.backend.StreamPodLogs(ctx, r.clusterID, namespace, name, container, tailLines, sinceSeconds, stdout)
}

func (r directPodRoute) ExecPod(ctx context.Context, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	return r.backend.ExecPod(ctx, r.clusterID, namespace, name, container, command, timeoutSeconds)
}

func (r directPodRoute) StreamPodTerminal(ctx context.Context, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue domainresource.TerminalSizeQueue) error {
	return r.backend.StreamPodTerminal(ctx, r.clusterID, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue)
}

func (r directPodRoute) GetPodYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	return r.backend.GetPodYAML(ctx, r.clusterID, namespace, name)
}

func (directPodRoute) Source() string {
	return "live"
}

func (r directPodRoute) AuditClusterID() string {
	return r.clusterID
}

func (directPodRoute) SupportsUsageMetrics() bool {
	return true
}

func (directPodRoute) RuntimeError(err error) error {
	return err
}

func (w *Workloads) routePods(connection domaincluster.Connection, clusterID string) (podRoute, error) {
	return w.routePodsFor(connection, clusterID, true)
}

func (w *Workloads) routePodDeletion(connection domaincluster.Connection, clusterID string) (podRoute, error) {
	return w.routePodsFor(connection, clusterID, false)
}

func (w *Workloads) routePodsFor(connection domaincluster.Connection, clusterID string, requireAgentClient bool) (podRoute, error) {
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		if !requireAgentClient {
			return agentPodRoute{clusterID: connection.Summary.ID}, nil
		}
		client, err := w.workloadAgentClient(connection)
		if err != nil {
			return nil, err
		}
		return agentPodRoute{client: client, clusterID: connection.Summary.ID}, nil
	default:
		if w.directPods == nil {
			return nil, fmt.Errorf("%w: direct pod backend is not configured", apperrors.ErrClusterUnready)
		}
		return directPodRoute{backend: w.directPods, clusterID: clusterID}, nil
	}
}
