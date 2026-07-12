package workflow

import (
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func evaluateDAGRunCondition(condition string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) (bool, string) {
	expr := strings.TrimSpace(condition)
	if expr == "" || strings.EqualFold(expr, "always") || strings.EqualFold(expr, "true") {
		return true, ""
	}
	if strings.EqualFold(expr, "never") || strings.EqualFold(expr, "false") {
		return false, fmt.Sprintf("runCondition %q is false", condition)
	}
	for _, operator := range []string{"==", "!="} {
		if !strings.Contains(expr, operator) {
			continue
		}
		parts := strings.SplitN(expr, operator, 2)
		left := strings.TrimSpace(parts[0])
		right := trimDAGConditionLiteral(parts[1])
		actual := dagConditionValue(left, app, input, binding, artifactState)
		matched := actual == right
		if operator == "!=" {
			matched = actual != right
		}
		if matched {
			return true, ""
		}
		return false, fmt.Sprintf("runCondition %q not met: %s is %q", condition, left, actual)
	}
	actual := dagConditionValue(expr, app, input, binding, artifactState)
	if actual == "" || strings.EqualFold(actual, "false") || actual == "0" {
		return false, fmt.Sprintf("runCondition %q not met", condition)
	}
	return true, ""
}

func trimDAGConditionLiteral(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	return value
}

func dagConditionValue(key string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) string {
	key = strings.TrimSpace(key)
	switch key {
	case "branch", "ref", "refName":
		return firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main")
	case "refType":
		return firstNonEmpty(input.RefType, binding.BuildPolicy.RefType)
	case "applicationId", "app.id", "application.id":
		return app.ID
	case "application", "app", "app.key", "application.key":
		return firstNonEmpty(app.Key, app.Name, app.ID)
	case "environment", "environmentKey":
		return binding.EnvironmentKey
	case "environmentId":
		return binding.EnvironmentID
	case "image", "artifact.image":
		return dagArtifactRuntimeString(artifactState["image"])
	case "namespace":
		return input.Namespace
	case "cluster", "clusterId":
		return input.ClusterID
	case "deployment", "deploymentName":
		return input.DeploymentName
	default:
		if strings.HasPrefix(key, "variables.") {
			return workflowMetadataString(input.Variables, strings.TrimPrefix(key, "variables."))
		}
		if value, ok := artifactState[key]; ok {
			return dagArtifactRuntimeString(value)
		}
		return workflowMetadataString(input.Variables, key)
	}
}

func resolveDAGNodeInputs(node dagWorkflowNode, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) map[string]any {
	if len(node.Inputs) == 0 {
		return nil
	}
	resolved := make(map[string]any, len(node.Inputs))
	for _, ref := range node.Inputs {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			continue
		}
		if value, ok := resolveDAGInputReference(ref, app, input, binding, artifactState); ok {
			resolved[ref] = value
		}
	}
	return resolved
}

func resolveDAGInputReference(ref string, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, artifactState map[string]any) (any, bool) {
	if value, ok := artifactState[ref]; ok {
		return value, true
	}
	parts := strings.Split(ref, ".")
	for _, part := range parts {
		if value, ok := artifactState[strings.TrimSpace(part)]; ok {
			return value, true
		}
	}
	switch ref {
	case "source":
		return map[string]any{
			"refType":       firstNonEmpty(input.RefType, binding.BuildPolicy.RefType, "branch"),
			"refName":       firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main"),
			"buildSourceId": firstNonEmpty(input.BuildSourceID, binding.BuildPolicy.SourceID),
		}, true
	case "application", "app":
		return map[string]any{"id": app.ID, "key": app.Key, "name": app.Name}, true
	case "applicationId", "application.id", "app.id":
		return app.ID, true
	case "branch", "ref", "refName":
		return firstNonEmpty(input.RefName, binding.BuildPolicy.RefValue, app.DefaultBranch, "main"), true
	case "image", "imageTag":
		return input.ImageTag, true
	case "environment", "applicationEnvironment":
		return map[string]any{"id": binding.ID, "environmentId": binding.EnvironmentID, "environmentKey": binding.EnvironmentKey}, true
	case "environmentId":
		return binding.EnvironmentID, true
	case "environmentKey":
		return binding.EnvironmentKey, true
	case "target":
		return map[string]any{"clusterId": input.ClusterID, "namespace": input.Namespace, "workloadName": input.DeploymentName}, true
	case "cluster", "clusterId":
		return input.ClusterID, true
	case "namespace":
		return input.Namespace, true
	case "deployment", "deploymentName":
		return input.DeploymentName, true
	default:
		if strings.HasPrefix(ref, "variables.") {
			key := strings.TrimPrefix(ref, "variables.")
			if value, ok := input.Variables[key]; ok {
				return value, true
			}
		}
		if value, ok := input.Variables[ref]; ok {
			return value, true
		}
		return nil, false
	}
}

