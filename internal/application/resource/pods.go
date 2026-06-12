package resource

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"k8s.io/client-go/tools/remotecommand"
)

func (s *Service) ListPods(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.PodView, error) {
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

func (s *Service) GetWorkloadOverview(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (domainresource.WorkloadOverviewView, error) {
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

func (s *Service) GetPodDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.PodDetailView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.PodDetailView{}, err
	}

	var (
		item   domainresource.PodDetailView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodDetailView{}, err
		}
		item, err = client.GetPodDetail(ctx, namespace, name)
		if err != nil {
			return domainresource.PodDetailView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItem, err := s.getDirectPod(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.PodDetailView{}, err
		}
		item = s.buildPodDetailView(ctx, clusterID, decision, *rawItem)
		source = "live"
	}
	item.AllowedActions = stringifyActions(decision.AllowedActions)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod detail via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) DeletePod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionDelete)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		return unsupportedAgentOperation("pod deletion is not supported for agent-connected clusters yet")
	default:
		if err := s.deleteDirectPod(ctx, clusterID, namespace, name); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionDelete), "failure", err.Error())
			return err
		}
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionDelete), "success", "deleted pod for rebuild")
	return nil
}

func (s *Service) GetPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return domainresource.PodLogsView{}, err
	}

	var (
		item   domainresource.PodLogsView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodLogsView{}, err
		}
		item, err = client.GetPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, previous)
		if err != nil {
			return domainresource.PodLogsView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectPodLogs(ctx, clusterID, namespace, name, container, tailLines, sinceSeconds, previous)
		if err != nil {
			return domainresource.PodLogsView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("read pod logs via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) StreamPodLogs(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionLogs)
	if err != nil {
		return err
	}

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
			return err
		}
		if err := client.StreamPodLogs(ctx, namespace, name, container, tailLines, sinceSeconds, stdout); err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
			return fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
	default:
		if err := s.streamDirectPodLogs(ctx, clusterID, namespace, name, container, tailLines, sinceSeconds, stdout); err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionLogs), "failure", err.Error())
			return err
		}
	}

	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionLogs), "success", fmt.Sprintf("streamed pod logs in namespace %s", displayNamespace(namespace)))
	return nil
}

func (s *Service) ExecPod(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return domainresource.PodExecView{}, fmt.Errorf("%w: command is required", apperrors.ErrInvalidArgument)
	}

	var (
		item   domainresource.PodExecView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.PodExecView{}, err
		}
		item, err = client.ExecPod(ctx, namespace, name, container, command, timeoutSeconds)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
			return domainresource.PodExecView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.execDirectPod(ctx, clusterID, namespace, name, container, command, timeoutSeconds)
		if err != nil {
			_ = s.recordAudit(ctx, principal, clusterID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
			return domainresource.PodExecView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("executed pod command via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) StreamPodTerminal(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue) error {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionExec)
	if err != nil {
		return err
	}
	shell = normalizeTerminalShell(shell)

	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, clientErr := s.agentClient(connection)
		if clientErr != nil {
			err = clientErr
		} else {
			err = client.StreamPodTerminal(ctx, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue)
			if err != nil {
				err = fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
			}
		}
	default:
		err = s.streamDirectPodTerminal(ctx, clusterID, namespace, name, container, shell, stdin, stdout, stderr, sizeQueue)
	}

	if err != nil && !errors.Is(err, context.Canceled) {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "failure", err.Error())
		return err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionExec), "success", fmt.Sprintf("opened interactive pod terminal via %s in namespace %s", shell, displayNamespace(namespace)))
	return nil
}

func (s *Service) GetPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ResourceYAMLView, error) {
	connection, _, err := s.authorize(ctx, principal, clusterID, namespace, "Pod", domainaccess.ActionView)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	var (
		item   domainresource.ResourceYAMLView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		item, err = client.GetPodYAML(ctx, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		item, err = s.getDirectPodYAML(ctx, clusterID, namespace, name)
		if err != nil {
			return domainresource.ResourceYAMLView{}, err
		}
		source = "live"
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Pod", name, string(domainaccess.ActionView), "success", fmt.Sprintf("viewed pod yaml via %s in namespace %s", source, displayNamespace(namespace)))
	return item, nil
}

func (s *Service) ApplyPodYAML(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	return s.applyResourceYAML(ctx, principal, clusterID, namespace, "Pod", name, content)
}
