package workflow

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type rolloutDAGNodeHandler struct {
	resources dagRolloutReader
}

type dagRolloutReader interface {
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
}

func (h rolloutDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	target := input.firstSelectedTarget()
	summary, err := waitForDAGRollout(
		ctx,
		h.resources,
		input.principal,
		targetClusterID(target, input.workflowInput),
		targetNamespace(target, input.workflowInput),
		targetWorkloadName(target, input.workflowInput),
		firstPositiveInt(toInt(input.node.Config["timeoutSeconds"], 0), input.node.TimeoutSeconds),
	)
	if err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(summary)
}

func (input dagNodeExecutionInput) firstSelectedTarget() *domaincatalog.ReleaseTarget {
	if len(input.selectedTargets) == 0 {
		return nil
	}
	return &input.selectedTargets[0]
}

func waitForDAGRollout(ctx context.Context, resources dagRolloutReader, principal domainidentity.Principal, clusterID, namespace, deploymentName string, timeoutSeconds int) (string, error) {
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(defaultDAGNodeTimeout / time.Second)
	}
	deadline := time.Now().Add(time.Duration(timeoutSeconds) * time.Second)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		status, err := resources.GetDeploymentRolloutStatus(ctx, principal, clusterID, namespace, deploymentName)
		if err != nil {
			return "", err
		}
		switch status.Status {
		case "healthy":
			return status.Message, nil
		case "degraded":
			return "", fmt.Errorf("rollout degraded: %s", status.Message)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("rollout timed out after %d seconds", timeoutSeconds)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
		}
	}
}

type checkDAGNodeHandler struct {
	client *http.Client
	store  ExecutionTaskStore
}

func (h checkDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if isExternalDAGExecutionNode(input.node) {
		return externalDAGNodeHandler{store: h.store}.execute(ctx, input)
	}
	targetURL := firstNonEmpty(configString(input.node.Config, "url"), configString(input.node.Config, "endpoint"))
	if targetURL == "" {
		return skippedDAGNode("HTTP check skipped because url is not configured")
	}
	expectedStatus := toInt(input.node.Config["expectedStatus"], http.StatusOK)
	if err := checkDAGHTTP(ctx, h.client, targetURL, expectedStatus); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(fmt.Sprintf("HTTP check %s returned %d", targetURL, expectedStatus))
}