func resolveDAGNodeSelectors(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode) (map[string]any, error) {
	return resolveDAGNodeSelectorsWithServices(app, input, binding, node, nil)
}

func resolveDAGNodeSelectorsWithServices(app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, node dagWorkflowNode, services []domainapp.Service) (map[string]any, error) {
	resolved := map[string]any{}
	if len(node.ServiceSelector) > 0 {
		service, err := resolveDAGServiceSelector(app, binding, services, node.ServiceSelector)
		if err != nil {
			return nil, fmt.Errorf("%w: delivery_dag node %s serviceSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
		}
		resolved["service"] = service
	}
	if len(node.EnvironmentSelector) > 0 {
		environment, err := resolveDAGEnvironmentSelector(binding, node.EnvironmentSelector)
		if err != nil {
			return nil, fmt.Errorf("%w: delivery_dag node %s environmentSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
		}
		resolved["environment"] = environment
	}
	targets, err := resolveDAGTargets(binding, input, node.TargetSelector, normalizeDAGFanOutStrategy(node.FanOutStrategy) != "")
	if err != nil {
		return nil, fmt.Errorf("%w: delivery_dag node %s targetSelector cannot be resolved: %v", apperrors.ErrInvalidArgument, node.ID, err)
	}
	if len(targets) > 1 {
		strategy := normalizeDAGFanOutStrategy(node.FanOutStrategy)
		if strategy == "" {
			return nil, fmt.Errorf("%w: targetSelector matched %d targets but fanOut strategy is not declared", apperrors.ErrInvalidArgument, len(targets))
		}
		targetMetadata := make([]map[string]any, 0, len(targets))
		for _, target := range targets {
			targetMetadata = append(targetMetadata, releaseTargetMetadata(target))
		}
		resolved["target"] = targetMetadata[0]
		resolved["targets"] = targetMetadata
		resolved["fanOut"] = map[string]any{
			"strategy":      strategy,
			"batchSize":     node.FanOutBatchSize,
			"failurePolicy": firstNonEmpty(normalizeDAGFanOutFailurePolicy(node.FanOutFailurePolicy), effectiveDAGFailurePolicy(dagWorkflowDefinition{}, node)),
			"targetCount":   len(targetMetadata),
		}
	} else if len(targets) == 1 {
		resolved["target"] = releaseTargetMetadata(targets[0])
	}
	if len(resolved) == 0 {
		return nil, nil
	}
	return resolved, nil
}

func resolveDAGServiceSelector(app domainapp.App, binding domaincatalog.ApplicationEnvironment, services []domainapp.Service, selector map[string]any) (map[string]any, error) {
	labels := dagAppLabels(app, binding)
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 {
			return nil, fmt.Errorf("matchLabels is empty")
		}
		if serviceMatches := matchDAGApplicationServices(services, matchLabels, "matchLabels"); len(serviceMatches) > 0 {
			return serviceDiscoveryMetadata(app, serviceMatches, "application.services"), nil
		}
		if serviceKey := firstNonEmpty(matchLabels["service"], matchLabels["serviceKey"], matchLabels["app"], matchLabels["application"]); serviceKey != "" {
			return map[string]any{"serviceKey": serviceKey, "applicationId": app.ID, "source": "matchLabels"}, nil
		}
		if !labelsMatch(labels, matchLabels) {
			return nil, fmt.Errorf("matchLabels did not match application labels")
		}
	}
	key := selectorString(selector, "service", "serviceKey", "key", "name")
	if key != "" {
		if serviceMatches := matchDAGApplicationServicesByKey(services, key); len(serviceMatches) > 0 {
			return serviceDiscoveryMetadata(app, serviceMatches, "application.services"), nil
		}
		if key != app.ID && key != app.Key && key != app.Name && key != labels["service"] {
			return map[string]any{"serviceKey": key, "applicationId": app.ID, "source": "selector"}, nil
		}
	}
	if id := selectorString(selector, "id", "applicationId"); id != "" && id != app.ID {
		return nil, fmt.Errorf("application id %s does not match %s", id, app.ID)
	}
	return map[string]any{
		"applicationId": app.ID,
		"serviceKey":    firstNonEmpty(key, app.Key, app.Name, app.ID),
		"serviceName":   firstNonEmpty(app.Name, app.Key, app.ID),
	}, nil
}

func resolveDAGEnvironmentSelector(binding domaincatalog.ApplicationEnvironment, selector map[string]any) (map[string]any, error) {
	if id := selectorString(selector, "id", "applicationEnvironmentId", "bindingId"); id != "" && id != binding.ID {
		return nil, fmt.Errorf("binding id %s does not match %s", id, binding.ID)
	}
	if environmentID := selectorString(selector, "environmentId"); environmentID != "" && environmentID != binding.EnvironmentID {
		return nil, fmt.Errorf("environment id %s does not match %s", environmentID, binding.EnvironmentID)
	}
	if key := selectorString(selector, "key", "environmentKey", "environment"); key != "" && key != binding.EnvironmentKey && key != binding.EnvironmentID {
		return nil, fmt.Errorf("environment key %s does not match %s", key, firstNonEmpty(binding.EnvironmentKey, binding.EnvironmentID))
	}
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 {
			return nil, fmt.Errorf("matchLabels is empty")
		}
		if !labelsMatch(dagBindingLabels(binding), matchLabels) {
			return nil, fmt.Errorf("matchLabels did not match environment labels")
		}
	}
	return map[string]any{
		"bindingId":      binding.ID,
		"environmentId":  binding.EnvironmentID,
		"environmentKey": binding.EnvironmentKey,
		"applicationId":  binding.ApplicationID,
	}, nil
}

