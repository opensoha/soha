package networkaccess

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	runtimeIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	sha256Pattern            = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

func validateDeviceInput(input domainnetworkaccess.DeviceInput) error {
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if input.SiteID != "" {
		if err := requiredText("siteId", input.SiteID, 128); err != nil {
			return err
		}
	}
	switch input.Status {
	case domainnetworkaccess.DeviceStatusActive, domainnetworkaccess.DeviceStatusPending, domainnetworkaccess.DeviceStatusQuarantined, domainnetworkaccess.DeviceStatusRevoked:
	default:
		return invalid("status is invalid")
	}
	if input.PostureStatus != "" && !oneOf(input.PostureStatus, domainnetworkaccess.PostureUnknown, domainnetworkaccess.PostureCompliant, domainnetworkaccess.PostureNoncompliant) {
		return invalid("postureStatus is invalid")
	}
	if input.DeviceType != "" && !validDeviceType(input.DeviceType) {
		return invalid("deviceType is invalid")
	}
	if input.OwnershipType != "" && !oneOf(input.OwnershipType, domainnetworkaccess.DeviceOwnershipCompany, domainnetworkaccess.DeviceOwnershipPersonal, domainnetworkaccess.DeviceOwnershipTemporary, domainnetworkaccess.DeviceOwnershipUnassigned) {
		return invalid("ownershipType is invalid")
	}
	return nil
}

