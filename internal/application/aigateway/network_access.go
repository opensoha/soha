package aigateway

import (
	"context"
	"fmt"
	"time"

	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkingest "github.com/opensoha/soha/internal/domain/networkingest"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type NetworkAccessPreviewService interface {
	PreviewPolicy(context.Context, domainidentity.Principal, appnetworkaccess.PreviewInput) (domainnetworkaccess.PolicyPreview, error)
	AnalyzeConflicts(context.Context, domainidentity.Principal, []domainnetworkaccess.ConflictRange) (domainnetworkaccess.ConflictAnalysis, error)
	ListSessions(context.Context, domainidentity.Principal, domainnetworkaccess.SessionFilter) ([]domainnetworkaccess.Session, error)
	PlanSessionAction(context.Context, domainidentity.Principal, string, domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionActionPlan, error)
	ExecuteSessionAction(context.Context, domainidentity.Principal, string, domainnetworkaccess.SessionActionInput) (domainnetworkaccess.SessionCommand, error)
	CreateGateway(context.Context, domainidentity.Principal, domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error)
	UpdateGateway(context.Context, domainidentity.Principal, string, domainnetworkaccess.GatewayInput) (domainnetworkaccess.Gateway, error)
	ListMihomoProfiles(context.Context, domainidentity.Principal, domainnetworkaccess.MihomoProfileFilter) ([]domainnetworkaccess.MihomoProfile, error)
}

type NetworkEnrollmentService interface {
	Create(context.Context, domainidentity.Principal, appnetworkaccess.EnrollmentInput) (domainnetworkruntime.EnrollmentSecret, error)
	List(context.Context, domainidentity.Principal, int) ([]domainnetworkruntime.EnrollmentChallenge, error)
	Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.EnrollmentChallenge, error)
}

type NetworkAccessGrantService interface {
	Create(context.Context, domainidentity.Principal, string, appnetworkaccess.AccessGrantInput) (domainnetworkruntime.AccessGrantSecret, error)
	List(context.Context, domainidentity.Principal, domainnetworkruntime.AccessGrantFilter) ([]domainnetworkruntime.AccessGrant, error)
	Revoke(context.Context, domainidentity.Principal, string) (domainnetworkruntime.AccessGrant, error)
}

type NetworkTelemetryService interface {
	Summary(context.Context, domainnetworkingest.SummaryFilter) (domainnetworkingest.Summary, error)
}

func (s *Service) SetNetworkAccessPreviewService(service NetworkAccessPreviewService) {
	s.networkAccess = service
}

func (s *Service) SetNetworkEnrollmentService(service NetworkEnrollmentService) {
	s.networkEnrollments = service
}

func (s *Service) SetNetworkAccessGrantService(service NetworkAccessGrantService) {
	s.networkAccessGrants = service
}

func (s *Service) SetNetworkTelemetryService(service NetworkTelemetryService) {
	s.networkTelemetry = service
}

func (s *Service) invokeNetworkAccessTool(ctx context.Context, principal domainidentity.Principal, sessionID, toolName string, input map[string]any) (any, map[string]any, error) {
	switch {
	case networkAccessReadTool(toolName):
		return s.invokeNetworkAccessReadTool(ctx, principal, toolName, input)
	case networkAccessRuntimeTool(toolName):
		return s.invokeNetworkAccessRuntimeTool(ctx, principal, toolName, input)
	case networkAccessGrantTool(toolName):
		return s.invokeNetworkAccessGrantTool(ctx, principal, sessionID, toolName, input)
	case networkEnrollmentTool(toolName):
		return s.invokeNetworkEnrollmentTool(ctx, principal, toolName, input)
	default:
		return unsupportedNetworkAccessTool(toolName)
	}
}

func networkAccessReadTool(toolName string) bool {
	switch toolName {
	case "network_access.policy.preview", "network_access.conflicts.analyze", "network_access.mihomo_profiles.list", "network_access.telemetry.summary":
		return true
	default:
		return false
	}
}

func networkAccessRuntimeTool(toolName string) bool {
	switch toolName {
	case "network_access.gateways.create", "network_access.gateways.update", "network_access.sessions.list", "network_access.session.coa.plan", "network_access.session.coa.execute":
		return true
	default:
		return false
	}
}

func networkAccessGrantTool(toolName string) bool {
	switch toolName {
	case "network_access.access_grants.create", "network_access.access_grants.list", "network_access.access_grants.revoke":
		return true
	default:
		return false
	}
}