func resolveDAGTargets(binding domaincatalog.ApplicationEnvironment, input domainworkflow.Input, selector map[string]any, fanOut bool) ([]domaincatalog.ReleaseTarget, error) {
	if len(selector) == 0 {
		if fanOut {
			targets := make([]domaincatalog.ReleaseTarget, 0, len(binding.Targets))
			for _, target := range binding.Targets {
				if target.Enabled {
					targets = append(targets, target)
				}
			}
			if len(targets) > 0 {
				return targets, nil
			}
		}
		if target := matchBindingTarget(binding, input); target != nil {
			return []domaincatalog.ReleaseTarget{*target}, nil
		}
		return nil, nil
	}
	matched := make([]domaincatalog.ReleaseTarget, 0)
	for _, target := range binding.Targets {
		if !target.Enabled {
			continue
		}
		if dagTargetMatchesSelector(target, binding, selector) {
			matched = append(matched, target)
		}
	}
	if len(matched) > 0 {
		return matched, nil
	}
	if input.ClusterID != "" || input.Namespace != "" || input.DeploymentName != "" {
		inputTarget := domaincatalog.ReleaseTarget{
			ID:            "input",
			ClusterID:     input.ClusterID,
			Namespace:     input.Namespace,
			WorkloadName:  input.DeploymentName,
			ContainerName: input.ContainerName,
			Enabled:       true,
		}
		if dagTargetMatchesSelector(inputTarget, binding, selector) {
			return []domaincatalog.ReleaseTarget{inputTarget}, nil
		}
	}
	return nil, fmt.Errorf("no enabled target matched selector")
}

func dagTargetMatchesSelector(target domaincatalog.ReleaseTarget, binding domaincatalog.ApplicationEnvironment, selector map[string]any) bool {
	for _, key := range []string{"id", "targetId"} {
		if value := selectorString(selector, key); value != "" && value != target.ID {
			return false
		}
	}
	if key := selectorString(selector, "key"); key != "" {
		if key != target.ID && key != target.GroupKey && key != target.WaveKey && key != target.RegionKey && key != binding.EnvironmentKey && key != strings.TrimSpace(fmt.Sprint(target.Metadata["key"])) {
			return false
		}
	}
	for selectorKey, targetValue := range map[string]string{
		"clusterId":    target.ClusterID,
		"namespace":    target.Namespace,
		"workloadName": target.WorkloadName,
		"deployment":   target.WorkloadName,
		"groupKey":     target.GroupKey,
		"waveKey":      target.WaveKey,
		"regionKey":    target.RegionKey,
	} {
		if value := selectorString(selector, selectorKey); value != "" && value != targetValue {
			return false
		}
	}
	if match, ok := selector["matchLabels"]; ok {
		matchLabels := stringMapFromAny(match)
		if len(matchLabels) == 0 || !labelsMatch(dagTargetLabels(target, binding), matchLabels) {
			return false
		}
	}
	return true
}

func releaseTargetMetadata(target domaincatalog.ReleaseTarget) map[string]any {
	return map[string]any{
		"id":             target.ID,
		"clusterId":      target.ClusterID,
		"namespace":      target.Namespace,
		"targetKind":     target.TargetKind,
		"executorKind":   target.ExecutorKind,
		"groupKey":       target.GroupKey,
		"waveKey":        target.WaveKey,
		"regionKey":      target.RegionKey,
		"workloadKind":   target.WorkloadKind,
		"workloadName":   target.WorkloadName,
		"containerName":  target.ContainerName,
		"configRef":      target.ConfigRef,
		"metadata":       target.Metadata,
		"resolvedSource": "applicationEnvironment.targets",
	}
}

