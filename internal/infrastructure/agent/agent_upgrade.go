package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	contractruntime "github.com/opensoha/soha-contracts/resource/runtime"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/mod/semver"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/yaml"
)

const agentWorkload = "soha-agent"

const agentUpgradeLegacyReason = "This Agent does not support safe remote updates. Use the Agent installation command once to update it."

func (c *Client) agentUpgradeRollout(ctx context.Context) (domainresource.DeploymentRolloutStatusView, error) {
	var payload struct {
		Data domainresource.DeploymentRolloutStatusView `json:"data"`
	}
	// The read-only probe checks the actual ownership-aware route family, not a
	// version string. Older Agents must be bootstrapped with the installer once.
	err := c.request(ctx, http.MethodGet, "/api/v1/platform/ownership-v2/workloads/deployments/soha-agent/rollout-status?namespace=soha-agent", nil, &payload)
	return payload.Data, err
}

func (c *Client) agentDeployment(ctx context.Context) (*appsv1.Deployment, error) {
	manifest, err := c.GetDeploymentYAML(ctx, agentWorkload, agentWorkload)
	if err != nil {
		return nil, err
	}
	var deployment appsv1.Deployment
	if err := yaml.Unmarshal([]byte(manifest.Content), &deployment); err != nil {
		return nil, fmt.Errorf("decode Agent deployment: %w", err)
	}
	if deployment.Name != agentWorkload || deployment.Namespace != agentWorkload {
		return nil, fmt.Errorf("%w: standard Agent deployment was not found", apperrors.ErrConflict)
	}
	return &deployment, nil
}

func agentDeploymentImage(deployment *appsv1.Deployment) (string, error) {
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name == agentWorkload && strings.HasPrefix(container.Image, "ghcr.io/opensoha/soha-agent:") {
			return container.Image, nil
		}
	}
	return "", fmt.Errorf("%w: update custom Agent installations with their original deployment tool", apperrors.ErrConflict)
}

func (c *Client) GetAgentUpgradeStatus(ctx context.Context) (domaincluster.AgentUpgradeStatus, error) {
	var payload struct {
		Data struct {
			Version string `json:"version"`
		} `json:"data"`
	}
	if err := c.request(ctx, http.MethodGet, "/api/v1/version", nil, &payload); err != nil {
		return domaincluster.AgentUpgradeStatus{}, err
	}
	deployment, err := c.agentDeployment(ctx)
	if err != nil {
		return domaincluster.AgentUpgradeStatus{}, err
	}
	rollout, err := c.agentUpgradeRollout(ctx)
	legacy := errors.Is(err, apperrors.ErrUnsupportedOperation)
	if legacy {
		rollout, err = c.GetDeploymentRolloutStatus(ctx, agentWorkload, agentWorkload)
	}
	if err != nil {
		return domaincluster.AgentUpgradeStatus{}, err
	}
	status := domaincluster.AgentUpgradeStatus{Version: payload.Data.Version, RolloutStatus: rollout.Status, Message: rollout.Message}
	status.Image, err = agentDeploymentImage(deployment)
	if err == nil {
		err = contractruntime.ValidateDirectManifestOwner(deployment)
	}
	status.CanUpgrade = err == nil
	if err != nil {
		status.UpgradeDisabledReason = err.Error()
	}
	if legacy {
		status.CanUpgrade = false
		status.UpgradeDisabledReason = agentUpgradeLegacyReason
	}
	if rollout.ObservedGeneration < deployment.Generation {
		status.RolloutStatus = "progressing"
		status.Message = "waiting for the Deployment controller to observe the new image"
	}
	return status, nil
}

func (c *Client) UpgradeAgent(ctx context.Context, image string) (string, error) {
	status, err := c.GetAgentUpgradeStatus(ctx)
	if err != nil {
		return "", err
	}
	if !status.CanUpgrade {
		if status.UpgradeDisabledReason == agentUpgradeLegacyReason {
			return "", apperrors.NewBusiness(apperrors.ErrConflict, "agent_upgrade_requires_bootstrap", agentUpgradeLegacyReason, "当前 Agent 不支持安全远程更新，请先使用 Agent 安装命令更新一次。")
		}
		return "", fmt.Errorf("%w: %s", apperrors.ErrConflict, status.UpgradeDisabledReason)
	}
	current := "v" + strings.TrimPrefix(status.Version, "v")
	target := strings.TrimPrefix(image, "ghcr.io/opensoha/soha-agent:")
	if semver.IsValid(current) && semver.Compare(target, current) < 0 {
		return "", apperrors.NewBusiness(apperrors.ErrConflict, "agent_downgrade_not_supported", "Choose the running Agent version or a newer release", "请选择当前 Agent 版本或更新的版本。")
	}
	_, previous, err := c.UpdateDeploymentImage(ctx, agentWorkload, agentWorkload, agentWorkload, image)
	return previous, err
}
