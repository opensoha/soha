package copilot

import (
	"context"
	"fmt"
	"sort"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func (s *Service) RecordGlobalAssistantEvent(ctx context.Context, principal domainidentity.Principal, input domaincopilot.WorkbenchGlobalAssistantEventInput) error {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return err
	}
	action := normalizeGlobalAssistantAction(input.Action)
	if action == "" {
		return fmt.Errorf("%w: global assistant action is required", apperrors.ErrInvalidArgument)
	}
	input.Action = action
	input.Source = firstNonEmpty(input.Source, "global-assistant")
	s.recordGlobalAssistantAudit(ctx, principal, input, domaincopilot.SessionMetadata{Source: "global-assistant"}, "success")
	return nil
}

func applyGlobalAssistantInput(metadata domaincopilot.SessionMetadata, input domaincopilot.WorkbenchSendMessageInput) (domaincopilot.SessionMetadata, bool) {
	changed := false
	if strings.TrimSpace(input.Source) != "" {
		source := normalizeSessionSource(input.Source)
		if source != metadata.Source {
			metadata.Source = source
			changed = true
		}
	}
	if input.PinnedContext != nil {
		pinned := compactMetadataMap(input.PinnedContext)
		metadata.PinnedContext = pinned
		changed = true
	} else if input.LaunchContext != nil {
		pinned := launchContextPinnedContext(input.LaunchContext)
		if len(pinned) > 0 {
			metadata.PinnedContext = pinned
			changed = true
		}
	}
	if input.LaunchContext != nil {
		scope := scopeFromLaunchContext(*input.LaunchContext)
		if scope.ClusterID != "" || scope.Namespace != "" || scope.Workload != "" || scope.Service != "" || scope.Pod != "" || scope.Node != "" || scope.AlertID != "" || scope.TimeRangeMinutes > 0 {
			metadata.Scope = mergeSessionScope(metadata.Scope, sessionScopeMap(scope))
			changed = true
		}
	}
	return metadata, changed
}

func launchContextPinnedContext(context *domaincopilot.WorkbenchLaunchContext) map[string]any {
	if context == nil {
		return nil
	}
	return compactMetadataMap(map[string]any{
		"sourceWorkbench":            context.SourceWorkbench,
		"sourceRoute":                context.SourceRoute,
		"sourceTitle":                context.SourceTitle,
		"entityKind":                 context.EntityKind,
		"entityName":                 context.EntityName,
		"clusterId":                  context.ClusterID,
		"namespace":                  context.Namespace,
		"workload":                   context.Workload,
		"service":                    context.Service,
		"pod":                        context.Pod,
		"node":                       context.Node,
		"alertId":                    context.AlertID,
		"applicationId":              context.ApplicationID,
		"releaseBundleId":            context.ReleaseBundleID,
		"dockerHostId":               context.DockerHostID,
		"dockerServiceId":            context.DockerServiceID,
		"virtualizationConnectionId": context.VirtualizationConnectionID,
		"vmId":                       context.VMID,
		"timeRangeMinutes":           context.TimeRangeMinutes,
		"visibleFilters":             compactMetadataMap(context.VisibleFilters),
		"pinnedData":                 compactMetadataMap(context.PinnedData),
	})
}

func scopeFromLaunchContext(context domaincopilot.WorkbenchLaunchContext) domaincopilot.SessionScope {
	return domaincopilot.SessionScope{
		ClusterID:        strings.TrimSpace(context.ClusterID),
		Namespace:        strings.TrimSpace(context.Namespace),
		Workload:         strings.TrimSpace(context.Workload),
		Service:          strings.TrimSpace(context.Service),
		Pod:              strings.TrimSpace(context.Pod),
		Node:             strings.TrimSpace(context.Node),
		AlertID:          strings.TrimSpace(context.AlertID),
		TimeRangeMinutes: context.TimeRangeMinutes,
	}
}

func sessionScopeMap(scope domaincopilot.SessionScope) map[string]any {
	return compactMetadataMap(map[string]any{
		"clusterId":        scope.ClusterID,
		"namespace":        scope.Namespace,
		"workload":         scope.Workload,
		"service":          scope.Service,
		"pod":              scope.Pod,
		"node":             scope.Node,
		"alertId":          scope.AlertID,
		"timeRangeMinutes": scope.TimeRangeMinutes,
	})
}