func checkDAGHTTP(ctx context.Context, client *http.Client, targetURL string, expectedStatus int) error {
	if client == nil {
		client = &http.Client{Timeout: defaultDAGHTTPTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != expectedStatus {
		return fmt.Errorf("HTTP check got status %d, want %d", resp.StatusCode, expectedStatus)
	}
	return nil
}

type k8sEventDAGNodeHandler struct {
	resources dagEventReader
}

type dagEventReader interface {
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
}

func (h k8sEventDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	workflowInput := input.workflowInput
	if err := checkDAGK8sEvents(ctx, h.resources, input.principal, workflowInput.ClusterID, workflowInput.Namespace, workflowInput.DeploymentName, input.node.Config); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode("no blocking kubernetes event detected")
}

func checkDAGK8sEvents(ctx context.Context, resources dagEventReader, principal domainidentity.Principal, clusterID, namespace, deploymentName string, config map[string]any) error {
	events, err := resources.ListClusterEvents(ctx, principal, clusterID, namespace, 50)
	if err != nil {
		return err
	}
	eventType := strings.TrimSpace(fmt.Sprint(config["eventType"]))
	reasonContains := strings.TrimSpace(fmt.Sprint(config["reasonContains"]))
	for _, item := range events {
		if item.InvolvedName != deploymentName {
			continue
		}
		if eventType != "" && item.Type != eventType {
			continue
		}
		if reasonContains != "" && !strings.Contains(strings.ToLower(item.Reason), strings.ToLower(reasonContains)) {
			continue
		}
		if item.Type == "Warning" {
			return fmt.Errorf("found warning event %s: %s", item.Reason, item.Message)
		}
	}
	return nil
}

type rollbackDAGNodeHandler struct {
	resources dagRollbackExecutor
}

type dagRollbackExecutor interface {
	dagRolloutReader
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
}

func (h rollbackDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	target := input.firstSelectedTarget()
	result, err := rollbackDAGToPreviousRevision(
		ctx,
		h.resources,
		input.principal,
		targetClusterID(target, input.workflowInput),
		targetNamespace(target, input.workflowInput),
		targetWorkloadName(target, input.workflowInput),
	)
	if err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(result.Message)
}

func rollbackDAGToPreviousRevision(ctx context.Context, resources dagRollbackExecutor, principal domainidentity.Principal, clusterID, namespace, deploymentName string) (domainresource.DeploymentRollbackView, error) {
	history, err := resources.ListDeploymentRolloutHistory(ctx, principal, clusterID, namespace, deploymentName)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	if len(history) < 2 {
		return domainresource.DeploymentRollbackView{}, errors.New("no previous revision available for rollback")
	}
	current, err := resources.GetDeploymentRolloutStatus(ctx, principal, clusterID, namespace, deploymentName)
	if err != nil {
		return domainresource.DeploymentRollbackView{}, err
	}
	targetRevision := ""
	for _, item := range history {
		if item.Revision != "" && item.Revision != current.Revision {
			targetRevision = item.Revision
			break
		}
	}
	if targetRevision == "" {
		return domainresource.DeploymentRollbackView{}, errors.New("unable to resolve previous deployment revision")
	}
	return resources.RollbackDeployment(ctx, principal, clusterID, namespace, deploymentName, targetRevision)
}

type restartDAGNodeHandler struct {
	resources dagRestarter
}

type dagRestarter interface {
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
}

func (h restartDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	target := input.firstSelectedTarget()
	deploymentName := firstNonEmpty(configString(input.node.Config, "deploymentName"), selectedTargetWorkloadName(target), strings.TrimSpace(input.workflowInput.DeploymentName))
	if deploymentName == "" {
		return failedDAGNode(errors.New("restart workload requires deploymentName"))
	}
	if err := h.resources.RestartDeployment(ctx, input.principal, targetClusterID(target, input.workflowInput), targetNamespace(target, input.workflowInput), deploymentName); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(fmt.Sprintf("restarted deployment %s", deploymentName))
}

type scaleDAGNodeHandler struct {
	resources dagScaler
}

type dagScaler interface {
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
}

func (h scaleDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	target := input.firstSelectedTarget()
	deploymentName := firstNonEmpty(configString(input.node.Config, "deploymentName"), selectedTargetWorkloadName(target), strings.TrimSpace(input.workflowInput.DeploymentName))
	if deploymentName == "" {
		return failedDAGNode(errors.New("scale workload requires deploymentName"))
	}
	replicas, err := dagReplicaCount(input.node.Config["replicas"])
	if err != nil {
		return failedDAGNode(err)
	}
	if err := h.resources.ScaleDeployment(ctx, input.principal, targetClusterID(target, input.workflowInput), targetNamespace(target, input.workflowInput), deploymentName, replicas); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(fmt.Sprintf("scaled deployment %s to %d", deploymentName, replicas))
}

type deletePodDAGNodeHandler struct {
	resources dagPodDeleter
}

type dagPodDeleter interface {
	DeletePod(context.Context, domainidentity.Principal, string, string, string) error
}

func (h deletePodDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.resources == nil {
		return failedDAGNode(errors.New("resource executor is not configured"))
	}
	podName := firstNonEmpty(configString(input.node.Config, "podName"), configString(input.node.Config, "name"))
	if podName == "" {
		return failedDAGNode(errors.New("delete pod requires podName"))
	}
	target := input.firstSelectedTarget()
	if err := h.resources.DeletePod(ctx, input.principal, targetClusterID(target, input.workflowInput), targetNamespace(target, input.workflowInput), podName); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(fmt.Sprintf("%s executed for pod %s", input.node.Type, podName))
}

type callbackDAGNodeHandler struct {
	client *http.Client
}

func (h callbackDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	callbackURL := configString(input.node.Config, "url")
	if callbackURL == "" {
		return failedDAGNode(errors.New("http callback requires url"))
	}
	method := firstNonEmpty(configString(input.node.Config, "method"), http.MethodPost)
	body := configString(input.node.Config, "body")
	expectedStatus := toInt(input.node.Config["expectedStatus"], http.StatusOK)
	if err := callDAGHTTPCallback(ctx, h.client, callbackURL, method, body, expectedStatus, toConfigMap(input.node.Config["headers"])); err != nil {
		return failedDAGNode(err)
	}
	return completedDAGNode(fmt.Sprintf("HTTP callback %s %s completed", method, callbackURL))
}

func callDAGHTTPCallback(ctx context.Context, client *http.Client, targetURL, method, body string, expectedStatus int, headers map[string]any) error {
	if client == nil {
		client = &http.Client{Timeout: defaultDAGHTTPTimeout}
	}
	req, err := http.NewRequestWithContext(ctx, method, targetURL, strings.NewReader(body))
	if err != nil {
		return err
	}
	for key, value := range headers {
		req.Header.Set(key, fmt.Sprint(value))
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if expectedStatus > 0 && resp.StatusCode != expectedStatus {
		return fmt.Errorf("HTTP callback got status %d, want %d", resp.StatusCode, expectedStatus)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("HTTP callback got status %d", resp.StatusCode)
	}
	return nil
}

func dagReplicaCount(value any) (int32, error) {
	replicas := toInt(value, 1)
	if replicas < 0 || replicas > math.MaxInt32 {
		return 0, fmt.Errorf("replicas %d is outside the int32 range", replicas)
	}
	return int32(replicas), nil
}

type silenceDAGNodeHandler struct {
	alerts AlertMutator
}

func (h silenceDAGNodeHandler) execute(ctx context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	if h.alerts == nil {
		return failedDAGNode(errors.New("alert mutator is not configured"))
	}
	name := firstNonEmpty(configString(input.node.Config, "name"), "workflow-generated-silence")
	durationMinutes := toInt(input.node.Config["durationMinutes"], 60)
	if durationMinutes <= 0 {
		durationMinutes = 60
	}
	startsAt := time.Now().UTC()
	matchers := toConfigMap(input.node.Config["matchers"])
	if len(matchers) == 0 {
		matchers = map[string]any{
			"clusterId": input.workflowInput.ClusterID,
			"namespace": input.workflowInput.Namespace,
		}
	}
	silence, err := h.alerts.CreateWorkflowSilence(ctx, input.principal, domainalert.SilenceInput{
		Name:     name,
		Matchers: matchers,
		Reason:   configString(input.node.Config, "reason"),
		StartsAt: startsAt,
		EndsAt:   startsAt.Add(time.Duration(durationMinutes) * time.Minute),
		Enabled:  true,
	})
	if err != nil {
		return failedDAGNode(err)
	}
	result := completedDAGNode(fmt.Sprintf("created silence %s", silence.ID))
	result.outputs["silenceId"] = silence.ID
	return result
}

type notifyDAGNodeHandler struct{}

func (notifyDAGNodeHandler) execute(_ context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	channel := configString(input.node.Config, "channel")
	if channel == "" {
		return skippedDAGNode("notification skipped because channel is not configured")
	}
	return completedDAGNode(fmt.Sprintf("notification queued for channel %s", channel))
}

type unsupportedDAGNodeHandler struct{}

func (unsupportedDAGNodeHandler) execute(_ context.Context, input dagNodeExecutionInput) dagNodeHandlerResult {
	return skippedDAGNode(fmt.Sprintf("node type %s is not executable yet", input.node.Type))
}