func validateDeviceRegistration(id string, input domainnetworkaccess.DeviceRegistrationInput) error {
	if !runtimeIdentifierPattern.MatchString(id) {
		return invalid("deviceId is invalid")
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if err := requiredText("platform", input.Platform, 64); err != nil {
		return err
	}
	if input.Hostname != "" {
		if err := requiredText("hostname", input.Hostname, 253); err != nil {
			return err
		}
	}
	if !validDeviceType(input.DeviceType) {
		return invalid("deviceType is invalid")
	}
	return validateDeviceReportedFacts(input.ReportedFacts)
}

func validDeviceType(value string) bool {
	return oneOf(value, domainnetworkaccess.DeviceTypeDesktop, domainnetworkaccess.DeviceTypeLaptop, domainnetworkaccess.DeviceTypeServer, domainnetworkaccess.DeviceTypeMobile, domainnetworkaccess.DeviceTypeTablet, domainnetworkaccess.DeviceTypeVirtual, domainnetworkaccess.DeviceTypeUnknown)
}

func validateDeviceReportedFacts(facts *domainnetworkaccess.DeviceReportedFacts) error {
	if facts == nil {
		return nil
	}
	for name, value := range map[string]string{"architecture": facts.Architecture, "agentVersion": facts.AgentVersion} {
		if err := requiredText(name, value, 64); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{"osName": facts.OSName, "osVersion": facts.OSVersion, "osBuild": facts.OSBuild, "manufacturer": facts.Manufacturer} {
		if err := optionalText(name, value, 128); err != nil {
			return err
		}
	}
	for name, value := range map[string]string{"model": facts.Model, "serialNumber": facts.SerialNumber} {
		if err := optionalText(name, value, 200); err != nil {
			return err
		}
	}
	if facts.CollectedAt.IsZero() || len(facts.NetworkInterfaces) > 64 {
		return invalid("reportedFacts collection time or network interfaces are invalid")
	}
	for _, item := range facts.NetworkInterfaces {
		if err := requiredText("network interface name", item.Name, 128); err != nil {
			return err
		}
		if err := optionalText("network interface display name", item.DisplayName, 200); err != nil {
			return err
		}
		if !oneOf(item.Kind, domainnetworkaccess.NetworkInterfaceKindPhysical, domainnetworkaccess.NetworkInterfaceKindVirtual, domainnetworkaccess.NetworkInterfaceKindLoopback, domainnetworkaccess.NetworkInterfaceKindUnknown) || !oneOf(item.Status, domainnetworkaccess.NetworkInterfaceStatusUp, domainnetworkaccess.NetworkInterfaceStatusDown, domainnetworkaccess.NetworkInterfaceStatusUnknown) {
			return invalid("network interface kind or status is invalid")
		}
		if item.MACAddress != "" {
			mac, err := net.ParseMAC(item.MACAddress)
			if err != nil || len(mac) != 6 {
				return invalid("network interface MAC address is invalid")
			}
		}
		if err := validateDeviceAddresses(item); err != nil {
			return err
		}
	}
	return nil
}

func validateDeviceAddresses(item domainnetworkaccess.DeviceNetworkInterface) error {
	for label, values := range map[string][]string{"IPv4": item.IPv4Addresses, "IPv6": item.IPv6Addresses, "DNS": item.DNSServers} {
		limit := 32
		if label == "DNS" {
			limit = 16
		}
		if len(values) > limit {
			return invalid("network interface " + label + " addresses exceed the limit")
		}
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			address, err := netip.ParseAddr(value)
			if err != nil || (label == "IPv4" && !address.Is4()) || (label == "IPv6" && !address.Is6()) {
				return invalid("network interface " + label + " address is invalid")
			}
			if _, exists := seen[value]; exists {
				return invalid("network interface " + label + " addresses must be unique")
			}
			seen[value] = struct{}{}
		}
	}
	return nil
}

func validateSiteInput(input domainnetworkaccess.SiteInput) error {
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if utf8.RuneCountInString(input.Description) > 2000 || utf8.RuneCountInString(input.Location) > 512 {
		return invalid("site description or location is too long")
	}
	if input.Status != domainnetworkaccess.StatusActive && input.Status != domainnetworkaccess.StatusDisabled {
		return invalid("status is invalid")
	}
	return nil
}

func validateSpaceInput(input domainnetworkaccess.SpaceInput) error {
	if err := requiredText("siteId", input.SiteID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if input.Status != domainnetworkaccess.StatusActive && input.Status != domainnetworkaccess.StatusDisabled {
		return invalid("status is invalid")
	}
	return validateIPv4CIDRs("cidrs", input.CIDRs, false)
}

func validateIPv4CIDRs(name string, values []string, allowEmpty bool) error {
	if (!allowEmpty && len(values) == 0) || len(values) > 64 {
		return invalid("cidrs must contain between 1 and 64 entries")
	}
	seen := make(map[string]struct{}, len(values))
	prefixes := make([]netip.Prefix, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
			return invalid(name + " must contain canonical non-default IPv4 prefixes")
		}
		if _, exists := seen[value]; exists {
			return invalid(name + " must be unique")
		}
		seen[value] = struct{}{}
		prefixes = append(prefixes, prefix)
	}
	// ponytail: 64 CIDRs cap this pairwise scan; replace with a prefix trie only if that ceiling grows.
	for left := range prefixes {
		for right := left + 1; right < len(prefixes); right++ {
			if prefixes[left].Overlaps(prefixes[right]) {
				return invalid(name + " must not overlap")
			}
		}
	}
	return nil
}

func validateResourceInput(input domainnetworkaccess.ResourceInput) error {
	if err := requiredText("spaceId", input.SpaceID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if err := requiredText("target", input.Target, 512); err != nil {
		return err
	}
	if err := validateResourceTarget(input.Kind, strings.TrimSpace(input.Target)); err != nil {
		return err
	}
	if err := validateResourcePorts(input.Protocol, input.Ports); err != nil {
		return err
	}
	return validateResourcePath(input.Protected, input.PathMode)
}

func validateResourceTarget(kind, target string) error {
	switch kind {
	case "cidr":
		prefix, err := netip.ParsePrefix(target)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return invalid("CIDR target must be a canonical IPv4 prefix")
		}
	case "ip":
		address, err := netip.ParseAddr(target)
		if err != nil || !address.Is4() {
			return invalid("IP target must be an IPv4 address")
		}
	case "fqdn":
		if !validFQDN(target) {
			return invalid("FQDN target is invalid")
		}
	default:
		return invalid("kind is invalid")
	}
	return nil
}

func validateResourcePorts(protocol string, ports []int) error {
	if len(ports) > 64 {
		return invalid("ports cannot contain more than 64 entries")
	}
	seenPorts := make(map[int]struct{}, len(ports))
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return invalid("ports must be between 1 and 65535")
		}
		if _, exists := seenPorts[port]; exists {
			return invalid("ports must be unique")
		}
		seenPorts[port] = struct{}{}
	}
	switch protocol {
	case "tcp", "udp":
		if len(ports) == 0 {
			return invalid("tcp and udp resources require at least one port")
		}
	case "any", "icmp":
		if len(ports) != 0 {
			return invalid("any and icmp resources cannot declare ports")
		}
	default:
		return invalid("protocol is invalid")
	}
	return nil
}