func networkEnrollmentTool(toolName string) bool {
	switch toolName {
	case "network_access.enrollments.create", "network_access.enrollments.list", "network_access.enrollments.revoke":
		return true
	default:
		return false
	}
}

func unsupportedNetworkAccessTool(toolName string) (any, map[string]any, error) {
	return nil, nil, fmt.Errorf("%w: tool %s is not implemented yet", apperrors.ErrInvalidArgument, toolName)
}

func (s *Service) invokeNetworkAccessReadTool(ctx context.Context, principal domainidentity.Principal, toolName string, input map[string]any) (any, map[string]any, error) {
	switch toolName {
	case "network_access.policy.preview":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request appnetworkaccess.PreviewInput
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		result, err := s.networkAccess.PreviewPolicy(ctx, principal, request)
		return result, map[string]any{"subjectUserId": request.SubjectUserID, "deviceId": request.DeviceID, "resourceId": request.ResourceID, "siteId": request.SiteID, "path": result.Path, "policyVersion": result.PolicyVersion}, err
	case "network_access.conflicts.analyze":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			RuntimeRanges []domainnetworkaccess.ConflictRange `json:"runtimeRanges"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		result, err := s.networkAccess.AnalyzeConflicts(ctx, principal, request.RuntimeRanges)
		return result, map[string]any{"rangesAnalyzed": result.RangesAnalyzed, "conflictCount": len(result.Conflicts), "valid": result.Valid}, err
	case "network_access.mihomo_profiles.list":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			Search   string `json:"search"`
			DeviceID string `json:"deviceId"`
			Mode     string `json:"mode"`
			Status   string `json:"status"`
			Limit    int    `json:"limit"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		items, err := s.networkAccess.ListMihomoProfiles(ctx, principal, domainnetworkaccess.MihomoProfileFilter{Search: request.Search, DeviceID: request.DeviceID, Mode: request.Mode, Status: request.Status, Limit: request.Limit})
		return map[string]any{"items": items}, map[string]any{"count": len(items), "deviceId": request.DeviceID, "mode": request.Mode, "status": request.Status}, err
	case "network_access.telemetry.summary":
		if s.networkTelemetry == nil {
			return nil, nil, fmt.Errorf("%w: network telemetry service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			From       time.Time `json:"from"`
			To         time.Time `json:"to"`
			ProducerID string    `json:"producerId"`
			Limit      int       `json:"limit"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		result, err := s.networkTelemetry.Summary(ctx, domainnetworkingest.SummaryFilter{From: request.From, To: request.To, ProducerID: request.ProducerID, Limit: request.Limit})
		return result, map[string]any{"from": result.From, "to": result.To, "eventCount": result.EventCount, "producerCount": len(result.Producers), "proxyFlowCount": len(result.ProxyFlows), "producerId": request.ProducerID}, err
	default:
		return unsupportedNetworkAccessTool(toolName)
	}
}

func (s *Service) invokeNetworkAccessRuntimeTool(ctx context.Context, principal domainidentity.Principal, toolName string, input map[string]any) (any, map[string]any, error) {
	switch toolName {
	case "network_access.gateways.create", "network_access.gateways.update":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			GatewayID                  string   `json:"gatewayId"`
			RuntimeID                  string   `json:"runtimeId"`
			SiteID                     string   `json:"siteId"`
			Name                       string   `json:"name"`
			AdministrativeStatus       string   `json:"administrativeStatus"`
			PublicEndpointHost         string   `json:"publicEndpointHost"`
			PublicEndpointPort         int      `json:"publicEndpointPort"`
			OverlayCIDR                string   `json:"overlayCidr"`
			RoutingMode                string   `json:"routingMode"`
			MTU                        int      `json:"mtu"`
			PersistentKeepaliveSeconds int      `json:"persistentKeepaliveSeconds"`
			DNSServers                 []string `json:"dnsServers"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		gatewayInput := domainnetworkaccess.GatewayInput{
			RuntimeID: request.RuntimeID, SiteID: request.SiteID, Name: request.Name, AdministrativeStatus: request.AdministrativeStatus,
			PublicEndpointHost: request.PublicEndpointHost, PublicEndpointPort: request.PublicEndpointPort,
			OverlayCIDR: request.OverlayCIDR, RoutingMode: request.RoutingMode, MTU: request.MTU,
			PersistentKeepaliveSeconds: request.PersistentKeepaliveSeconds, DNSServers: request.DNSServers,
		}
		var result domainnetworkaccess.Gateway
		var err error
		if toolName == "network_access.gateways.create" {
			result, err = s.networkAccess.CreateGateway(ctx, principal, gatewayInput)
		} else {
			result, err = s.networkAccess.UpdateGateway(ctx, principal, request.GatewayID, gatewayInput)
		}
		return result, map[string]any{"gatewayId": result.ID, "runtimeId": result.RuntimeID, "siteId": result.SiteID}, err
	case "network_access.sessions.list":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			SiteID    string `json:"siteId"`
			RuntimeID string `json:"runtimeId"`
			SubjectID string `json:"subjectId"`
			DeviceID  string `json:"deviceId"`
			Status    string `json:"status"`
			Limit     int    `json:"limit"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		items, err := s.networkAccess.ListSessions(ctx, principal, domainnetworkaccess.SessionFilter{SiteID: request.SiteID, RuntimeID: request.RuntimeID, SubjectID: request.SubjectID, DeviceID: request.DeviceID, Status: request.Status, Limit: request.Limit})
		return map[string]any{"items": items}, map[string]any{"count": len(items), "siteId": request.SiteID, "runtimeId": request.RuntimeID, "subjectUserId": request.SubjectID, "deviceId": request.DeviceID}, err
	case "network_access.session.coa.plan", "network_access.session.coa.execute":
		if s.networkAccess == nil {
			return nil, nil, fmt.Errorf("%w: network access policy service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			SessionID           string `json:"sessionId"`
			Action              string `json:"action"`
			TargetAccessProfile string `json:"targetAccessProfile"`
			ReasonCode          string `json:"reasonCode"`
			PlanHash            string `json:"planHash"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		action := domainnetworkaccess.SessionActionInput{Action: request.Action, TargetAccessProfile: request.TargetAccessProfile, ReasonCode: request.ReasonCode, PlanHash: request.PlanHash}
		if toolName == "network_access.session.coa.plan" {
			result, err := s.networkAccess.PlanSessionAction(ctx, principal, request.SessionID, action)
			return result, map[string]any{"sessionId": result.SessionID, "runtimeId": result.RuntimeID, "nasId": result.NASID, "effectiveAction": result.EffectiveAction}, err
		}
		result, err := s.networkAccess.ExecuteSessionAction(ctx, principal, request.SessionID, action)
		return result, map[string]any{"commandId": result.ID, "sessionId": result.SessionID, "runtimeId": result.RuntimeID, "nasId": result.NASID}, err
	default:
		return unsupportedNetworkAccessTool(toolName)
	}
}

func (s *Service) invokeNetworkAccessGrantTool(ctx context.Context, principal domainidentity.Principal, sessionID, toolName string, input map[string]any) (any, map[string]any, error) {
	switch toolName {
	case "network_access.access_grants.create":
		if s.networkAccessGrants == nil {
			return nil, nil, fmt.Errorf("%w: network access grant service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			DeviceID       string   `json:"deviceId"`
			SiteID         string   `json:"siteId"`
			NetworkSpaceID string   `json:"networkSpaceId"`
			Mode           string   `json:"mode"`
			ResourceIDs    []string `json:"resourceIds"`
			TTLSeconds     int      `json:"ttlSeconds"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		secret, err := s.networkAccessGrants.Create(ctx, principal, sessionID, appnetworkaccess.AccessGrantInput{
			DeviceID: request.DeviceID, SiteID: request.SiteID, NetworkSpaceID: request.NetworkSpaceID,
			Mode: request.Mode, ResourceIDs: request.ResourceIDs, TTL: time.Duration(request.TTLSeconds) * time.Second,
		})
		return map[string]any{"grant": networkAccessGrantValue(secret.Grant), "token": secret.Token}, map[string]any{
			"grantId": secret.Grant.ID, "subjectUserId": secret.Grant.SubjectID, "deviceId": secret.Grant.DeviceID,
			"siteId": secret.Grant.SiteID, "networkSpaceId": secret.Grant.NetworkSpaceID,
		}, err
	case "network_access.access_grants.list":
		if s.networkAccessGrants == nil {
			return nil, nil, fmt.Errorf("%w: network access grant service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			SubjectID string `json:"subjectId"`
			DeviceID  string `json:"deviceId"`
			Status    string `json:"status"`
			Limit     int    `json:"limit"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		items, err := s.networkAccessGrants.List(ctx, principal, domainnetworkruntime.AccessGrantFilter{SubjectID: request.SubjectID, DeviceID: request.DeviceID, Status: request.Status, Limit: request.Limit})
		values := make([]map[string]any, len(items))
		for index, item := range items {
			values[index] = networkAccessGrantValue(item)
		}
		return map[string]any{"items": values}, map[string]any{"count": len(values), "subjectUserId": request.SubjectID, "deviceId": request.DeviceID}, err
	case "network_access.access_grants.revoke":
		if s.networkAccessGrants == nil {
			return nil, nil, fmt.Errorf("%w: network access grant service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			GrantID string `json:"grantId"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		grant, err := s.networkAccessGrants.Revoke(ctx, principal, request.GrantID)
		return networkAccessGrantValue(grant), map[string]any{"grantId": grant.ID, "subjectUserId": grant.SubjectID, "deviceId": grant.DeviceID}, err
	default:
		return unsupportedNetworkAccessTool(toolName)
	}
}

func (s *Service) invokeNetworkEnrollmentTool(ctx context.Context, principal domainidentity.Principal, toolName string, input map[string]any) (any, map[string]any, error) {
	switch toolName {
	case "network_access.enrollments.create":
		if s.networkEnrollments == nil {
			return nil, nil, fmt.Errorf("%w: network enrollment service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			RuntimeID   string `json:"runtimeId"`
			RuntimeKind string `json:"runtimeKind"`
			DeviceID    string `json:"deviceId"`
			SubjectID   string `json:"subjectId"`
			TTLSeconds  int    `json:"ttlSeconds"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		secret, err := s.networkEnrollments.Create(ctx, principal, appnetworkaccess.EnrollmentInput{
			RuntimeID: request.RuntimeID, RuntimeKind: request.RuntimeKind, DeviceID: request.DeviceID,
			SubjectID: request.SubjectID, TTL: time.Duration(request.TTLSeconds) * time.Second,
		})
		return map[string]any{"enrollment": networkEnrollmentValue(secret.EnrollmentChallenge), "token": secret.Token}, map[string]any{
			"enrollmentId": secret.ID, "runtimeId": secret.RuntimeID, "deviceId": secret.DeviceID, "subjectUserId": secret.SubjectID,
		}, err
	case "network_access.enrollments.list":
		if s.networkEnrollments == nil {
			return nil, nil, fmt.Errorf("%w: network enrollment service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			Limit int `json:"limit"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		items, err := s.networkEnrollments.List(ctx, principal, request.Limit)
		values := make([]map[string]any, len(items))
		for index, item := range items {
			values[index] = networkEnrollmentValue(item)
		}
		return map[string]any{"items": values}, map[string]any{"count": len(values)}, err
	case "network_access.enrollments.revoke":
		if s.networkEnrollments == nil {
			return nil, nil, fmt.Errorf("%w: network enrollment service is not configured", apperrors.ErrInvalidArgument)
		}
		var request struct {
			EnrollmentID string `json:"enrollmentId"`
		}
		if err := mapInput(input, &request); err != nil {
			return nil, nil, err
		}
		item, err := s.networkEnrollments.Revoke(ctx, principal, request.EnrollmentID)
		return networkEnrollmentValue(item), map[string]any{"enrollmentId": item.ID, "runtimeId": item.RuntimeID, "deviceId": item.DeviceID, "subjectUserId": item.SubjectID}, err
	default:
		return unsupportedNetworkAccessTool(toolName)
	}
}

func networkEnrollmentValue(item domainnetworkruntime.EnrollmentChallenge) map[string]any {
	value := map[string]any{
		"id": item.ID, "challengeId": item.ChallengeID, "runtimeId": item.RuntimeID, "runtimeKind": item.RuntimeKind,
		"deviceId": item.DeviceID, "subjectId": item.SubjectID, "status": item.Status, "expiresAt": item.ExpiresAt,
		"createdBy": item.CreatedBy, "createdAt": item.CreatedAt,
	}
	if item.ConsumedAt != nil {
		value["consumedAt"] = *item.ConsumedAt
	}
	if item.RevokedAt != nil {
		value["revokedAt"] = *item.RevokedAt
	}
	return value
}

func networkAccessGrantValue(item domainnetworkruntime.AccessGrant) map[string]any {
	value := map[string]any{
		"id": item.ID, "subjectId": item.SubjectID, "deviceId": item.DeviceID, "siteId": item.SiteID,
		"networkSpaceId": item.NetworkSpaceID, "mode": item.Mode, "resourceIds": item.ResourceIDs,
		"policyVersion": item.PolicyVersion, "status": item.Status, "resourceLeaseIds": item.ResourceLeaseIDs,
		"reasonCode": item.ReasonCode, "expiresAt": item.ExpiresAt, "createdBy": item.CreatedBy, "createdAt": item.CreatedAt,
	}
	if item.SessionID != "" {
		value["sessionId"] = item.SessionID
	}
	if item.ConsumedAt != nil {
		value["consumedAt"] = *item.ConsumedAt
	}
	if item.RevokedAt != nil {
		value["revokedAt"] = *item.RevokedAt
	}
	return value
}

func networkAccessGrantCreateInputSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"deviceId", "siteId", "networkSpaceId", "mode", "resourceIds"}, map[string]any{
		"deviceId": gatewayStringSchema("Endpoint device id."), "siteId": gatewayStringSchema("Network site id."),
		"networkSpaceId": gatewayStringSchema("Network space id."),
		"mode":           map[string]any{"type": "string", "enum": []any{"internal_ztna", "external_vpn_ztna", "external_direct_ztna"}},
		"resourceIds":    map[string]any{"type": "array", "minItems": 1, "maxItems": 256, "uniqueItems": true, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}},
		"ttlSeconds":     map[string]any{"type": "integer", "minimum": 60, "maximum": 300, "default": 300},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkAccessGrantFilterSchema() map[string]any {
	schema := gatewayObjectSchema(nil, map[string]any{
		"subjectId": gatewayStringSchema("Optional subject id."), "deviceId": gatewayStringSchema("Optional endpoint device id."),
		"status": map[string]any{"type": "string", "enum": []any{"issued", "consumed", "revoked", "expired"}},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 100},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkAccessGrantSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"id", "subjectId", "deviceId", "siteId", "networkSpaceId", "mode", "resourceIds", "policyVersion", "status", "expiresAt", "createdBy", "createdAt"}, map[string]any{
		"id": gatewayStringSchema("Access grant id."), "subjectId": gatewayStringSchema("Bound subject id."),
		"deviceId": gatewayStringSchema("Bound endpoint device id."), "siteId": gatewayStringSchema("Bound site id."),
		"networkSpaceId": gatewayStringSchema("Bound network space id."),
		"mode":           map[string]any{"type": "string", "enum": []any{"internal_ztna", "external_vpn_ztna", "external_direct_ztna"}},
		"resourceIds":    gatewayStringArraySchema("Bound resource ids."), "policyVersion": gatewayIntegerSchema("Bound policy version."),
		"status":    map[string]any{"type": "string", "enum": []any{"issued", "consumed", "revoked", "expired"}},
		"sessionId": gatewayStringSchema("Created network session id."), "resourceLeaseIds": gatewayStringArraySchema("Created resource lease ids."),
		"reasonCode": gatewayStringSchema("Stable decision reason."), "expiresAt": gatewayDateTimeSchema(),
		"consumedAt": gatewayDateTimeSchema(), "revokedAt": gatewayDateTimeSchema(), "createdBy": gatewayStringSchema("Creating principal id."), "createdAt": gatewayDateTimeSchema(),
	})
	schema["additionalProperties"] = false
	return schema
}

func networkAccessGrantSecretSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"grant", "token"}, map[string]any{
		"grant": networkAccessGrantSchema(),
		"token": map[string]any{"type": "string", "minLength": 32, "maxLength": 512, "description": "One-time network access grant token."},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkAccessGrantListSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"items"}, map[string]any{"items": map[string]any{"type": "array", "maxItems": 200, "items": networkAccessGrantSchema()}})
	schema["additionalProperties"] = false
	return schema
}

func networkEnrollmentCreateInputSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"runtimeId", "runtimeKind", "deviceId", "subjectId"}, map[string]any{
		"runtimeId": gatewayStringSchema("Runtime id."), "runtimeKind": map[string]any{"type": "string", "enum": []any{"endpoint", "gateway", "nas"}},
		"deviceId": gatewayStringSchema("Device id bound to the runtime."), "subjectId": gatewayStringSchema("User or service subject id."),
		"ttlSeconds": map[string]any{"type": "integer", "minimum": 60, "maximum": 600, "default": 600},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkEnrollmentListInputSchema() map[string]any {
	schema := gatewayObjectSchema(nil, map[string]any{"limit": map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 100}})
	schema["additionalProperties"] = false
	return schema
}

func networkEnrollmentSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"id", "challengeId", "runtimeId", "runtimeKind", "deviceId", "subjectId", "status", "expiresAt", "createdBy", "createdAt"}, map[string]any{
		"id": gatewayStringSchema("Enrollment id."), "challengeId": gatewayStringSchema("Public challenge id."),
		"runtimeId": gatewayStringSchema("Runtime id."), "runtimeKind": map[string]any{"type": "string", "enum": []any{"endpoint", "gateway", "nas"}},
		"deviceId": gatewayStringSchema("Bound device id."), "subjectId": gatewayStringSchema("Bound subject id."),
		"status":    map[string]any{"type": "string", "enum": []any{"pending", "consumed", "revoked"}},
		"expiresAt": gatewayDateTimeSchema(), "createdBy": gatewayStringSchema("Creating principal id."), "createdAt": gatewayDateTimeSchema(),
		"consumedAt": gatewayDateTimeSchema(), "revokedAt": gatewayDateTimeSchema(),
	})
	schema["additionalProperties"] = false
	return schema
}

func networkEnrollmentSecretSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"enrollment", "token"}, map[string]any{
		"enrollment": networkEnrollmentSchema(),
		"token":      map[string]any{"type": "string", "minLength": 32, "maxLength": 512, "description": "One-time enrollment token."},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkEnrollmentListSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"items"}, map[string]any{
		"items": map[string]any{"type": "array", "maxItems": 200, "items": networkEnrollmentSchema()},
	})
	schema["additionalProperties"] = false
	return schema
}

func gatewayDateTimeSchema() map[string]any {
	return map[string]any{"type": "string", "format": "date-time"}
}

func networkPolicyPreviewOutputSchema() map[string]any {
	return gatewayObjectSchema([]string{"decision", "path", "policyVersion", "protected", "networkLeaseRequired", "resourceLeaseRequired", "reasons"}, map[string]any{
		"decision":              map[string]any{"type": "string", "enum": []any{"allow", "deny"}},
		"path":                  map[string]any{"type": "string", "enum": []any{"site_direct", "wireguard", "wireguard_ztna", "access_proxy", "deny"}},
		"networkProfile":        gatewayStringSchema("Selected route and enforcement profile."),
		"policyVersion":         gatewayIntegerSchema("Policy version used for the decision."),
		"protected":             gatewayBooleanSchema("Whether the target is in the protected set."),
		"networkLeaseRequired":  gatewayBooleanSchema("Whether a network lease is required."),
		"resourceLeaseRequired": gatewayBooleanSchema("Whether a resource lease is required."),
		"reasons":               gatewayStringArraySchema("Stable decision reasons."),
	})
}

func networkConflictInputSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"runtimeRanges"}, map[string]any{
		"runtimeRanges": map[string]any{"type": "array", "maxItems": 128, "items": networkConflictRangeSchema()},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkConflictOutputSchema() map[string]any {
	conflict := gatewayObjectSchema([]string{"left", "right", "reason"}, map[string]any{
		"left": networkConflictRangeSchema(), "right": networkConflictRangeSchema(),
		"reason": map[string]any{"type": "string", "enum": []any{"duplicate", "overlap"}},
	})
	conflict["additionalProperties"] = false
	result := gatewayObjectSchema([]string{"valid", "rangesAnalyzed", "conflicts", "warnings"}, map[string]any{
		"valid":          gatewayBooleanSchema("Whether all configured ranges are disjoint."),
		"rangesAnalyzed": gatewayIntegerSchema("Number of persisted and submitted ranges analyzed."),
		"conflicts":      map[string]any{"type": "array", "maxItems": 10000, "items": conflict},
		"warnings":       gatewayStringArraySchema("Non-conflicting configuration warnings."),
	})
	result["additionalProperties"] = false
	return result
}

func networkGatewayInputSchema(update bool) map[string]any {
	required := []string{"runtimeId", "siteId", "name", "administrativeStatus", "publicEndpointHost", "publicEndpointPort", "overlayCidr", "routingMode", "mtu", "persistentKeepaliveSeconds", "dnsServers"}
	properties := map[string]any{
		"runtimeId": gatewayStringSchema("Gateway runtime id."), "siteId": gatewayStringSchema("Owning site id."),
		"name": gatewayStringSchema("Gateway name."), "administrativeStatus": map[string]any{"type": "string", "enum": []any{"active", "disabled"}},
		"publicEndpointHost":         gatewayStringSchema("Public WireGuard endpoint host."),
		"publicEndpointPort":         map[string]any{"type": "integer", "minimum": 1, "maximum": 65535},
		"overlayCidr":                gatewayStringSchema("Canonical IPv4 split-tunnel overlay CIDR from /16 through /30."),
		"routingMode":                map[string]any{"type": "string", "enum": []any{"routed", "snat"}},
		"mtu":                        map[string]any{"type": "integer", "minimum": 1280, "maximum": 1500},
		"persistentKeepaliveSeconds": map[string]any{"type": "integer", "minimum": 0, "maximum": 300},
		"dnsServers":                 map[string]any{"type": "array", "maxItems": 8, "uniqueItems": true, "items": map[string]any{"type": "string"}},
	}
	if update {
		required = append([]string{"gatewayId"}, required...)
		properties["gatewayId"] = gatewayStringSchema("Gateway id to update.")
	}
	schema := gatewayObjectSchema(required, properties)
	schema["additionalProperties"] = false
	return schema
}

func networkGatewaySchema() map[string]any {
	schema := networkGatewayInputSchema(false)
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		panic("gateway object schema has invalid properties")
	}
	properties["id"] = gatewayStringSchema("Gateway id.")
	properties["status"] = map[string]any{"type": "string", "enum": []any{"offline", "online", "degraded"}}
	properties["wireguardPublicKey"] = gatewayStringSchema("Active gateway WireGuard public key, when enrolled.")
	properties["version"] = gatewayStringSchema("Observed runtime version.")
	properties["capabilities"] = gatewayStringArraySchema("Observed runtime capabilities.")
	properties["policyVersion"] = gatewayIntegerSchema("Observed policy version.")
	properties["appliedAt"] = gatewayDateTimeSchema()
	properties["lastHeartbeatAt"] = gatewayDateTimeSchema()
	properties["createdAt"] = gatewayDateTimeSchema()
	properties["updatedAt"] = gatewayDateTimeSchema()
	required, ok := schema["required"].([]any)
	if !ok {
		panic("gateway object schema has invalid required fields")
	}
	schema["required"] = append([]any{"id", "status", "createdAt", "updatedAt"}, required...)
	return schema
}