func (s *Service) recordGlobalAssistantAudit(ctx context.Context, principal domainidentity.Principal, input domaincopilot.WorkbenchGlobalAssistantEventInput, metadata domaincopilot.SessionMetadata, result string) {
	if s == nil || s.audits == nil {
		return
	}
	source := normalizeSessionSource(firstNonEmpty(input.Source, metadata.Source))
	if source != "global-assistant" {
		return
	}
	action := normalizeGlobalAssistantAction(input.Action)
	if action == "" {
		return
	}
	meta := requestctx.FromContext(ctx)
	resourceKind, resourceName := globalAssistantResource(metadata, input.LaunchContext)
	_ = s.audits.Record(ctx, domainaudit.Entry{
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Roles:         append([]string(nil), principal.Roles...),
		Teams:         append([]string(nil), principal.Teams...),
		ClusterID:     firstNonEmpty(metadata.Scope.ClusterID, launchContextString(input.LaunchContext, "clusterId")),
		Namespace:     firstNonEmpty(metadata.Scope.Namespace, launchContextString(input.LaunchContext, "namespace")),
		ResourceKind:  resourceKind,
		ResourceName:  resourceName,
		Action:        "global_assistant." + strings.ReplaceAll(action, "-", "_"),
		Result:        result,
		Summary:       globalAssistantAuditSummary(action, resourceKind, resourceName),
		RequestPath:   meta.Path,
		RequestMethod: meta.Method,
		RequestID:     meta.RequestID,
		SourceIP:      meta.SourceIP,
		Metadata:      globalAssistantAuditMetadata(input, metadata),
	})
}

func normalizeGlobalAssistantAction(action string) string {
	switch strings.TrimSpace(action) {
	case "open", "send", "open-workbench", "analyze-page", "analyze-selection", "analyze-resource":
		return strings.TrimSpace(action)
	case "troubleshoot-resource":
		return "analyze-resource"
	case "explain-selection", "troubleshoot-selection", "summarize-selection", "next-steps-selection":
		return "analyze-selection"
	default:
		return ""
	}
}

func globalAssistantResource(metadata domaincopilot.SessionMetadata, context *domaincopilot.WorkbenchLaunchContext) (string, string) {
	if context != nil {
		if strings.TrimSpace(context.EntityKind) != "" || strings.TrimSpace(context.EntityName) != "" {
			return firstNonEmpty(context.EntityKind, "AIWorkbenchGlobalAssistant"), firstNonEmpty(context.EntityName, context.SourceTitle, context.SourceRoute)
		}
	}
	if metadata.PinnedContext != nil {
		return firstNonEmpty(stringValue(metadata.PinnedContext["entityKind"]), "AIWorkbenchGlobalAssistant"), firstNonEmpty(stringValue(metadata.PinnedContext["entityName"]), stringValue(metadata.PinnedContext["sourceTitle"]), stringValue(metadata.PinnedContext["sourceRoute"]))
	}
	return "AIWorkbenchGlobalAssistant", ""
}

func globalAssistantAuditSummary(action, resourceKind, resourceName string) string {
	target := strings.TrimSpace(firstNonEmpty(resourceName, resourceKind, "current page"))
	switch action {
	case "open":
		return "Global AI assistant opened for " + target
	case "send":
		return "Global AI assistant prompt sent for " + target
	case "open-workbench":
		return "Global AI assistant opened full Workbench for " + target
	default:
		return "Global AI assistant action " + action + " for " + target
	}
}

func globalAssistantAuditMetadata(input domaincopilot.WorkbenchGlobalAssistantEventInput, metadata domaincopilot.SessionMetadata) map[string]any {
	selectionKind := ""
	selectionLength := 0
	if input.SelectionContext != nil {
		selectionKind = input.SelectionContext.Kind
		selectionLength = len(input.SelectionContext.Text)
	}
	return compactMetadataMap(map[string]any{
		"sessionId":         input.SessionID,
		"source":            "global-assistant",
		"sourceWorkbench":   firstNonEmpty(launchContextString(input.LaunchContext, "sourceWorkbench"), stringValue(metadata.PinnedContext["sourceWorkbench"])),
		"sourceRoute":       firstNonEmpty(launchContextString(input.LaunchContext, "sourceRoute"), stringValue(metadata.PinnedContext["sourceRoute"])),
		"mode":              metadata.Mode,
		"agentProviderId":   metadata.AgentProviderID,
		"selectionKind":     selectionKind,
		"selectionLength":   selectionLength,
		"promptLength":      len(strings.TrimSpace(input.Prompt)),
		"pinnedContextKeys": sortedMapKeys(metadata.PinnedContext),
		"visibleFilterKeys": sortedMapKeys(mapValue(metadata.PinnedContext["visibleFilters"])),
		"pinnedDataKeys":    sortedMapKeys(mapValue(metadata.PinnedContext["pinnedData"])),
		"timeRangeMinutes":  metadata.Scope.TimeRangeMinutes,
		"scopeClusterId":    metadata.Scope.ClusterID,
		"scopeNamespace":    metadata.Scope.Namespace,
		"scopeWorkload":     metadata.Scope.Workload,
		"scopeService":      metadata.Scope.Service,
		"scopePod":          metadata.Scope.Pod,
		"scopeNode":         metadata.Scope.Node,
		"scopeAlertId":      metadata.Scope.AlertID,
	})
}

func launchContextString(context *domaincopilot.WorkbenchLaunchContext, key string) string {
	if context == nil {
		return ""
	}
	switch key {
	case "sourceWorkbench":
		return context.SourceWorkbench
	case "sourceRoute":
		return context.SourceRoute
	case "clusterId":
		return context.ClusterID
	case "namespace":
		return context.Namespace
	default:
		return ""
	}
}

func sortedMapKeys(input map[string]any) []string {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		if strings.TrimSpace(key) != "" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}
