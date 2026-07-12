package resource

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	defaultHelmInstallTimeoutSeconds = 300
	maxHelmInstallTimeoutSeconds     = 3600
)

func (h *Helm) InstallHelmChart(ctx context.Context, principal domainidentity.Principal, clusterID string, input domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error) {
	s := h
	input = normalizeHelmChartInstallInput(input)
	if err := validateHelmChartInstallInput(input); err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	connection, _, err := s.authorize(ctx, principal, clusterID, input.Namespace, "HelmRelease", domainaccess.ActionCreate)
	if err != nil {
		return domainresource.HelmChartInstallResult{}, err
	}
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, err := s.helmAgentClient(connection)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", input.ReleaseName, string(domainaccess.ActionCreate), "failure", err.Error())
			return domainresource.HelmChartInstallResult{}, err
		}
		result, err := client.InstallHelmChart(ctx, input)
		if err != nil {
			_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", input.ReleaseName, string(domainaccess.ActionCreate), "failure", err.Error())
			return domainresource.HelmChartInstallResult{}, err
		}
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, string(domainaccess.ActionCreate), "success", fmt.Sprintf("installed helm chart %s %s via agent", input.ChartName, input.Version))
		s.recordOperation(ctx, principal, "platform.helm_release.install", connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, "installed helm chart via agent", map[string]any{
			"repositoryName": input.RepositoryName, "repositoryUrl": input.RepositoryURL,
			"chartName": input.ChartName, "version": input.Version,
			"createNamespace": input.CreateNamespace, "wait": input.Wait,
			"timeoutSeconds": input.TimeoutSeconds, "source": "agent",
		})
		return result, nil
	}

	result, err := s.direct.InstallHelmChart(ctx, clusterID, input)
	if err != nil {
		_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", input.ReleaseName, string(domainaccess.ActionCreate), "failure", err.Error())
		return domainresource.HelmChartInstallResult{}, err
	}
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, string(domainaccess.ActionCreate), "success", fmt.Sprintf("installed helm chart %s %s", input.ChartName, input.Version))
	s.recordOperation(ctx, principal, "platform.helm_release.install", connection.Summary.ID, input.Namespace, "HelmRelease", result.Name, "installed helm chart", map[string]any{
		"repositoryName": input.RepositoryName, "repositoryUrl": input.RepositoryURL,
		"chartName": input.ChartName, "version": input.Version,
		"createNamespace": input.CreateNamespace, "wait": input.Wait,
		"timeoutSeconds": input.TimeoutSeconds,
	})
	return result, nil
}

func normalizeHelmChartInstallInput(input domainresource.HelmChartInstallInput) domainresource.HelmChartInstallInput {
	input.RepositoryName = strings.TrimSpace(input.RepositoryName)
	input.RepositoryURL = strings.TrimSpace(input.RepositoryURL)
	input.ChartName = strings.TrimSpace(input.ChartName)
	input.Version = strings.TrimSpace(input.Version)
	input.ReleaseName = strings.TrimSpace(input.ReleaseName)
	input.Namespace = strings.TrimSpace(input.Namespace)
	if input.TimeoutSeconds <= 0 {
		input.TimeoutSeconds = defaultHelmInstallTimeoutSeconds
	}
	if input.TimeoutSeconds > maxHelmInstallTimeoutSeconds {
		input.TimeoutSeconds = maxHelmInstallTimeoutSeconds
	}
	return input
}

func validateHelmChartInstallInput(input domainresource.HelmChartInstallInput) error {
	if input.RepositoryURL == "" {
		return fmt.Errorf("%w: repositoryUrl is required", apperrors.ErrInvalidArgument)
	}
	if input.ChartName == "" {
		return fmt.Errorf("%w: chartName is required", apperrors.ErrInvalidArgument)
	}
	if input.Version == "" {
		return fmt.Errorf("%w: version is required", apperrors.ErrInvalidArgument)
	}
	if input.ReleaseName == "" {
		return fmt.Errorf("%w: releaseName is required", apperrors.ErrInvalidArgument)
	}
	if input.Namespace == "" {
		return fmt.Errorf("%w: namespace is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func parseHelmInstallValues(valuesYAML string) (map[string]interface{}, error) {
	values := map[string]interface{}{}
	if strings.TrimSpace(valuesYAML) == "" {
		return values, nil
	}
	if err := yaml.Unmarshal([]byte(valuesYAML), &values); err != nil {
		return nil, fmt.Errorf("%w: invalid values yaml: %v", apperrors.ErrInvalidArgument, err)
	}
	if values == nil {
		return map[string]interface{}{}, nil
	}
	return values, nil
}

func maxInt(left, right int) int {
	if left > right {
		return left
	}
	return right
}