func validateResourcePath(protected bool, pathMode string) error {
	switch pathMode {
	case domainnetworkaccess.PathAutomatic, domainnetworkaccess.PathSiteDirect, domainnetworkaccess.PathAccessProxy, domainnetworkaccess.PathWireGuard, domainnetworkaccess.PathWireGuardZTNA:
	default:
		return invalid("pathMode is invalid")
	}
	if protected && (pathMode == domainnetworkaccess.PathSiteDirect || pathMode == domainnetworkaccess.PathWireGuard) {
		return invalid("protected resources cannot use a broad network path")
	}
	return nil
}

func validateGatewayInput(input domainnetworkaccess.GatewayInput) error {
	if !runtimeIdentifierPattern.MatchString(input.RuntimeID) {
		return invalid("runtimeId is invalid")
	}
	if err := requiredText("siteId", input.SiteID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if input.AdministrativeStatus != domainnetworkaccess.StatusActive && input.AdministrativeStatus != domainnetworkaccess.StatusDisabled {
		return invalid("administrativeStatus is invalid")
	}
	if err := validateGatewayNetwork(input); err != nil {
		return err
	}
	if input.HubGatewayID != "" && !runtimeIdentifierPattern.MatchString(input.HubGatewayID) {
		return invalid("hubGatewayId is invalid")
	}
	if input.HubGatewayID != "" && input.RoutingMode != domainnetworkaccess.GatewayRoutingRouted {
		return invalid("a spoke gateway must use routed mode")
	}
	if err := validateIPv4CIDRs("advertisedCidrs", input.AdvertisedCIDRs, true); err != nil {
		return err
	}
	if err := validateGatewaySelection(input); err != nil {
		return err
	}
	return validateGatewayDNS(input.DNSServers)
}

func validateGatewayNetwork(input domainnetworkaccess.GatewayInput) error {
	if address, err := netip.ParseAddr(input.PublicEndpointHost); (err != nil || !address.Is4()) && !validFQDN(input.PublicEndpointHost) {
		return invalid("publicEndpointHost is invalid")
	}
	if input.PublicEndpointPort < 1 || input.PublicEndpointPort > 65535 {
		return invalid("publicEndpointPort must be between 1 and 65535")
	}
	prefix, err := netip.ParsePrefix(input.OverlayCIDR)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() < 16 || prefix.Bits() > 30 {
		return invalid("overlayCidr must be a canonical IPv4 /16 through /30 prefix")
	}
	if input.RoutingMode != domainnetworkaccess.GatewayRoutingRouted && input.RoutingMode != domainnetworkaccess.GatewayRoutingSNAT {
		return invalid("routingMode is invalid")
	}
	if input.MTU < 1280 || input.MTU > 1500 {
		return invalid("mtu must be between 1280 and 1500")
	}
	if input.PersistentKeepaliveSeconds < 0 || input.PersistentKeepaliveSeconds > 300 {
		return invalid("persistentKeepaliveSeconds must be between 0 and 300")
	}
	return nil
}

func validateGatewayDNS(servers []string) error {
	if len(servers) > 8 {
		return invalid("dnsServers cannot contain more than 8 entries")
	}
	seen := make(map[string]struct{}, len(servers))
	for _, value := range servers {
		address, err := netip.ParseAddr(value)
		if err != nil || !address.Is4() {
			return invalid("dnsServers must contain IPv4 addresses")
		}
		if _, exists := seen[value]; exists {
			return invalid("dnsServers must be unique")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func validateMihomoProfileInput(input domainnetworkaccess.MihomoProfileInput) error {
	if err := requiredText("deviceId", input.DeviceID, 128); err != nil {
		return err
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if !oneOf(input.Mode, domainnetworkaccess.MihomoModeManagedFollow, domainnetworkaccess.MihomoModeAppSubscription) {
		return invalid("mode is invalid")
	}
	if !oneOf(input.Status, domainnetworkaccess.StatusActive, domainnetworkaccess.StatusDisabled) {
		return invalid("status is invalid")
	}
	if input.SubscriptionURL != nil {
		if err := validateMihomoSubscriptionURL(*input.SubscriptionURL); err != nil {
			return err
		}
	}
	if input.ManualNode != nil {
		if err := validateMihomoManualNode(*input.ManualNode); err != nil {
			return err
		}
	}
	if input.MixedPort < 1 || input.MixedPort > 65535 || input.ControllerPort < 1 || input.ControllerPort > 65535 {
		return invalid("mixedPort and controllerPort must be between 1 and 65535")
	}
	if input.MixedPort == input.ControllerPort {
		return invalid("mixedPort and controllerPort must be different")
	}
	if err := requiredText("selectorGroup", input.SelectorGroup, 128); err != nil {
		return err
	}
	if err := validateMihomoMode(input); err != nil {
		return err
	}
	fakeIP, err := validateMihomoDNS(input.DNSMode, input.FakeIPRange)
	if err != nil {
		return err
	}
	if err := validateMihomoBypassCIDRs(input.BypassCIDRs, fakeIP); err != nil {
		return err
	}
	return validateMihomoBypassHosts(input.BypassHosts)
}

func validateMihomoMode(input domainnetworkaccess.MihomoProfileInput) error {
	switch input.Mode {
	case domainnetworkaccess.MihomoModeManagedFollow:
		if !input.FailClosed {
			return invalid("managed_follow requires failClosed")
		}
		switch input.SourceType {
		case domainnetworkaccess.MihomoSourceManagedSubscription:
			if input.ManualNode != nil {
				return invalid("managed_subscription cannot contain a manual node")
			}
			if err := requiredText("selectedProxy", input.SelectedProxy, 128); err != nil {
				return err
			}
		case domainnetworkaccess.MihomoSourceManualNode:
			if input.SubscriptionURL != nil || input.SelectedProxy != "" {
				return invalid("manual_node cannot contain a subscription or selected proxy")
			}
		default:
			return invalid("sourceType is invalid")
		}
	case domainnetworkaccess.MihomoModeAppSubscription:
		if input.SourceType != "" || input.SubscriptionURL != nil || input.ManualNode != nil || input.SelectedProxy != "" {
			return invalid("app_subscription cannot contain managed source state")
		}
	}
	return nil
}

func validateMihomoManualNode(node domainnetworkaccess.MihomoManualNode) error {
	if !oneOf(node.Protocol, domainnetworkaccess.MihomoManualProtocolHTTP, domainnetworkaccess.MihomoManualProtocolHTTPS, domainnetworkaccess.MihomoManualProtocolSOCKS5) {
		return invalid("manualNode protocol is invalid")
	}
	if err := requiredText("manualNode server", node.Server, 253); err != nil {
		return err
	}
	address, addressErr := netip.ParseAddr(node.Server)
	if (addressErr != nil || !address.IsValid() || address.Zone() != "") && !validFQDN(node.Server) {
		return invalid("manualNode server is invalid")
	}
	if node.Port < 1 || node.Port > 65535 {
		return invalid("manualNode port must be between 1 and 65535")
	}
	if (node.Username == "") != (node.Password == "") {
		return invalid("manualNode username and password must be provided together")
	}
	if node.Username != "" {
		if err := requiredText("manualNode username", node.Username, 256); err != nil {
			return err
		}
		if count := utf8.RuneCountInString(node.Password); count < 1 || count > 4096 {
			return invalid("manualNode password is invalid")
		}
	}
	return nil
}

func ValidateMihomoManualNode(node domainnetworkaccess.MihomoManualNode) error {
	return validateMihomoManualNode(node)
}

func validateMihomoDNS(mode, fakeIPRange string) (netip.Prefix, error) {
	var fakeIP netip.Prefix
	switch mode {
	case domainnetworkaccess.MihomoDNSDisabled:
		if fakeIPRange != "" {
			return netip.Prefix{}, invalid("fakeIpRange requires fake_ip DNS mode")
		}
	case domainnetworkaccess.MihomoDNSFakeIP:
		var err error
		fakeIP, err = parseCanonicalIPv4Prefix(fakeIPRange)
		if err != nil || fakeIP.Bits() == 0 {
			return netip.Prefix{}, invalid("fakeIpRange must be a canonical IPv4 prefix other than the default route")
		}
	default:
		return netip.Prefix{}, invalid("dnsMode is invalid")
	}
	return fakeIP, nil
}

func validateMihomoBypassCIDRs(cidrs []string, fakeIP netip.Prefix) error {
	if len(cidrs) > 256 {
		return invalid("bypassCidrs cannot contain more than 256 entries")
	}
	seenCIDRs := make(map[string]struct{}, len(cidrs))
	for _, value := range cidrs {
		prefix, err := parseCanonicalIPv4Prefix(value)
		if err != nil || prefix.Bits() == 0 {
			return invalid("bypassCidrs must contain canonical IPv4 prefixes other than the default route")
		}
		if _, exists := seenCIDRs[value]; exists {
			return invalid("bypassCidrs must be unique")
		}
		seenCIDRs[value] = struct{}{}
		if fakeIP.IsValid() && fakeIP.Overlaps(prefix) {
			return invalid("fakeIpRange cannot overlap bypassCidrs")
		}
	}
	return nil
}

func validateMihomoBypassHosts(hosts []string) error {
	if len(hosts) > 128 {
		return invalid("bypassHosts cannot contain more than 128 entries")
	}
	seenHosts := make(map[string]struct{}, len(hosts))
	for _, host := range hosts {
		address, err := netip.ParseAddr(host)
		if (err != nil || !address.Is4()) && !validFQDN(host) {
			return invalid("bypassHosts must contain IPv4 addresses or FQDNs")
		}
		if _, exists := seenHosts[host]; exists {
			return invalid("bypassHosts must be unique")
		}
		seenHosts[host] = struct{}{}
	}
	return nil
}

func validateMihomoSubscriptionURL(value string) error {
	if err := requiredText("subscriptionUrl", value, 4096); err != nil {
		return err
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" || parsed.Opaque != "" {
		return invalid("subscriptionUrl must be an absolute HTTPS URL without user info or fragment")
	}
	hostname := parsed.Hostname()
	address, addressErr := netip.ParseAddr(hostname)
	if (addressErr != nil || !address.IsValid()) && !validFQDN(hostname) {
		return invalid("subscriptionUrl host is invalid")
	}
	return nil
}

func ValidateMihomoSubscriptionURL(value string) error {
	return validateMihomoSubscriptionURL(value)
}

func parseCanonicalIPv4Prefix(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(value)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
		return netip.Prefix{}, invalid("value must be a canonical IPv4 prefix")
	}
	return prefix, nil
}

func validatePreviewInput(input PreviewInput) error {
	for name, value := range map[string]string{
		"subjectUserId": input.SubjectUserID,
		"deviceId":      input.DeviceID,
		"resourceId":    input.ResourceID,
	} {
		if err := requiredText(name, value, 128); err != nil {
			return err
		}
	}
	if input.SiteID != "" {
		if err := requiredText("siteId", input.SiteID, 128); err != nil {
			return err
		}
	}
	switch input.Mode {
	case domainnetworkaccess.ModeInternalDirect, domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPN, domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeExternalDirectZTNA:
		return nil
	default:
		return invalid("mode is invalid")
	}
}

func validateDeviceFilter(filter domainnetworkaccess.DeviceFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if err := optionalText("ownerUserId", filter.OwnerUserID, 128); err != nil {
		return err
	}
	if err := optionalText("siteId", filter.SiteID, 128); err != nil {
		return err
	}
	if filter.Status != "" {
		switch filter.Status {
		case domainnetworkaccess.DeviceStatusActive, domainnetworkaccess.DeviceStatusPending, domainnetworkaccess.DeviceStatusQuarantined, domainnetworkaccess.DeviceStatusRevoked:
		default:
			return invalid("status is invalid")
		}
	}
	return nil
}

func validateSiteFilter(filter domainnetworkaccess.SiteFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if filter.Status != "" && filter.Status != domainnetworkaccess.StatusActive && filter.Status != domainnetworkaccess.StatusDisabled {
		return invalid("status is invalid")
	}
	return nil
}

func validateSpaceFilter(filter domainnetworkaccess.SpaceFilter) error {
	if err := validateSiteFilter(domainnetworkaccess.SiteFilter{Search: filter.Search, Status: filter.Status, Limit: filter.Limit}); err != nil {
		return err
	}
	return optionalText("siteId", filter.SiteID, 128)
}

func validateResourceFilter(filter domainnetworkaccess.ResourceFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if err := optionalText("spaceId", filter.SpaceID, 128); err != nil {
		return err
	}
	if filter.Kind != "" && filter.Kind != "cidr" && filter.Kind != "ip" && filter.Kind != "fqdn" {
		return invalid("kind is invalid")
	}
	return nil
}

func validateGatewayFilter(filter domainnetworkaccess.GatewayFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if err := optionalText("siteId", filter.SiteID, 128); err != nil {
		return err
	}
	if filter.Status != "" && filter.Status != domainnetworkaccess.GatewayOnline && filter.Status != domainnetworkaccess.GatewayDegraded && filter.Status != domainnetworkaccess.GatewayOffline {
		return invalid("status is invalid")
	}
	return nil
}

func validateMihomoProfileFilter(filter domainnetworkaccess.MihomoProfileFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if err := optionalText("deviceId", filter.DeviceID, 128); err != nil {
		return err
	}
	if filter.Mode != "" && !oneOf(filter.Mode, domainnetworkaccess.MihomoModeManagedFollow, domainnetworkaccess.MihomoModeAppSubscription) {
		return invalid("mode is invalid")
	}
	if filter.Status != "" && !oneOf(filter.Status, domainnetworkaccess.StatusActive, domainnetworkaccess.StatusDisabled) {
		return invalid("status is invalid")
	}
	return nil
}

func validateSessionFilter(filter domainnetworkaccess.SessionFilter) error {
	if err := validateListFilter("", filter.Limit); err != nil {
		return err
	}
	for name, value := range map[string]string{"siteId": filter.SiteID, "runtimeId": filter.RuntimeID, "subjectId": filter.SubjectID, "deviceId": filter.DeviceID} {
		if value != "" && !runtimeIdentifierPattern.MatchString(value) {
			return invalid(name + " is invalid")
		}
	}
	if filter.Status != "" {
		switch filter.Status {
		case "pending", "active", "restricted", "quarantine", "revoked", "expired":
		default:
			return invalid("status is invalid")
		}
	}
	return nil
}

func validateSessionActionInput(input domainnetworkaccess.SessionActionInput, requirePlanHash bool) error {
	if input.Action != domainnetworkaccess.SessionActionCoA && input.Action != domainnetworkaccess.SessionActionDisconnect {
		return invalid("action is invalid")
	}
	switch input.TargetAccessProfile {
	case domainnetworkaccess.ProfileOnboarding, domainnetworkaccess.ProfileFull, domainnetworkaccess.ProfileRestricted, domainnetworkaccess.ProfileQuarantine, domainnetworkaccess.ProfileDeny:
	default:
		return invalid("targetAccessProfile is invalid")
	}
	if err := requiredText("reasonCode", input.ReasonCode, 128); err != nil || !runtimeIdentifierPattern.MatchString(input.ReasonCode) {
		return invalid("reasonCode is invalid")
	}
	if requirePlanHash && !sha256Pattern.MatchString(input.PlanHash) {
		return invalid("planHash is invalid")
	}
	return nil
}

func validateNASBindingInput(input domainnetworkaccess.NASBindingInput) error {
	for name, value := range map[string]string{"nasId": input.NASID, "runtimeId": input.RuntimeID, "siteId": input.SiteID} {
		if !runtimeIdentifierPattern.MatchString(value) {
			return invalid(name + " is invalid")
		}
	}
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if input.AccessMedium != "" && !oneOf(input.AccessMedium, domainnetworkaccess.AccessMediumWiFi, domainnetworkaccess.AccessMediumWired) {
		return invalid("accessMedium is invalid")
	}
	if input.DeviceType != "" && !oneOf(input.DeviceType, domainnetworkaccess.AccessDeviceTypeWirelessController, domainnetworkaccess.AccessDeviceTypeAccessPoint, domainnetworkaccess.AccessDeviceTypeSwitch, domainnetworkaccess.AccessDeviceTypeOther) {
		return invalid("deviceType is invalid")
	}
	if input.SSID != "" && (input.AccessMedium != domainnetworkaccess.AccessMediumWiFi || len(input.SSID) > 32) {
		return invalid("ssid is invalid")
	}
	if input.ManagementAddress != "" && len(input.ManagementAddress) > 255 {
		return invalid("managementAddress is invalid")
	}
	if input.Status != domainnetworkaccess.StatusActive && input.Status != domainnetworkaccess.StatusDisabled {
		return invalid("status is invalid")
	}
	return nil
}

func validateNASBindingFilter(filter domainnetworkaccess.NASBindingFilter) error {
	if err := validateListFilter("", filter.Limit); err != nil {
		return err
	}
	for name, value := range map[string]string{"siteId": filter.SiteID, "runtimeId": filter.RuntimeID} {
		if value != "" && !runtimeIdentifierPattern.MatchString(value) {
			return invalid(name + " is invalid")
		}
	}
	if filter.Status != "" && filter.Status != domainnetworkaccess.StatusActive && filter.Status != domainnetworkaccess.StatusDisabled {
		return invalid("status is invalid")
	}
	return nil
}

func validateSiteProfileBindingInput(input domainnetworkaccess.SiteProfileBindingInput) error {
	if !runtimeIdentifierPattern.MatchString(input.SiteID) {
		return invalid("siteId is invalid")
	}
	if !oneOf(input.AccessProfile, domainnetworkaccess.ProfileOnboarding, domainnetworkaccess.ProfileFull, domainnetworkaccess.ProfileRestricted, domainnetworkaccess.ProfileQuarantine, domainnetworkaccess.ProfileDeny) {
		return invalid("accessProfile is invalid")
	}
	if input.VLANID < 0 || input.VLANID > 4094 {
		return invalid("vlanId must be between 1 and 4094 when set")
	}
	if err := optionalText("filterId", input.FilterID, 128); err != nil {
		return err
	}
	if input.SessionTimeoutSeconds < 1 || input.SessionTimeoutSeconds > 86400 {
		return invalid("sessionTimeoutSeconds must be between 1 and 86400")
	}
	return nil
}

func validateSiteProfileBindingFilter(filter domainnetworkaccess.SiteProfileBindingFilter) error {
	if err := validateListFilter("", filter.Limit); err != nil {
		return err
	}
	if filter.SiteID != "" && !runtimeIdentifierPattern.MatchString(filter.SiteID) {
		return invalid("siteId is invalid")
	}
	if filter.AccessProfile != "" && !oneOf(filter.AccessProfile, domainnetworkaccess.ProfileOnboarding, domainnetworkaccess.ProfileFull, domainnetworkaccess.ProfileRestricted, domainnetworkaccess.ProfileQuarantine, domainnetworkaccess.ProfileDeny) {
		return invalid("accessProfile is invalid")
	}
	return nil
}

func validatePolicyInput(input domainnetworkaccess.PolicyInput) error {
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if input.Priority < 1 || input.Priority > 10000 {
		return invalid("priority must be between 1 and 10000")
	}
	if input.Effect != domainnetworkaccess.PolicyEffectAllow && input.Effect != domainnetworkaccess.PolicyEffectDeny {
		return invalid("effect is invalid")
	}
	if !oneOf(input.AccessProfile, domainnetworkaccess.ProfileOnboarding, domainnetworkaccess.ProfileFull, domainnetworkaccess.ProfileRestricted, domainnetworkaccess.ProfileQuarantine, domainnetworkaccess.ProfileDeny) {
		return invalid("accessProfile is invalid")
	}
	if input.Effect == domainnetworkaccess.PolicyEffectAllow && input.AccessProfile == domainnetworkaccess.ProfileDeny {
		return invalid("allow policy cannot use deny profile")
	}
	if input.Effect == domainnetworkaccess.PolicyEffectDeny && input.AccessProfile != domainnetworkaccess.ProfileDeny {
		return invalid("deny policy must use deny profile")
	}
	for name, values := range map[string][]string{
		"subjects.users": input.Subjects.Users,
		"subjects.teams": input.Subjects.Teams,
		"subjects.tags":  input.Subjects.Tags,
		"siteIds":        input.SiteIDs,
		"resourceIds":    input.ResourceIDs,
	} {
		limit := 128
		if name == "resourceIds" {
			limit = 256
		}
		if err := validateStringList(name, values, limit, nil); err != nil {
			return err
		}
	}
	if err := validateStringList("modes", input.Modes, 5, []string{domainnetworkaccess.ModeInternalDirect, domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPN, domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeExternalDirectZTNA}); err != nil {
		return err
	}
	if err := validateStringList("deviceStatuses", input.DeviceStatuses, 4, []string{domainnetworkaccess.DeviceStatusPending, domainnetworkaccess.DeviceStatusActive, domainnetworkaccess.DeviceStatusQuarantined, domainnetworkaccess.DeviceStatusRevoked}); err != nil {
		return err
	}
	return validateStringList("postureStatuses", input.PostureStatuses, 3, []string{domainnetworkaccess.PostureUnknown, domainnetworkaccess.PostureCompliant, domainnetworkaccess.PostureNoncompliant})
}

func validatePolicyFilter(filter domainnetworkaccess.PolicyFilter) error {
	if err := validateListFilter(filter.Search, filter.Limit); err != nil {
		return err
	}
	if filter.Effect != "" && filter.Effect != domainnetworkaccess.PolicyEffectAllow && filter.Effect != domainnetworkaccess.PolicyEffectDeny {
		return invalid("effect is invalid")
	}
	return nil
}

func validateConflictRanges(ranges []domainnetworkaccess.ConflictRange) error {
	if len(ranges) > 128 {
		return invalid("runtimeRanges cannot contain more than 128 entries")
	}
	for _, item := range ranges {
		if !oneOf(item.SourceType, domainnetworkaccess.ConflictSourceNetworkSpace, domainnetworkaccess.ConflictSourceNetworkResource, domainnetworkaccess.ConflictSourceLAN, domainnetworkaccess.ConflictSourceWireGuardOverlay, domainnetworkaccess.ConflictSourceContainer, domainnetworkaccess.ConflictSourceKubernetesPod, domainnetworkaccess.ConflictSourceKubernetesSvc, domainnetworkaccess.ConflictSourceReserved, domainnetworkaccess.ConflictSourceMihomoFakeIP) {
			return invalid("conflict range sourceType is invalid")
		}
		if err := requiredText("conflict range name", item.Name, 200); err != nil {
			return err
		}
		if err := optionalText("conflict range sourceId", item.SourceID, 128); err != nil {
			return err
		}
		prefix, err := netip.ParsePrefix(item.CIDR)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() {
			return invalid("conflict range CIDR must be a canonical IPv4 prefix")
		}
	}
	return nil
}

func validateStringList(name string, values []string, maxItems int, allowed []string) error {
	if len(values) > maxItems {
		return invalid(fmt.Sprintf("%s cannot contain more than %d entries", name, maxItems))
	}
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if err := requiredText(name, value, 128); err != nil {
			return err
		}
		if allowed != nil && !oneOf(value, allowed...) {
			return invalid(name + " contains an invalid value")
		}
		if _, exists := seen[value]; exists {
			return invalid(name + " must be unique")
		}
		seen[value] = struct{}{}
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func validateListFilter(search string, limit int) error {
	if utf8.RuneCountInString(search) > 200 {
		return invalid("search cannot exceed 200 characters")
	}
	if limit < 0 || limit > 200 {
		return invalid("limit must be between 1 and 200")
	}
	return nil
}

func optionalText(name, value string, maxLength int) error {
	if value == "" {
		return nil
	}
	return requiredText(name, value, maxLength)
}

func validFQDN(value string) bool {
	if value == "" || len(value) > 253 || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < '0' || character > '9') && (character < 'A' || character > 'Z') && (character < 'a' || character > 'z') {
				return false
			}
		}
	}
	return true
}

func requiredText(name, value string, maxLength int) error {
	length := utf8.RuneCountInString(strings.TrimSpace(value))
	if length == 0 || length > maxLength {
		return invalid(fmt.Sprintf("%s must contain between 1 and %d characters", name, maxLength))
	}
	return nil
}

func invalid(message string) error {
	return fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, message)
}