func matchDAGApplicationServicesByKey(services []domainapp.Service, key string) []domainapp.Service {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	matches := make([]domainapp.Service, 0)
	for _, service := range services {
		if !service.Enabled {
			continue
		}
		if key == service.ID || key == service.Key || key == service.Name || key == strings.TrimSpace(fmt.Sprint(service.Metadata["service"])) || key == strings.TrimSpace(fmt.Sprint(service.Metadata["serviceKey"])) {
			matches = append(matches, service)
		}
	}
	return matches
}

func matchDAGApplicationServices(services []domainapp.Service, labels map[string]string, source string) []domainapp.Service {
	if len(services) == 0 || len(labels) == 0 {
		return nil
	}
	matches := make([]domainapp.Service, 0)
	for _, service := range services {
		if !service.Enabled {
			continue
		}
		if labelsMatch(dagServiceLabels(service), labels) {
			matches = append(matches, service)
		}
	}
	_ = source
	return matches
}

func serviceDiscoveryMetadata(app domainapp.App, services []domainapp.Service, source string) map[string]any {
	items := make([]map[string]any, 0, len(services))
	for _, service := range services {
		containers := make([]map[string]any, 0, len(service.Containers))
		for _, container := range service.Containers {
			containers = append(containers, map[string]any{
				"id":              container.ID,
				"name":            container.Name,
				"imageRepository": container.ImageRepository,
				"runtimePorts":    container.RuntimePorts,
			})
		}
		items = append(items, map[string]any{
			"id":            service.ID,
			"key":           service.Key,
			"name":          service.Name,
			"serviceKind":   service.ServiceKind,
			"buildSourceId": service.BuildSourceID,
			"metadata":      service.Metadata,
			"containers":    containers,
		})
	}
	result := map[string]any{
		"applicationId": app.ID,
		"source":        source,
		"services":      items,
		"serviceCount":  len(items),
	}
	if len(items) > 0 {
		result["serviceKey"] = items[0]["key"]
		result["serviceName"] = items[0]["name"]
	}
	return result
}

func dagServiceLabels(service domainapp.Service) map[string]string {
	labels := map[string]string{
		"serviceId":   service.ID,
		"serviceKey":  service.Key,
		"service":     service.Key,
		"key":         service.Key,
		"name":        service.Name,
		"serviceKind": string(service.ServiceKind),
	}
	for key, value := range service.Metadata {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			labels[key] = text
		}
	}
	return labels
}

func dagAppLabels(app domainapp.App, binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := map[string]string{
		"applicationId":   app.ID,
		"applicationKey":  app.Key,
		"app":             app.Key,
		"service":         app.Key,
		"name":            app.Name,
		"group":           app.Group,
		"businessLineId":  app.BusinessLineID,
		"environmentKey":  binding.EnvironmentKey,
		"environmentId":   binding.EnvironmentID,
		"applicationName": app.Name,
	}
	for key, value := range app.Metadata {
		labels[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return labels
}

func dagBindingLabels(binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := map[string]string{
		"bindingId":        binding.ID,
		"applicationId":    binding.ApplicationID,
		"environmentId":    binding.EnvironmentID,
		"environmentKey":   binding.EnvironmentKey,
		"businessLineId":   binding.BusinessLineID,
		"applicationGroup": binding.ApplicationGroup,
	}
	for key, value := range binding.ResourceSelector.MatchLabels {
		labels[key] = value
	}
	return labels
}

func dagTargetLabels(target domaincatalog.ReleaseTarget, binding domaincatalog.ApplicationEnvironment) map[string]string {
	labels := dagBindingLabels(binding)
	labels["targetId"] = target.ID
	labels["clusterId"] = target.ClusterID
	labels["namespace"] = target.Namespace
	labels["workloadKind"] = target.WorkloadKind
	labels["workloadName"] = target.WorkloadName
	labels["deployment"] = target.WorkloadName
	labels["groupKey"] = target.GroupKey
	labels["waveKey"] = target.WaveKey
	labels["regionKey"] = target.RegionKey
	for key, value := range target.Metadata {
		labels[key] = strings.TrimSpace(fmt.Sprint(value))
	}
	return labels
}

func labelsMatch(labels, matchLabels map[string]string) bool {
	for key, expected := range matchLabels {
		if strings.TrimSpace(expected) == "" {
			return false
		}
		if labels[key] != expected {
			return false
		}
	}
	return true
}

func selectorString(selector map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := mapString(selector, key); value != "" {
			return value
		}
	}
	return ""
}

func stringMapFromAny(value any) map[string]string {
	out := map[string]string{}
	switch current := value.(type) {
	case map[string]string:
		for key, item := range current {
			if text := strings.TrimSpace(item); text != "" {
				out[key] = text
			}
		}
	case map[string]any:
		for key, item := range current {
			if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
				out[key] = text
			}
		}
	}
	return out
}
