package routes

import "strings"

func isNetworkSecurityReadOnlyPOST(path string) bool {
	switch path {
	case "/api/v1/network-access/policy/preview", "/api/v1/network-access/admission/preview",
		"/api/v1/network-access/conflicts/analyze", "/api/v1/network-access/sessions/:sessionID/actions/plan",
		"/api/v1/network-access/vpn/selection-preview",
		"/api/v1/network-access/endpoint-response-policies/:policyID/preview",
		"/api/v1/network-access/content-transfers/destinations/:destinationID/preview",
		"/api/v1/docker/projects/:id/content-dlp/preview":
		return true
	default:
		return false
	}
}

// These exact routes precede broader CRUD families so release/publish actions
// retain their owning service's permission instead of inheriting POST=create.
var networkSecurityMutations = map[string]struct{ kind, action, permission string }{
	"PUT /api/v1/network-access/workload-bindings/:bindingID":                                         {"NetworkWorkloadBinding", "update", "network_access.policy.update"},
	"POST /api/v1/network-access/sessions/:sessionID/vpn-revocation":                                  {"NetworkSession", "revoke", "network_access.vpn_sessions.revoke"},
	"POST /api/v1/network-access/sessions/:sessionID/vpn-revocation/release":                          {"NetworkSession", "release", "network_access.vpn_sessions.release"},
	"POST /api/v1/network-access/proxy-instances/:instanceID/configuration-history/:revision/restore": {"NetworkProxyInstance", "restore", "network_access.proxy_instances.update"},
	"PUT /api/v1/network-access/endpoint-devices/:deviceID/channel-controls":                          {"EndpointDevice", "update", "network_access.endpoint_controls.update"},
	"POST /api/v1/network-access/endpoint-devices/:deviceID/channel-controls/publish":                 {"EndpointDevice", "publish", "network_access.endpoint_controls.publish"},
	"POST /api/v1/network-access/endpoint-response-policies":                                          {"EndpointResponsePolicy", "create", "network_access.endpoint_response_policies.create"},
	"PUT /api/v1/network-access/endpoint-response-policies/:policyID":                                 {"EndpointResponsePolicy", "update", "network_access.endpoint_response_policies.update"},
	"DELETE /api/v1/network-access/endpoint-response-policies/:policyID":                              {"EndpointResponsePolicy", "delete", "network_access.endpoint_response_policies.delete"},
	"POST /api/v1/network-access/endpoint-response-policies/:policyID/publish":                        {"EndpointResponsePolicy", "publish", "network_access.endpoint_response_policies.publish"},
	"POST /api/v1/network-access/endpoint-response-policies/:policyID/rollback":                       {"EndpointResponsePolicy", "rollback", "network_access.endpoint_response_policies.publish"},
	"POST /api/v1/network-access/endpoint-response-events/:eventID/restore":                           {"EndpointResponseEvent", "restore", "network_access.endpoint_response_events.restore"},
	"POST /api/v1/network-access/endpoint-maintenance-windows":                                        {"EndpointMaintenance", "create", "network_access.endpoint_response_events.maintenance"},
	"DELETE /api/v1/network-access/endpoint-maintenance-windows/:windowID":                            {"EndpointMaintenance", "delete", "network_access.endpoint_response_events.maintenance"},
	"POST /api/v1/network-access/vpn/profiles":                                                        {"NetworkVPNProfile", "create", "network_access.vpn_profiles.create"},
	"PUT /api/v1/network-access/vpn/profiles/:id":                                                     {"NetworkVPNProfile", "update", "network_access.vpn_profiles.update"},
	"DELETE /api/v1/network-access/vpn/profiles/:id":                                                  {"NetworkVPNProfile", "delete", "network_access.vpn_profiles.delete"},
	"POST /api/v1/network-access/vpn/profiles/:id/publish":                                            {"NetworkVPNProfile", "publish", "network_access.vpn_profiles.publish"},
	"POST /api/v1/network-access/vpn/profiles/:id/rollback":                                           {"NetworkVPNProfile", "rollback", "network_access.vpn_profiles.publish"},
	"POST /api/v1/network-access/vpn/selection-policies":                                              {"NetworkVPNSelectionPolicy", "create", "network_access.vpn_selection_policies.create"},
	"PUT /api/v1/network-access/vpn/selection-policies/:id":                                           {"NetworkVPNSelectionPolicy", "update", "network_access.vpn_selection_policies.update"},
	"DELETE /api/v1/network-access/vpn/selection-policies/:id":                                        {"NetworkVPNSelectionPolicy", "delete", "network_access.vpn_selection_policies.delete"},
	"POST /api/v1/network-access/vpn/selection-policies/:id/publish":                                  {"NetworkVPNSelectionPolicy", "publish", "network_access.vpn_selection_policies.publish"},
	"POST /api/v1/network-access/vpn/selection-policies/:id/rollback":                                 {"NetworkVPNSelectionPolicy", "rollback", "network_access.vpn_selection_policies.publish"},
	"PUT /api/v1/network-access/content-transfers/destinations/:destinationID":                        {"ContentTransferDestination", "update", "network_access.content_destinations.update"},
	"POST /api/v1/network-access/content-transfers/destinations/:destinationID/publish":               {"ContentTransferDestination", "publish", "network_access.content_destinations.publish"},
	"POST /api/v1/network-access/content-transfers/uploads":                                           {"ContentTransfer", "create", "network_access.content_transfers.create"},
	"POST /api/v1/network-access/content-transfers/pastes":                                            {"ContentTransfer", "create", "network_access.content_transfers.create"},
	"POST /api/v1/network-access/content-transfers/files/:fileID/read":                                {"ContentTransfer", "download", "network_access.content_transfers.download"},
	"POST /api/v1/network-access/content-transfers/files/:fileID/copy":                                {"ContentTransfer", "copy", "network_access.content_transfers.create"},
	"POST /api/v1/network-access/content-transfers/files/:fileID/delete":                              {"ContentTransfer", "delete", "network_access.content_transfers.delete"},
	"POST /api/v1/network-access/content-transfers/records/:recordID/authorize":                       {"ContentTransfer", "authorize", "network_access.content_transfers.view"},
	"POST /api/v1/network-access/content-transfers/records/:recordID/appeal":                          {"ContentTransfer", "appeal", "network_access.content_transfers.view"},
	"POST /api/v1/network-access/content-transfers/records/:recordID/review":                          {"ContentTransfer", "review", "network_access.content_transfer_appeals.review"},
	"PUT /api/v1/docker/projects/:id/content-dlp":                                                     {"DockerProject", "update", "docker.dlp-policy.update"},
	"POST /api/v1/docker/projects/:id/content-dlp/publish":                                            {"DockerProject", "publish", "docker.dlp-policy.publish"},
	"POST /api/v1/docker/projects/:id/content-dlp/events/:eventID/authorizations":                     {"DockerProject", "authorize", "docker.services.view"},
	"POST /api/v1/docker/projects/:id/content-dlp/events/:eventID/appeal":                             {"DockerProject", "appeal", "docker.services.view"},
	"POST /api/v1/docker/projects/:id/content-dlp/events/:eventID/appeal/review":                      {"DockerProject", "review", "docker.dlp-appeals.review"},
	"POST /api/v1/docker/projects/:id/runtime/volume-file/read":                                       {"DockerProject", "read", "docker.services.view"},
}

func networkSecurityMutationSurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	if method == "POST" && path == "/api/v1/network-access/vpn/connection-intents" {
		// Self-service connection uses server profile assignments plus owned-device
		// and network policy checks, rather than management CRUD permissions.
		entry := nonPlatformMutationEntry("NetworkVPNConnectionIntent", "create", "published_vpn_profile_assignment", false)
		entry.DynamicPermission = true
		return entry, true
	}
	spec, ok := networkSecurityMutations[strings.ToUpper(method)+" "+path]
	if !ok {
		return nonPlatformMutationSecuritySurfaceEntry{}, false
	}
	return nonPlatformMutationEntry(spec.kind, spec.action, spec.permission, false), true
}
