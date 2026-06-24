package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"k8s.io/client-go/tools/remotecommand"
)

type Registry struct {
	defaultTimeout time.Duration
}

func NewRegistry(defaultTimeout time.Duration) *Registry {
	if defaultTimeout <= 0 {
		defaultTimeout = 10 * time.Second
	}
	return &Registry{defaultTimeout: defaultTimeout}
}

type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

type RuntimeExecutionTask struct {
	TaskID                   string    `json:"taskId"`
	ApplicationID            string    `json:"applicationId"`
	ApplicationEnvironmentID string    `json:"applicationEnvironmentId,omitempty"`
	TaskKind                 string    `json:"taskKind"`
	ProviderKind             string    `json:"providerKind"`
	Status                   string    `json:"status"`
	CurrentCommand           string    `json:"currentCommand,omitempty"`
	CommandIndex             int       `json:"commandIndex,omitempty"`
	CommandCount             int       `json:"commandCount,omitempty"`
	WorkspacePath            string    `json:"workspacePath,omitempty"`
	StartedAt                time.Time `json:"startedAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
	StopSource               string    `json:"stopSource,omitempty"`
	StopReason               string    `json:"stopReason,omitempty"`
}

type scaleDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type restartDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type restartStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type scaleStatefulSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Replicas  int32  `json:"replicas"`
}

type restartDaemonSetRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

type updateDeploymentImageRequest struct {
	Namespace     string `json:"namespace"`
	Name          string `json:"name"`
	ContainerName string `json:"containerName,omitempty"`
	Image         string `json:"image"`
}

type execPodRequest struct {
	Command        string `json:"command"`
	Container      string `json:"container,omitempty"`
	TimeoutSeconds int64  `json:"timeoutSeconds,omitempty"`
}

type rollbackDeploymentRequest struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Revision  string `json:"revision"`
}

type resourceYAMLRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Content   string `json:"content"`
}

type deleteResourceRequest struct {
	Namespace string `json:"namespace"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

type customResourceListRequest struct {
	Definition domainresource.CRDResourceDefinition `json:"definition"`
	Namespace  string                               `json:"namespace"`
}

type customResourceYAMLRequest struct {
	Definition domainresource.CRDResourceDefinition `json:"definition"`
	Namespace  string                               `json:"namespace"`
	Name       string                               `json:"name,omitempty"`
	Content    string                               `json:"content,omitempty"`
}

type helmReleaseValuesRequest struct {
	Content string `json:"content"`
}

type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type cancelRuntimeTaskRequest struct {
	Reason string `json:"reason"`
}

func (r *Registry) ClientFor(connection domaincluster.Connection) (*Client, error) {
	endpoint, _ := connection.Metadata["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("agent endpoint is missing for cluster %s", connection.Summary.ID)
	}
	token, _ := connection.Metadata["token"].(string)
	return &Client{
		baseURL: strings.TrimRight(endpoint, "/"),
		token:   token,
		httpClient: &http.Client{
			Timeout: r.defaultTimeout,
		},
	}, nil
}

func (c *Client) GetSummary(ctx context.Context) (domaincluster.Summary, error) {
	var payload struct {
		Data domaincluster.Summary `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/summary", nil, &payload); err != nil {
		return domaincluster.Summary{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListNamespaces(ctx context.Context) ([]domainresource.NamespaceView, error) {
	var payload struct {
		Items []domainresource.NamespaceView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/namespaces", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListNodes(ctx context.Context) ([]domainresource.NodeView, error) {
	var payload struct {
		Items []domainresource.NodeView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/infrastructure/nodes", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetNodeDetail(ctx context.Context, name string) (domainresource.NodeDetailView, error) {
	var payload struct {
		Data domainresource.NodeDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/infrastructure/nodes/%s/detail", url.PathEscape(name))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.NodeDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListPods(ctx context.Context, namespace string) ([]domainresource.PodView, error) {
	var payload struct {
		Items []domainresource.PodView `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods?namespace=%s", url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetPodDetail(ctx context.Context, namespace, name string) (domainresource.PodDetailView, error) {
	var payload struct {
		Data domainresource.PodDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.PodDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, previous bool) (domainresource.PodLogsView, error) {
	var payload struct {
		Data domainresource.PodLogsView `json:"data"`
	}
	values := url.Values{}
	values.Set("namespace", namespace)
	if container != "" {
		values.Set("container", container)
	}
	if tailLines > 0 {
		values.Set("tailLines", fmt.Sprintf("%d", tailLines))
	}
	if sinceSeconds > 0 {
		values.Set("sinceSeconds", fmt.Sprintf("%d", sinceSeconds))
	}
	if previous {
		values.Set("previous", "true")
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/logs?%s", url.PathEscape(name), values.Encode())
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.PodLogsView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetPodYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) StreamPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer) error {
	values := url.Values{}
	values.Set("namespace", namespace)
	if container != "" {
		values.Set("container", container)
	}
	if tailLines > 0 {
		values.Set("tailLines", fmt.Sprintf("%d", tailLines))
	}
	if sinceSeconds > 0 {
		values.Set("sinceSeconds", fmt.Sprintf("%d", sinceSeconds))
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/logs/stream?%s", url.PathEscape(name), values.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("build agent stream request: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute agent stream request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("agent stream request failed with status %d", resp.StatusCode)
	}
	if _, err := io.Copy(stdout, resp.Body); err != nil {
		return fmt.Errorf("copy agent stream response: %w", err)
	}
	return nil
}

func (c *Client) ExecPod(ctx context.Context, namespace, name, container, command string, timeoutSeconds int64) (domainresource.PodExecView, error) {
	var payload struct {
		Data domainresource.PodExecView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/exec?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	err := c.request(ctx, http.MethodPost, path, execPodRequest{
		Command:        command,
		Container:      container,
		TimeoutSeconds: timeoutSeconds,
	}, &payload)
	if err != nil {
		return domainresource.PodExecView{}, err
	}
	return payload.Data, nil
}

func (c *Client) StreamPodTerminal(ctx context.Context, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue) error {
	values := url.Values{}
	values.Set("namespace", namespace)
	if container != "" {
		values.Set("container", container)
	}
	if shell != "" {
		values.Set("shell", shell)
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/pods/%s/terminal?%s", url.PathEscape(name), values.Encode())
	endpoint, err := c.websocketEndpoint(path)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if c.token != "" {
		headers.Set("Authorization", "Bearer "+c.token)
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("agent terminal websocket failed with status %d", resp.StatusCode)
		}
		return fmt.Errorf("connect agent terminal websocket: %w", err)
	}
	defer conn.Close()

	var writeMu sync.Mutex
	errCh := make(chan error, 1)
	go c.readAgentTerminalMessages(ctx, conn, stdout, stderr, errCh)
	go c.copyAgentTerminalInput(ctx, conn, &writeMu, stdin)
	if sizeQueue != nil {
		go c.forwardAgentTerminalResizes(ctx, conn, &writeMu, sizeQueue)
	}

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		_ = writeAgentTerminalMessage(conn, &writeMu, terminalMessage{Type: "close"})
		return ctx.Err()
	}
}

func (c *Client) readAgentTerminalMessages(ctx context.Context, conn *websocket.Conn, stdout, stderr io.Writer, errCh chan<- error) {
	for {
		var message terminalMessage
		if err := conn.ReadJSON(&message); err != nil {
			if ctx.Err() != nil {
				errCh <- ctx.Err()
				return
			}
			errCh <- err
			return
		}
		switch message.Type {
		case "stdout":
			if _, err := io.WriteString(stdout, message.Data); err != nil {
				errCh <- err
				return
			}
		case "stderr":
			if _, err := io.WriteString(stderr, message.Data); err != nil {
				errCh <- err
				return
			}
		case "error":
			errCh <- fmt.Errorf("agent terminal error: %s", strings.TrimSpace(message.Message))
			return
		case "exit":
			errCh <- nil
			return
		}
	}
}

func (c *Client) copyAgentTerminalInput(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, stdin io.Reader) {
	buffer := make([]byte, 4096)
	for {
		n, err := stdin.Read(buffer)
		if n > 0 {
			if writeAgentTerminalMessage(conn, writeMu, terminalMessage{Type: "input", Data: string(buffer[:n])}) != nil {
				return
			}
		}
		if err != nil {
			_ = writeAgentTerminalMessage(conn, writeMu, terminalMessage{Type: "close"})
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func (c *Client) forwardAgentTerminalResizes(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, sizeQueue remotecommand.TerminalSizeQueue) {
	for {
		size := sizeQueue.Next()
		if size == nil || ctx.Err() != nil {
			return
		}
		if writeAgentTerminalMessage(conn, writeMu, terminalMessage{
			Type: "resize",
			Cols: int(size.Width),
			Rows: int(size.Height),
		}) != nil {
			return
		}
	}
}

func writeAgentTerminalMessage(conn *websocket.Conn, writeMu *sync.Mutex, message terminalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteJSON(message)
}

func (c *Client) ListDeployments(ctx context.Context, namespace string) ([]domainresource.DeploymentView, error) {
	var payload struct {
		Items []domainresource.DeploymentView `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/deployments?namespace=%s", url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetDeploymentDetail(ctx context.Context, namespace, name string) (domainresource.DeploymentDetailView, error) {
	var payload struct {
		Data domainresource.DeploymentDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/deployments/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.DeploymentDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetDeploymentYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/deployments/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetDeploymentRolloutStatus(ctx context.Context, namespace, name string) (domainresource.DeploymentRolloutStatusView, error) {
	var payload struct {
		Data domainresource.DeploymentRolloutStatusView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/deployments/%s/rollout-status?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.DeploymentRolloutStatusView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListDeploymentRolloutHistory(ctx context.Context, namespace, name string) ([]domainresource.RolloutHistoryView, error) {
	var payload struct {
		Items []domainresource.RolloutHistoryView `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/deployments/%s/rollouts?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) RollbackDeployment(ctx context.Context, namespace, name, revision string) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/deployments/rollback", rollbackDeploymentRequest{
		Namespace: namespace,
		Name:      name,
		Revision:  revision,
	}, nil)
}

func (c *Client) ListStatefulSets(ctx context.Context, namespace string) ([]domainresource.StatefulSetView, error) {
	var payload struct {
		Items []domainresource.StatefulSetView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/workloads/statefulsets", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetStatefulSetDetail(ctx context.Context, namespace, name string) (domainresource.StatefulSetDetailView, error) {
	var payload struct {
		Data domainresource.StatefulSetDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/statefulsets/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.StatefulSetDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetStatefulSetYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/statefulsets/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListDaemonSets(ctx context.Context, namespace string) ([]domainresource.DaemonSetView, error) {
	var payload struct {
		Items []domainresource.DaemonSetView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/workloads/daemonsets", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetDaemonSetDetail(ctx context.Context, namespace, name string) (domainresource.DaemonSetDetailView, error) {
	var payload struct {
		Data domainresource.DaemonSetDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/daemonsets/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.DaemonSetDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetDaemonSetYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/daemonsets/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListJobs(ctx context.Context, namespace string) ([]domainresource.JobView, error) {
	var payload struct {
		Items []domainresource.JobView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/workloads/jobs", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetJobDetail(ctx context.Context, namespace, name string) (domainresource.JobDetailView, error) {
	var payload struct {
		Data domainresource.JobDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/jobs/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.JobDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetJobYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/jobs/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListCronJobs(ctx context.Context, namespace string) ([]domainresource.CronJobView, error) {
	var payload struct {
		Items []domainresource.CronJobView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/workloads/cronjobs", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListReplicaSets(ctx context.Context, namespace string) ([]domainresource.ReplicaSetView, error) {
	var payload struct {
		Items []domainresource.ReplicaSetView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/workloads/replicasets", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListConfigMaps(ctx context.Context, namespace string) ([]domainresource.ConfigMapView, error) {
	var payload struct {
		Items []domainresource.ConfigMapView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/configuration/configmaps", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListSecrets(ctx context.Context, namespace string) ([]domainresource.SecretView, error) {
	var payload struct {
		Items []domainresource.SecretView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/configuration/secrets", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListServiceAccounts(ctx context.Context, namespace string) ([]domainresource.ServiceAccountView, error) {
	var payload struct {
		Items []domainresource.ServiceAccountView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/access-control/serviceaccounts", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetServiceAccountDetail(ctx context.Context, namespace, name string) (domainresource.ServiceAccountDetailView, error) {
	var payload struct {
		Data domainresource.ServiceAccountDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/access-control/serviceaccounts/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ServiceAccountDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListRoles(ctx context.Context, namespace string) ([]domainresource.RoleView, error) {
	var payload struct {
		Items []domainresource.RoleView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/access-control/roles", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetRoleDetail(ctx context.Context, namespace, name string) (domainresource.RoleDetailView, error) {
	var payload struct {
		Data domainresource.RoleDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/access-control/roles/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.RoleDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListRoleBindings(ctx context.Context, namespace string) ([]domainresource.RoleBindingView, error) {
	var payload struct {
		Items []domainresource.RoleBindingView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/access-control/rolebindings", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetRoleBindingDetail(ctx context.Context, namespace, name string) (domainresource.RoleBindingDetailView, error) {
	var payload struct {
		Data domainresource.RoleBindingDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/access-control/rolebindings/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.RoleBindingDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListHorizontalPodAutoscalers(ctx context.Context, namespace string) ([]domainresource.HorizontalPodAutoscalerView, error) {
	var payload struct {
		Items []domainresource.HorizontalPodAutoscalerView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/configuration/hpas", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListPodDisruptionBudgets(ctx context.Context, namespace string) ([]domainresource.PodDisruptionBudgetView, error) {
	var payload struct {
		Items []domainresource.PodDisruptionBudgetView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/configuration/poddisruptionbudgets", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetCronJobDetail(ctx context.Context, namespace, name string) (domainresource.CronJobDetailView, error) {
	var payload struct {
		Data domainresource.CronJobDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/cronjobs/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.CronJobDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetCronJobYAML(ctx context.Context, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/workloads/cronjobs/%s/yaml?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListCRDs(ctx context.Context) ([]domainresource.CRDView, error) {
	var payload struct {
		Items []domainresource.CRDView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/extensions/crds", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListHelmReleases(ctx context.Context, namespace string) ([]domainresource.HelmReleaseView, error) {
	var payload struct {
		Items []domainresource.HelmReleaseView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/helm/releases", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetHelmReleaseDetail(ctx context.Context, namespace, name string) (domainresource.HelmReleaseDetailView, error) {
	var payload struct {
		Data domainresource.HelmReleaseDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/helm/releases/%s/detail?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.HelmReleaseDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListHelmReleaseHistory(ctx context.Context, namespace, name string) ([]domainresource.HelmReleaseHistoryView, error) {
	var payload struct {
		Items []domainresource.HelmReleaseHistoryView `json:"items"`
	}
	path := fmt.Sprintf("/api/v1/platform/helm/releases/%s/history?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetHelmReleaseValues(ctx context.Context, namespace, name, revision string) (domainresource.HelmValuesView, error) {
	var payload struct {
		Data domainresource.HelmValuesView `json:"data"`
	}
	values := url.Values{}
	values.Set("namespace", namespace)
	if strings.TrimSpace(revision) != "" {
		values.Set("revision", revision)
	}
	path := fmt.Sprintf("/api/v1/platform/helm/releases/%s/values?%s", url.PathEscape(name), values.Encode())
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.HelmValuesView{}, err
	}
	return payload.Data, nil
}

func (c *Client) InstallHelmChart(ctx context.Context, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	var payload struct {
		Data domainresource.HelmChartInstallResult `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/helm/charts/install", input, &payload); err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	return payload.Data, nil
}

func (c *Client) UpdateHelmReleaseValues(ctx context.Context, namespace, name, content string) (domainresource.HelmValuesView, error) {
	var payload struct {
		Data domainresource.HelmValuesView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/helm/releases/%s/values?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	if err := c.request(ctx, http.MethodPut, path, helmReleaseValuesRequest{Content: content}, &payload); err != nil {
		return domainresource.HelmValuesView{}, err
	}
	return payload.Data, nil
}

func (c *Client) DeleteHelmRelease(ctx context.Context, namespace, name string) error {
	path := fmt.Sprintf("/api/v1/platform/helm/releases/%s?namespace=%s", url.PathEscape(name), url.QueryEscape(namespace))
	return c.request(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) ListServices(ctx context.Context, namespace string) ([]domainresource.ServiceView, error) {
	var payload struct {
		Items []domainresource.ServiceView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/services", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListIngresses(ctx context.Context, namespace string) ([]domainresource.IngressView, error) {
	var payload struct {
		Items []domainresource.IngressView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/ingresses", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListEndpointSlices(ctx context.Context, namespace string) ([]domainresource.EndpointSliceView, error) {
	var payload struct {
		Items []domainresource.EndpointSliceView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/endpointslices", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListNetworkPolicies(ctx context.Context, namespace string) ([]domainresource.NetworkPolicyView, error) {
	var payload struct {
		Items []domainresource.NetworkPolicyView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/networkpolicies", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListGatewayClasses(ctx context.Context) ([]domainresource.GatewayClassView, error) {
	var payload struct {
		Items []domainresource.GatewayClassView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/network/gatewayclasses", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListGateways(ctx context.Context, namespace string) ([]domainresource.GatewayView, error) {
	var payload struct {
		Items []domainresource.GatewayView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/gateways", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListHTTPRoutes(ctx context.Context, namespace string) ([]domainresource.HTTPRouteView, error) {
	var payload struct {
		Items []domainresource.HTTPRouteView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/httproutes", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListBackendTLSPolicies(ctx context.Context, namespace string) ([]domainresource.BackendTLSPolicyView, error) {
	var payload struct {
		Items []domainresource.BackendTLSPolicyView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/backendtlspolicies", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListGRPCRoutes(ctx context.Context, namespace string) ([]domainresource.GRPCRouteView, error) {
	var payload struct {
		Items []domainresource.GRPCRouteView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/grpcroutes", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListReferenceGrants(ctx context.Context, namespace string) ([]domainresource.ReferenceGrantView, error) {
	var payload struct {
		Items []domainresource.ReferenceGrantView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/network/referencegrants", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListPersistentVolumeClaims(ctx context.Context, namespace string) ([]domainresource.PersistentVolumeClaimView, error) {
	var payload struct {
		Items []domainresource.PersistentVolumeClaimView `json:"items"`
	}
	path := withNamespace("/api/v1/platform/storage/persistentvolumeclaims", namespace)
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListPersistentVolumes(ctx context.Context) ([]domainresource.PersistentVolumeView, error) {
	var payload struct {
		Items []domainresource.PersistentVolumeView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/storage/persistentvolumes", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListStorageClasses(ctx context.Context) ([]domainresource.StorageClassView, error) {
	var payload struct {
		Items []domainresource.StorageClassView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/storage/storageclasses", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListIngressClasses(ctx context.Context) ([]domainresource.IngressClassView, error) {
	var payload struct {
		Items []domainresource.IngressClassView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/network/ingressclasses", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListPriorityClasses(ctx context.Context) ([]domainresource.PriorityClassView, error) {
	var payload struct {
		Items []domainresource.PriorityClassView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/configuration/priorityclasses", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListRuntimeClasses(ctx context.Context) ([]domainresource.RuntimeClassView, error) {
	var payload struct {
		Items []domainresource.RuntimeClassView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/configuration/runtimeclasses", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListClusterRoles(ctx context.Context) ([]domainresource.ClusterRoleView, error) {
	var payload struct {
		Items []domainresource.ClusterRoleView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/access-control/clusterroles", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetClusterRoleDetail(ctx context.Context, name string) (domainresource.ClusterRoleDetailView, error) {
	var payload struct {
		Data domainresource.ClusterRoleDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/access-control/clusterroles/%s/detail", url.PathEscape(name))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ClusterRoleDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListClusterRoleBindings(ctx context.Context) ([]domainresource.ClusterRoleBindingView, error) {
	var payload struct {
		Items []domainresource.ClusterRoleBindingView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/access-control/clusterrolebindings", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetClusterRoleBindingDetail(ctx context.Context, name string) (domainresource.ClusterRoleBindingDetailView, error) {
	var payload struct {
		Data domainresource.ClusterRoleBindingDetailView `json:"data"`
	}
	path := fmt.Sprintf("/api/v1/platform/access-control/clusterrolebindings/%s/detail", url.PathEscape(name))
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ClusterRoleBindingDetailView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ListMutatingWebhookConfigurations(ctx context.Context) ([]domainresource.MutatingWebhookConfigurationView, error) {
	var payload struct {
		Items []domainresource.MutatingWebhookConfigurationView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/configuration/mutatingwebhookconfigurations", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListValidatingWebhookConfigurations(ctx context.Context) ([]domainresource.ValidatingWebhookConfigurationView, error) {
	var payload struct {
		Items []domainresource.ValidatingWebhookConfigurationView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/configuration/validatingwebhookconfigurations", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListResourceQuotas(ctx context.Context, namespace string) ([]domainresource.ResourceQuotaView, error) {
	var payload struct {
		Items []domainresource.ResourceQuotaView `json:"items"`
	}
	values := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		values.Set("namespace", namespace)
	}
	path := "/api/v1/platform/configuration/resourcequotas"
	if encoded := values.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListLimitRanges(ctx context.Context, namespace string) ([]domainresource.LimitRangeView, error) {
	var payload struct {
		Items []domainresource.LimitRangeView `json:"items"`
	}
	values := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		values.Set("namespace", namespace)
	}
	path := "/api/v1/platform/configuration/limitranges"
	if encoded := values.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListLeases(ctx context.Context, namespace string) ([]domainresource.LeaseView, error) {
	var payload struct {
		Items []domainresource.LeaseView `json:"items"`
	}
	values := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		values.Set("namespace", namespace)
	}
	path := "/api/v1/platform/configuration/leases"
	if encoded := values.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListReplicationControllers(ctx context.Context, namespace string) ([]domainresource.ReplicationControllerView, error) {
	var payload struct {
		Items []domainresource.ReplicationControllerView `json:"items"`
	}
	values := url.Values{}
	if strings.TrimSpace(namespace) != "" {
		values.Set("namespace", namespace)
	}
	path := "/api/v1/platform/workloads/replicationcontrollers"
	if encoded := values.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) ListClusterEvents(ctx context.Context, namespace string, limit int) ([]domainresource.ClusterEventView, error) {
	var payload struct {
		Items []domainresource.ClusterEventView `json:"items"`
	}
	values := url.Values{}
	if namespace != "" {
		values.Set("namespace", namespace)
	}
	if limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", limit))
	}
	path := "/api/v1/platform/events"
	if encoded := values.Encode(); encoded != "" {
		path = path + "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) GetResourceYAML(ctx context.Context, namespace, kind, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	values := url.Values{}
	values.Set("kind", kind)
	values.Set("name", name)
	if strings.TrimSpace(namespace) != "" {
		values.Set("namespace", namespace)
	}
	path := "/api/v1/platform/resources/yaml"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	if err := c.request(ctx, http.MethodGet, path, nil, &payload); err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ApplyResourceYAML(ctx context.Context, namespace, kind, name, content string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, "/api/v1/platform/resources/yaml", resourceYAMLRequest{
		Namespace: namespace,
		Kind:      kind,
		Name:      name,
		Content:   content,
	}, &payload)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) DeleteResource(ctx context.Context, namespace, kind, name string) error {
	return c.request(ctx, http.MethodDelete, "/api/v1/platform/resources", deleteResourceRequest{
		Namespace: namespace,
		Kind:      kind,
		Name:      name,
	}, nil)
}

func (c *Client) ListCustomResources(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace string) ([]domainresource.CustomResourceView, error) {
	var payload struct {
		Items []domainresource.CustomResourceView `json:"items"`
	}
	err := c.request(ctx, http.MethodPost, "/api/v1/platform/extensions/custom-resources/list", customResourceListRequest{
		Definition: definition,
		Namespace:  namespace,
	}, &payload)
	if err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) CreateCustomResourceYAML(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace, content string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, "/api/v1/platform/extensions/custom-resources", customResourceYAMLRequest{
		Definition: definition,
		Namespace:  namespace,
		Content:    content,
	}, &payload)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) GetCustomResourceYAML(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace, name string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, "/api/v1/platform/extensions/custom-resources/yaml", customResourceYAMLRequest{
		Definition: definition,
		Namespace:  namespace,
		Name:       name,
	}, &payload)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) ApplyCustomResourceYAML(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace, name, content string) (domainresource.ResourceYAMLView, error) {
	var payload struct {
		Data domainresource.ResourceYAMLView `json:"data"`
	}
	err := c.request(ctx, http.MethodPut, "/api/v1/platform/extensions/custom-resources/yaml", customResourceYAMLRequest{
		Definition: definition,
		Namespace:  namespace,
		Name:       name,
		Content:    content,
	}, &payload)
	if err != nil {
		return domainresource.ResourceYAMLView{}, err
	}
	return payload.Data, nil
}

func (c *Client) DeleteCustomResource(ctx context.Context, definition domainresource.CRDResourceDefinition, namespace, name string) error {
	return c.request(ctx, http.MethodDelete, "/api/v1/platform/extensions/custom-resources", customResourceYAMLRequest{
		Definition: definition,
		Namespace:  namespace,
		Name:       name,
	}, nil)
}

func (c *Client) ListPortForwards(ctx context.Context) ([]domainresource.PortForwardSessionView, error) {
	var payload struct {
		Items []domainresource.PortForwardSessionView `json:"items"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/platform/network/port-forwards", nil, &payload); err != nil {
		return nil, err
	}
	return payload.Items, nil
}

func (c *Client) RegisterPortForward(ctx context.Context, input domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error) {
	var payload struct {
		Data domainresource.PortForwardSessionView `json:"data"`
	}
	if err := c.request(ctx, http.MethodPost, "/api/v1/platform/network/port-forwards", input, &payload); err != nil {
		return domainresource.PortForwardSessionView{}, err
	}
	return payload.Data, nil
}

func (c *Client) StopPortForward(ctx context.Context, sessionID string) error {
	path := fmt.Sprintf("/api/v1/platform/network/port-forwards/%s", url.PathEscape(strings.TrimSpace(sessionID)))
	return c.request(ctx, http.MethodDelete, path, nil, nil)
}

func (c *Client) StreamPortForward(ctx context.Context, sessionID string, local net.Conn) error {
	path := fmt.Sprintf("/api/v1/platform/network/port-forwards/%s/tunnel", url.PathEscape(strings.TrimSpace(sessionID)))
	endpoint, err := c.websocketEndpoint(path)
	if err != nil {
		return err
	}
	headers := http.Header{}
	if c.token != "" {
		headers.Set("Authorization", "Bearer "+c.token)
	}
	conn, resp, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		if resp != nil {
			return fmt.Errorf("agent port-forward tunnel failed with status %d", resp.StatusCode)
		}
		return fmt.Errorf("connect agent port-forward tunnel: %w", err)
	}
	defer conn.Close()
	return bridgeAgentPortForwardTunnel(ctx, conn, local)
}

func bridgeAgentPortForwardTunnel(ctx context.Context, ws *websocket.Conn, local net.Conn) error {
	errCh := make(chan error, 2)
	go func() {
		errCh <- copyAgentPortForwardWebSocketToTCP(ctx, ws, local)
	}()
	go func() {
		errCh <- copyAgentPortForwardTCPToWebSocket(ctx, ws, local)
	}()

	select {
	case err := <-errCh:
		_ = local.Close()
		_ = ws.Close()
		if agentPortForwardTunnelClosed(err) {
			return nil
		}
		return err
	case <-ctx.Done():
		_ = local.Close()
		_ = ws.Close()
		return ctx.Err()
	}
}

func copyAgentPortForwardWebSocketToTCP(ctx context.Context, ws *websocket.Conn, local net.Conn) error {
	for {
		messageType, payload, err := ws.ReadMessage()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if messageType != websocket.BinaryMessage && messageType != websocket.TextMessage {
			continue
		}
		if len(payload) == 0 {
			continue
		}
		if _, err := local.Write(payload); err != nil {
			return err
		}
	}
}

func copyAgentPortForwardTCPToWebSocket(ctx context.Context, ws *websocket.Conn, local net.Conn) error {
	buffer := make([]byte, 32*1024)
	for {
		n, err := local.Read(buffer)
		if n > 0 {
			if writeErr := ws.WriteMessage(websocket.BinaryMessage, buffer[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
	}
}

func agentPortForwardTunnelClosed(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	return websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) ||
		errors.Is(err, websocket.ErrCloseSent)
}

func (c *Client) RestartDeployment(ctx context.Context, namespace, name string) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/deployments/restart", restartDeploymentRequest{Namespace: namespace, Name: name}, nil)
}

func (c *Client) ScaleDeployment(ctx context.Context, namespace, name string, replicas int32) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/deployments/scale", scaleDeploymentRequest{Namespace: namespace, Name: name, Replicas: replicas}, nil)
}

func (c *Client) RestartStatefulSet(ctx context.Context, namespace, name string) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/statefulsets/restart", restartStatefulSetRequest{Namespace: namespace, Name: name}, nil)
}

func (c *Client) ScaleStatefulSet(ctx context.Context, namespace, name string, replicas int32) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/statefulsets/scale", scaleStatefulSetRequest{Namespace: namespace, Name: name, Replicas: replicas}, nil)
}

func (c *Client) RestartDaemonSet(ctx context.Context, namespace, name string) error {
	return c.request(ctx, http.MethodPost, "/api/v1/platform/actions/daemonsets/restart", restartDaemonSetRequest{Namespace: namespace, Name: name}, nil)
}

func (c *Client) UpdateDeploymentImage(ctx context.Context, namespace, name, containerName, image string) (string, string, error) {
	var payload struct {
		Data struct {
			ContainerName string `json:"containerName"`
			PreviousImage string `json:"previousImage"`
		} `json:"data"`
	}
	err := c.request(ctx, http.MethodPost, "/api/v1/platform/actions/deployments/image", updateDeploymentImageRequest{
		Namespace:     namespace,
		Name:          name,
		ContainerName: containerName,
		Image:         image,
	}, &payload)
	if err != nil {
		return "", "", err
	}
	return payload.Data.ContainerName, payload.Data.PreviousImage, nil
}

func (c *Client) CancelRuntimeExecutionTask(ctx context.Context, taskID, reason string) error {
	return c.request(ctx, http.MethodPost, fmt.Sprintf("/api/v1/runtime/execution-tasks/%s/cancel", url.PathEscape(strings.TrimSpace(taskID))), cancelRuntimeTaskRequest{
		Reason: strings.TrimSpace(reason),
	}, nil)
}

func (c *Client) request(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal agent request: %w", err)
		}
		payload = encoded
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("build agent request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("execute agent request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("agent request failed with status %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode agent response: %w", err)
	}
	return nil
}

func (c *Client) websocketEndpoint(path string) (string, error) {
	endpoint, err := url.Parse(c.baseURL + path)
	if err != nil {
		return "", fmt.Errorf("build agent websocket endpoint: %w", err)
	}
	switch endpoint.Scheme {
	case "http":
		endpoint.Scheme = "ws"
	case "https":
		endpoint.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported agent websocket scheme %q", endpoint.Scheme)
	}
	return endpoint.String(), nil
}

func withNamespace(path, namespace string) string {
	if strings.TrimSpace(namespace) == "" {
		return path
	}
	return fmt.Sprintf("%s?namespace=%s", path, url.QueryEscape(namespace))
}
