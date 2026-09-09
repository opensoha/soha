package networkgateway

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

const (
	defaultInterfaceName = "soha0"
	firewallTable        = "soha_network_gateway"
)

var (
	linuxInterfacePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,14}$`)
	identifierPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
	endpointHostPattern   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	sha256Pattern         = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
)

type System interface {
	Baseline(context.Context, string) error
	Apply(context.Context, networkprotocol.ConfigurationDesired, wgtypes.Key) error
	Readback(context.Context, networkprotocol.ConfigurationDesired) (string, error)
	Disable(context.Context, string, wgtypes.Key) error
}

type ApplyOutcome struct {
	Status       string
	ReadbackHash string
	ReasonCode   string
}

type Executor struct {
	mu       sync.Mutex
	key      wgtypes.Key
	system   System
	egress   string
	previous *networkprotocol.ConfigurationDesired
}

func NewExecutor(key wgtypes.Key, system System, egressInterface string) (*Executor, error) {
	if system == nil || key == (wgtypes.Key{}) {
		return nil, fmt.Errorf("gateway executor key and system are required")
	}
	if egressInterface != "" && !linuxInterfacePattern.MatchString(egressInterface) {
		return nil, fmt.Errorf("gateway egress interface is invalid")
	}
	return &Executor{key: key, system: system, egress: egressInterface}, nil
}

func (e *Executor) Apply(ctx context.Context, desired networkprotocol.ConfigurationDesired, now time.Time) ApplyOutcome {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ValidateGatewayDesired(desired, e.key.PublicKey(), now, e.egress); err != nil {
		return failedOutcome("rejected", "invalid_configuration")
	}
	interfaceName := desiredInterfaceName(desired)
	if err := e.system.Baseline(ctx, interfaceName); err != nil {
		return failedOutcome("rejected", "firewall_baseline_failed")
	}
	if err := e.system.Apply(ctx, desired, e.key); err != nil {
		return e.recoverFailure(ctx, interfaceName, "system_apply_failed")
	}
	hash, err := e.system.Readback(ctx, desired)
	if err != nil || !sha256Pattern.MatchString(hash) {
		return e.recoverFailure(ctx, interfaceName, "readback_failed")
	}
	copy := desired
	e.previous = &copy
	return ApplyOutcome{Status: "applied", ReadbackHash: hash}
}

func (e *Executor) recoverFailure(ctx context.Context, interfaceName, reason string) ApplyOutcome {
	hadPrevious := e.previous != nil
	if e.rollback(ctx, interfaceName) != nil {
		return failedOutcome("rejected", "rollback_failed")
	}
	if !hadPrevious {
		return failedOutcome("rejected", reason)
	}
	return failedOutcome("rolled-back", reason)
}

func failedOutcome(status, reason string) ApplyOutcome {
	digest := sha256.Sum256([]byte("network-gateway:" + status + ":" + reason))
	return ApplyOutcome{Status: status, ReadbackHash: fmt.Sprintf("sha256:%x", digest), ReasonCode: reason}
}

func (e *Executor) Disable(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	interfaceName := defaultInterfaceName
	if e.previous != nil {
		interfaceName = desiredInterfaceName(*e.previous)
	}
	baselineErr := e.system.Baseline(ctx, interfaceName)
	disableErr := e.system.Disable(ctx, interfaceName, e.key.PublicKey())
	return errors.Join(baselineErr, disableErr)
}

func (e *Executor) rollback(ctx context.Context, interfaceName string) error {
	if err := e.system.Baseline(ctx, interfaceName); err != nil {
		_ = e.system.Disable(ctx, interfaceName, e.key.PublicKey())
		return err
	}
	if e.previous == nil {
		return e.system.Disable(ctx, interfaceName, e.key.PublicKey())
	}
	if err := e.system.Apply(ctx, *e.previous, e.key); err != nil {
		_ = e.system.Disable(ctx, interfaceName, e.key.PublicKey())
		return err
	}
	hash, err := e.system.Readback(ctx, *e.previous)
	if err != nil || !sha256Pattern.MatchString(hash) {
		_ = e.system.Disable(ctx, interfaceName, e.key.PublicKey())
		if err != nil {
			return err
		}
		return fmt.Errorf("rollback readback hash is invalid")
	}
	return nil
}

func desiredInterfaceName(desired networkprotocol.ConfigurationDesired) string {
	if desired.WireGuard == nil {
		return defaultInterfaceName
	}
	return desired.WireGuard.InterfaceName
}

func ValidateGatewayDesired(desired networkprotocol.ConfigurationDesired, publicKey wgtypes.Key, now time.Time, egressInterface string) error {
	if desired.ConfigurationVersion <= 0 || desired.PolicyVersion <= 0 || !desired.ValidUntil.After(now) {
		return fmt.Errorf("gateway configuration version or validity is invalid")
	}
	if desired.WireGuard == nil {
		if len(desired.NetworkLeases) != 0 || len(desired.ResourceLeases) != 0 || (desired.AccessProfile != "onboarding" && desired.AccessProfile != "deny") {
			return fmt.Errorf("configuration without WireGuard must be onboarding or deny without leases")
		}
		return nil
	}
	wg := desired.WireGuard
	if err := validateGatewayWireGuard(*wg, publicKey, egressInterface); err != nil {
		return err
	}
	peerSources, err := validateGatewayPeers(wg.Peers)
	if err != nil {
		return err
	}
	if err := validateGatewayRoutes(wg.Routes, peerSources.routes); err != nil {
		return err
	}
	if wg.RoutingMode == "snat" && len(peerSources.site) != 0 {
		return fmt.Errorf("site links require routed mode")
	}
	return validateGatewayFirewall(wg.FirewallRules, peerSources, desired.NetworkLeases, desired.ResourceLeases, desired.ValidUntil, now)
}

func validateGatewayWireGuard(wg networkprotocol.WireGuardConfiguration, publicKey wgtypes.Key, egressInterface string) error {
	if wg.Role != "gateway" || wg.InterfaceName != defaultInterfaceName || wg.PublicKey != publicKey.String() || wg.ListenPort < 1 || wg.ListenPort > 65535 || wg.MTU < 1280 || wg.MTU > 1500 || wg.FirewallDefault != "deny" {
		return fmt.Errorf("gateway WireGuard identity or bounds are invalid")
	}
	if wg.RoutingMode != "routed" && wg.RoutingMode != "snat" {
		return fmt.Errorf("gateway routing mode is invalid")
	}
	if wg.RoutingMode == "snat" && !linuxInterfacePattern.MatchString(egressInterface) {
		return fmt.Errorf("SNAT requires an explicit egress interface")
	}
	if len(wg.Addresses) != 1 || len(wg.Peers) > 1024 || len(wg.Routes) > 256 || len(wg.FirewallRules) > 4096 {
		return fmt.Errorf("gateway WireGuard collection bounds are invalid")
	}
	if _, err := canonicalIPv4Prefix(wg.Addresses[0], 32); err != nil {
		return fmt.Errorf("gateway address: %w", err)
	}
	return nil
}

func validateGatewayRoutes(routes []string, allowed map[string]struct{}) error {
	seen := make(map[string]struct{}, len(routes))
	for _, route := range routes {
		prefix, err := canonicalIPv4Prefix(route, -1)
		if err != nil || prefix.Bits() == 0 {
			return fmt.Errorf("gateway route is invalid")
		}
		if _, exists := seen[prefix.String()]; exists {
			return fmt.Errorf("gateway route is duplicate")
		}
		seen[prefix.String()] = struct{}{}
	}
	if len(seen) != len(allowed) {
		return fmt.Errorf("gateway routes do not match peer AllowedIPs")
	}
	for route := range seen {
		if _, exists := allowed[route]; !exists {
			return fmt.Errorf("gateway route has no peer")
		}
	}
	return nil
}

type gatewayPeerSources struct {
	endpoint map[string]struct{}
	site     map[string][]netip.Prefix
	routes   map[string]struct{}
}

func validateGatewayPeers(peers []networkprotocol.WireGuardPeer) (gatewayPeerSources, error) {
	peerSources := gatewayPeerSources{endpoint: map[string]struct{}{}, site: map[string][]netip.Prefix{}, routes: map[string]struct{}{}}
	peerKeys := make(map[wgtypes.Key]struct{}, len(peers))
	allPrefixes := []netip.Prefix{}
	for _, peer := range peers {
		key, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil || !identifierPattern.MatchString(peer.RuntimeID) || len(peer.AllowedIPs) == 0 || len(peer.AllowedIPs) > 128 {
			return gatewayPeerSources{}, fmt.Errorf("gateway peer is invalid")
		}
		if _, exists := peerKeys[key]; exists {
			return gatewayPeerSources{}, fmt.Errorf("gateway peer public key is duplicate")
		}
		peerKeys[key] = struct{}{}
		sitePeer, err := validateGatewayPeerKind(peer)
		if err != nil {
			return gatewayPeerSources{}, err
		}
		if sitePeer {
			if _, exists := peerSources.site[peer.RuntimeID]; exists {
				return gatewayPeerSources{}, fmt.Errorf("gateway site peer runtime is duplicate")
			}
		}
		prefixes := make([]netip.Prefix, 0, len(peer.AllowedIPs))
		for _, raw := range peer.AllowedIPs {
			prefix, err := canonicalIPv4Prefix(raw, -1)
			if err != nil || prefix.Bits() == 0 || (!sitePeer && prefix.Bits() != 32) {
				return gatewayPeerSources{}, fmt.Errorf("gateway peer AllowedIPs are invalid")
			}
			for _, existing := range allPrefixes {
				if prefix.Overlaps(existing) {
					return gatewayPeerSources{}, fmt.Errorf("gateway peer AllowedIPs overlap")
				}
			}
			allPrefixes = append(allPrefixes, prefix)
			prefixes = append(prefixes, prefix)
			peerSources.routes[prefix.String()] = struct{}{}
		}
		if sitePeer {
			peerSources.site[peer.RuntimeID] = prefixes
		} else {
			peerSources.endpoint[prefixes[0].String()] = struct{}{}
		}
	}
	return peerSources, nil
}

func validateGatewayPeerKind(peer networkprotocol.WireGuardPeer) (bool, error) {
	sitePeer := peer.EndpointHost != "" || peer.EndpointPort != 0
	if sitePeer {
		if peer.DeviceID != "" || !endpointHostPattern.MatchString(peer.EndpointHost) || peer.EndpointPort < 1 || peer.EndpointPort > 65535 || peer.PersistentKeepaliveSeconds < 0 || peer.PersistentKeepaliveSeconds > 300 {
			return false, fmt.Errorf("gateway site peer is invalid")
		}
	} else if !identifierPattern.MatchString(peer.DeviceID) || len(peer.AllowedIPs) != 1 || peer.PersistentKeepaliveSeconds != 0 {
		return false, fmt.Errorf("gateway endpoint peer is invalid")
	}
	return sitePeer, nil
}

func validateGatewayFirewall(rules []networkprotocol.WireGuardFirewallRule, peerSources gatewayPeerSources, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease, validUntil, now time.Time) error {
	seenRules := make(map[string]struct{}, len(rules))
	networkLeaseByID := make(map[string]networkprotocol.NetworkLease, len(networkLeases))
	resourceLeaseIDs := make(map[string]struct{}, len(resourceLeases))
	for _, lease := range networkLeases {
		networkLeaseByID[lease.ID] = lease
	}
	for _, lease := range resourceLeases {
		resourceLeaseIDs[lease.ID] = struct{}{}
	}
	for index, rule := range rules {
		destination, err := validateGatewayFirewallRule(rule, seenRules, peerSources, networkLeaseByID, resourceLeaseIDs, validUntil, now)
		if err != nil {
			return err
		}
		_, resourceAllow := resourceLeaseIDs[rule.LeaseID]
		if rule.Effect == "allow" && !resourceAllow && protectedDenyFollows(rules[index+1:], rule, destination) {
			return fmt.Errorf("gateway protected deny follows its covering allow")
		}
	}
	return nil
}

func validateGatewayFirewallRule(rule networkprotocol.WireGuardFirewallRule, seenRules map[string]struct{}, peerSources gatewayPeerSources, networkLeases map[string]networkprotocol.NetworkLease, resourceLeases map[string]struct{}, validUntil, now time.Time) (netip.Prefix, error) {
	if !identifierPattern.MatchString(rule.ID) {
		return netip.Prefix{}, fmt.Errorf("gateway firewall rule ID is invalid")
	}
	if _, exists := seenRules[rule.ID]; exists {
		return netip.Prefix{}, fmt.Errorf("gateway firewall rule ID is duplicate")
	}
	seenRules[rule.ID] = struct{}{}
	source, err := canonicalIPv4Prefix(rule.SourceCIDR, -1)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("gateway firewall source is invalid")
	}
	destination, err := canonicalIPv4Prefix(rule.DestinationCIDR, -1)
	if err != nil || destination.Bits() == 0 || !validFirewallEffect(rule.Effect) || !validFirewallProtocol(rule.Protocol) {
		return netip.Prefix{}, fmt.Errorf("gateway firewall rule is invalid")
	}
	if rule.Protocol == "any" && len(rule.Ports) != 0 {
		return netip.Prefix{}, fmt.Errorf("gateway any-protocol rule cannot specify ports")
	}
	for _, port := range rule.Ports {
		if port < 1 || port > 65535 {
			return netip.Prefix{}, fmt.Errorf("gateway firewall port is invalid")
		}
	}
	if err := validateGatewayFirewallSource(rule, source, destination, peerSources, validUntil, now); err != nil {
		return netip.Prefix{}, err
	}
	if err := validateGatewayFirewallLease(rule, destination, networkLeases, resourceLeases, validUntil, now); err != nil {
		return netip.Prefix{}, err
	}
	return destination, nil
}

func validateGatewayFirewallSource(rule networkprotocol.WireGuardFirewallRule, source, destination netip.Prefix, peerSources gatewayPeerSources, validUntil, now time.Time) error {
	if rule.LeaseID != "" && rule.SiteLinkRuntimeID != "" {
		return fmt.Errorf("gateway firewall rule has conflicting authorization bindings")
	}
	if rule.SiteLinkRuntimeID != "" {
		prefixes, exists := peerSources.site[rule.SiteLinkRuntimeID]
		if !exists || (rule.Direction != "from_wireguard" && rule.Direction != "to_wireguard") || !siteRuleCovered(prefixes, rule.Direction, source, destination) {
			return fmt.Errorf("gateway site-link firewall rule is invalid")
		}
		if rule.ExpiresAt == nil || rule.ExpiresAt.Before(validUntil) || !rule.ExpiresAt.After(now) {
			return fmt.Errorf("gateway site-link firewall rule is expired")
		}
	} else {
		if rule.Direction != "" || source.Bits() != 32 {
			return fmt.Errorf("gateway endpoint firewall rule is invalid")
		}
		if _, exists := peerSources.endpoint[source.String()]; !exists {
			return fmt.Errorf("gateway firewall source has no endpoint peer")
		}
	}
	return nil
}

func validateGatewayFirewallLease(rule networkprotocol.WireGuardFirewallRule, destination netip.Prefix, networkLeases map[string]networkprotocol.NetworkLease, resourceLeases map[string]struct{}, validUntil, now time.Time) error {
	if rule.Effect == "allow" && (rule.ExpiresAt == nil || rule.ExpiresAt.Before(validUntil) || !rule.ExpiresAt.After(now)) {
		return fmt.Errorf("gateway allow rule is not bound to a live lease")
	}
	if rule.Effect == "allow" && rule.SiteLinkRuntimeID == "" {
		lease, networkLease := networkLeases[rule.LeaseID]
		_, resourceLease := resourceLeases[rule.LeaseID]
		if !networkLease && !resourceLease {
			return fmt.Errorf("gateway allow rule references an unknown lease")
		}
		if networkLease && !prefixCoveredByAny(destination, lease.CIDRs) {
			return fmt.Errorf("gateway network allow exceeds its lease")
		}
	}
	return nil
}

func siteRuleCovered(prefixes []netip.Prefix, direction string, source, destination netip.Prefix) bool {
	candidate := source
	if direction == "to_wireguard" {
		candidate = destination
	}
	for _, prefix := range prefixes {
		if prefix.Bits() <= candidate.Bits() && prefix.Contains(candidate.Addr()) {
			return true
		}
	}
	return false
}

func prefixCoveredByAny(candidate netip.Prefix, rawPrefixes []string) bool {
	for _, raw := range rawPrefixes {
		prefix, err := canonicalIPv4Prefix(raw, -1)
		if err == nil && prefix.Bits() <= candidate.Bits() && prefix.Contains(candidate.Addr()) {
			return true
		}
	}
	return false
}

func protectedDenyFollows(later []networkprotocol.WireGuardFirewallRule, allowed networkprotocol.WireGuardFirewallRule, destination netip.Prefix) bool {
	for _, candidate := range later {
		if candidate.Effect != "deny" || candidate.SourceCIDR != allowed.SourceCIDR {
			continue
		}
		denied, err := canonicalIPv4Prefix(candidate.DestinationCIDR, -1)
		if err == nil && destination.Contains(denied.Addr()) && destination.Bits() <= denied.Bits() {
			return true
		}
	}
	return false
}

func validFirewallEffect(effect string) bool { return effect == "allow" || effect == "deny" }

func validFirewallProtocol(protocol string) bool {
	return protocol == "any" || protocol == "tcp" || protocol == "udp"
}

func canonicalIPv4Prefix(raw string, requiredBits int) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || (requiredBits >= 0 && prefix.Bits() != requiredBits) {
		return netip.Prefix{}, fmt.Errorf("IPv4 prefix must be canonical")
	}
	return prefix, nil
}

func BuildNftProgram(wg networkprotocol.WireGuardConfiguration, tableExists bool, egressInterface string) (string, error) {
	if !linuxInterfacePattern.MatchString(wg.InterfaceName) || wg.FirewallDefault != "deny" || (wg.RoutingMode == "snat" && !linuxInterfacePattern.MatchString(egressInterface)) {
		return "", fmt.Errorf("invalid nftables gateway parameters")
	}
	var lines []string
	if tableExists {
		lines = append(lines, "flush table inet "+firewallTable)
	} else {
		lines = append(lines, "add table inet "+firewallTable)
	}
	lines = append(lines,
		"add chain inet "+firewallTable+" forward { type filter hook forward priority -20; policy accept; }",
		"add rule inet "+firewallTable+" forward oifname "+strconv.Quote(wg.InterfaceName)+" ct state established,related accept comment \"return-established\"",
	)
	for _, rule := range wg.FirewallRules {
		line, err := nftFilterRule(wg.InterfaceName, rule)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	lines = append(lines,
		"add rule inet "+firewallTable+" forward iifname "+strconv.Quote(wg.InterfaceName)+" drop comment \"default-deny-from-wireguard\"",
		"add rule inet "+firewallTable+" forward oifname "+strconv.Quote(wg.InterfaceName)+" drop comment \"default-deny-to-wireguard\"",
	)
	if wg.RoutingMode == "snat" {
		lines = append(lines, "add chain inet "+firewallTable+" postrouting { type nat hook postrouting priority srcnat; policy accept; }")
		for _, rule := range wg.FirewallRules {
			if rule.Effect == "allow" {
				lines = append(lines, "add rule inet "+firewallTable+" postrouting ip saddr "+rule.SourceCIDR+" ip daddr "+rule.DestinationCIDR+" oifname "+strconv.Quote(egressInterface)+" masquerade comment "+strconv.Quote("snat-"+rule.ID))
			}
		}
	}
	return strings.Join(lines, "\n") + "\n", nil
}

func nftFilterRule(interfaceName string, rule networkprotocol.WireGuardFirewallRule) (string, error) {
	if !identifierPattern.MatchString(rule.ID) || !validFirewallEffect(rule.Effect) {
		return "", fmt.Errorf("invalid nftables firewall rule")
	}
	if _, err := canonicalIPv4Prefix(rule.SourceCIDR, -1); err != nil {
		return "", fmt.Errorf("invalid nftables firewall source")
	}
	destination, err := canonicalIPv4Prefix(rule.DestinationCIDR, -1)
	if err != nil || destination.Bits() == 0 || !validFirewallProtocol(rule.Protocol) || (rule.Protocol == "any" && len(rule.Ports) != 0) {
		return "", fmt.Errorf("invalid nftables firewall destination or protocol")
	}
	for _, port := range rule.Ports {
		if port < 1 || port > 65535 {
			return "", fmt.Errorf("invalid nftables firewall port")
		}
	}
	direction := "iifname"
	if rule.Direction == "to_wireguard" {
		direction = "oifname"
	} else if rule.Direction != "" && rule.Direction != "from_wireguard" {
		return "", fmt.Errorf("invalid nftables firewall direction")
	}
	parts := []string{"add rule inet", firewallTable, "forward", direction, strconv.Quote(interfaceName), "ip saddr", rule.SourceCIDR, "ip daddr", rule.DestinationCIDR}
	if rule.Protocol != "any" {
		if len(rule.Ports) == 0 {
			parts = append(parts, "ip protocol", rule.Protocol)
		} else if len(rule.Ports) == 1 {
			parts = append(parts, rule.Protocol)
			parts = append(parts, "dport", strconv.Itoa(rule.Ports[0]))
		} else if len(rule.Ports) > 1 {
			parts = append(parts, rule.Protocol)
			ports := slices.Clone(rule.Ports)
			slices.Sort(ports)
			values := make([]string, len(ports))
			for index, port := range ports {
				values[index] = strconv.Itoa(port)
			}
			parts = append(parts, "dport { "+strings.Join(values, ", ")+" }")
		}
	}
	parts = append(parts, map[string]string{"allow": "accept", "deny": "drop"}[rule.Effect], "comment", strconv.Quote(rule.ID))
	return strings.Join(parts, " "), nil
}
