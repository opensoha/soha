package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (w *Workloads) ListPods(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
	s := w
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}

	items, source, err := s.listPodViews(ctx, clusterID, namespace, connection, decision, true)
	if err != nil {
		return nil, err
	}
	populateAllowedActionsPods(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed pods via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (w *Workloads) GetWorkloadOverview(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (domainresource.WorkloadOverviewView, error) {
	s := w
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionList)
	if err != nil {
		return domainresource.WorkloadOverviewView{}, err
	}

	items, source, err := s.listPodViews(ctx, clusterID, namespace, connection, decision, false)
	if err != nil {
		return domainresource.WorkloadOverviewView{}, err
	}
	view := buildWorkloadOverview(clusterID, namespace, source, items)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", "", string(domainaccess.ActionList), "success", fmt.Sprintf("summarized pod runtime via %s in namespace %s", source, displayNamespace(namespace)))
	return view, nil
}

func (w *Workloads) GetPodDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PodDetailView, error) {
	s := w
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.PodDetailView{}, err
	}

	route, err := s.routePods(connection, clusterID)
	if err != nil {
		return domainresource.PodDetailView{}, err
	}
	item, err := route.GetPodDetail(ctx, namespace, name)
	if err != nil {
		return domainresource.PodDetailView{}, route.RuntimeError(err)
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod detail via %s in namespace %s", route.Source(), displayNamespace(namespace)))
	return item, nil
}

func (w *Workloads) DeletePod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionDelete)
	if err != nil {
		return err
	}

	route, err := s.routePodDeletion(connection, clusterID)
	if err != nil {
		return err
	}
	auditFailure, err := route.DeletePod(ctx, namespace, name)
	if err != nil {
		if auditFailure {
			_ = s.recordAudit(ctx, principal, route.AuditClusterID(), namespace, "Pod", name, string(domainaccess.ActionDelete), "failure", err.Error())
		}
		return err
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionDelete), "success", "deleted pod for rebuild")
	return nil
}

func (w *Workloads) GetPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}

	route, err := s.routePods(connection, clusterID)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}
	item, err := route.GetPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, previous)
	if err != nil {
		return domainresource.PodLogsView{}, route.RuntimeError(err)
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("read pod logs via %s in namespace %s", route.Source(), displayNamespace(namespace)))
	return item, nil
}

func (w *Workloads) StreamPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return err
	}

	route, err := s.routePods(connection, clusterID)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
		return err
	}
	if err := route.StreamPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, stdout); err != nil {
		_ = s.recordAudit(ctx, principal, route.AuditClusterID(), namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
		return route.RuntimeError(err)
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("streamed pod logs in namespace %s", displayNamespace(namespace)))
	return nil
}

func (w *Workloads) ExecPod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return domainresource.PodExecView{}, fmt.Errorf("%w: command is required", apperrors.ErrInvalidArgument)
	}

	route, err := s.routePods(connection, clusterID)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	item, err := route.ExecPod(ctx, namespace, name, container, command, timeoutSeconds)
	if err != nil {
		_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
		return domainresource.PodExecView{}, route.RuntimeError(err)
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("executed pod command via %s in namespace %s", route.Source(), displayNamespace(namespace)))
	return item, nil
}

func (w *Workloads) StreamPodTerminal(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue domainresource.TerminalSizeQueue) error {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return err
	}
	shell = normalizeTerminalShell(shell)

	route, routeErr := s.routePods(connection, clusterID)
	if routeErr != nil {
		err = routeErr
	} else if err = route.StreamPodTerminal(ctx, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue); err != nil {
		err = route.RuntimeError(err)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
		return err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("opened interactive pod terminal via %s in namespace %s", shell, displayNamespace(namespace)))
	return nil
}

func (w *Workloads) GetPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	s := w
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	route, err := s.routePods(connection, clusterID)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	item, err := route.GetPodYAML(ctx, namespace, name)
	if err != nil {
		return domainresource.ResourceYAMLView{}, route.RuntimeError(err)
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod yaml via %s in namespace %s", route.Source(), displayNamespace(namespace)))
	return item, nil
}

func (w *Workloads) ApplyPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	s := w
	return s.yaml.applyResourceYAML(ctx, principal, clusterID, namespace, "Pod", name, content)
}