func networkConflictRangeSchema() map[string]any {
	rangeSchema := gatewayObjectSchema([]string{"sourceType", "name", "cidr"}, map[string]any{
		"sourceType": map[string]any{"type": "string", "enum": []any{"network_space", "network_resource", "lan", "wireguard_overlay", "container", "kubernetes_pod", "kubernetes_service", "rfc6598", "mihomo_fake_ip"}},
		"sourceId":   gatewayStringSchema("Optional source resource id."),
		"name":       gatewayStringSchema("Human-readable range name."),
		"cidr":       gatewayStringSchema("Canonical IPv4 CIDR."),
	})
	rangeSchema["additionalProperties"] = false
	return rangeSchema
}

func networkSessionFilterSchema() map[string]any {
	schema := gatewayObjectSchema(nil, map[string]any{
		"siteId": gatewayStringSchema("Optional site id."), "runtimeId": gatewayStringSchema("Optional NAS runtime id."),
		"subjectId": gatewayStringSchema("Optional subject id."), "deviceId": gatewayStringSchema("Optional endpoint device id."),
		"status": map[string]any{"type": "string", "enum": []any{"active", "expired", "pending", "quarantine", "restricted", "revoked"}},
		"limit":  map[string]any{"type": "integer", "minimum": 1, "maximum": 200, "default": 100},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkSessionActionInputSchema(execute bool) map[string]any {
	required := []string{"sessionId", "action", "targetAccessProfile", "reasonCode"}
	properties := map[string]any{
		"sessionId":           gatewayStringSchema("Network session id."),
		"action":              map[string]any{"type": "string", "enum": []any{"coa", "disconnect"}},
		"targetAccessProfile": networkAccessProfileSchema(),
		"reasonCode":          map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
	}
	if execute {
		required = append(required, "planHash")
		properties["planHash"] = map[string]any{"type": "string", "pattern": "^sha256:[a-f0-9]{64}$"}
	}
	schema := gatewayObjectSchema(required, properties)
	schema["additionalProperties"] = false
	return schema
}

func networkSessionListSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"items"}, map[string]any{"items": map[string]any{"type": "array", "maxItems": 200, "items": networkSessionSchema()}})
	schema["additionalProperties"] = false
	return schema
}

func networkSessionSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"id", "subjectId", "deviceId", "mode", "path", "accessProfile", "status", "policyVersion", "networkLeaseIds", "resourceLeaseIds", "startedAt", "expiresAt"}, map[string]any{
		"id": gatewayStringSchema("Network session id."), "subjectId": gatewayStringSchema("Subject id."), "deviceId": gatewayStringSchema("Device id."),
		"siteId": gatewayStringSchema("Site id."), "gatewayId": gatewayStringSchema("Gateway id."), "nasId": gatewayStringSchema("NAS id."),
		"mode":          map[string]any{"type": "string", "enum": []any{"internal_direct", "internal_ztna", "external_vpn", "external_vpn_ztna", "external_direct_ztna"}},
		"path":          map[string]any{"type": "string", "enum": []any{"site_direct", "wireguard", "wireguard_ztna", "access_proxy", "deny"}},
		"accessProfile": networkAccessProfileSchema(),
		"status":        map[string]any{"type": "string", "enum": []any{"active", "expired", "pending", "quarantine", "restricted", "revoked"}},
		"policyVersion": gatewayIntegerSchema("Policy version."), "networkLeaseIds": gatewayStringArraySchema("Network lease ids."), "resourceLeaseIds": gatewayStringArraySchema("Resource lease ids."),
		"reasonCode": gatewayStringSchema("Decision reason."), "startedAt": gatewayDateTimeSchema(), "expiresAt": gatewayDateTimeSchema(),
	})
	schema["additionalProperties"] = false
	return schema
}

func networkSessionActionPlanSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"sessionId", "runtimeId", "nasId", "requestedAction", "effectiveAction", "currentAccessProfile", "targetAccessProfile", "reasonCode", "willDisconnect", "commandExpiresAt", "planHash"}, map[string]any{
		"sessionId": gatewayStringSchema("Session id."), "runtimeId": gatewayStringSchema("NAS runtime id."), "nasId": gatewayStringSchema("NAS id."),
		"requestedAction": map[string]any{"type": "string", "enum": []any{"coa", "disconnect"}}, "effectiveAction": map[string]any{"type": "string", "enum": []any{"coa", "disconnect"}},
		"currentAccessProfile": networkAccessProfileSchema(), "targetAccessProfile": networkAccessProfileSchema(),
		"reasonCode": gatewayStringSchema("Action reason."), "willDisconnect": gatewayBooleanSchema("Whether enforcement disconnects the session."),
		"commandExpiresAt": gatewayDateTimeSchema(), "planHash": map[string]any{"type": "string", "pattern": "^sha256:[a-f0-9]{64}$"},
	})
	schema["additionalProperties"] = false
	return schema
}

func networkSessionCommandSchema() map[string]any {
	schema := gatewayObjectSchema([]string{"id", "sessionId", "runtimeId", "nasId", "action", "targetAccessProfile", "policyVersion", "status", "reasonCode", "effectiveAt", "expiresAt", "createdAt"}, map[string]any{
		"id": gatewayStringSchema("Command id."), "sessionId": gatewayStringSchema("Session id."), "runtimeId": gatewayStringSchema("NAS runtime id."), "nasId": gatewayStringSchema("NAS id."),
		"action": map[string]any{"type": "string", "enum": []any{"coa", "disconnect"}}, "targetAccessProfile": networkAccessProfileSchema(),
		"policyVersion": gatewayIntegerSchema("Policy version."), "status": map[string]any{"type": "string", "enum": []any{"pending", "delivered", "applied", "rejected", "unsupported", "timed-out", "expired"}},
		"reasonCode": gatewayStringSchema("Action or result reason."), "effectiveAt": gatewayDateTimeSchema(), "expiresAt": gatewayDateTimeSchema(), "completedAt": gatewayDateTimeSchema(), "createdAt": gatewayDateTimeSchema(),
	})
	schema["additionalProperties"] = false
	return schema
}

func networkAccessProfileSchema() map[string]any {
	return map[string]any{"type": "string", "enum": []any{"onboarding", "full", "restricted", "quarantine", "deny"}}
}
