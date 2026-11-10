package networkaccess

import (
	"net/netip"
	"time"
)

// WireGuardResourceTarget is the compatibility validation entry; runtime
// compilation uses every target returned by WireGuardResourceTargets.
func WireGuardResourceTarget(resource Resource, space Space) (string, bool) {
	targets, ok := WireGuardResourceTargets(resource, space)
	if !ok || len(targets) == 0 {
		return "", false
	}
	return targets[0], true
}

func WireGuardResourceTargets(resource Resource, space Space) ([]string, bool) {
	if resource.SpaceID != space.ID || !validWireGuardProtocol(resource.Protocol, resource.Ports) {
		return nil, false
	}
	if resource.Kind == "fqdn" {
		return resolvedResourceTargets(resource, space)
	}
	target, ok := wireGuardTargetPrefix(resource)
	if !ok || !prefixWithinSpace(target, space.CIDRs) {
		return nil, false
	}
	return []string{target.String()}, true
}

func resolvedResourceTargets(resource Resource, space Space) ([]string, bool) {
	if !resource.DNSExpiresAt.After(time.Now()) || len(resource.TrustedAddresses) == 0 || len(resource.TrustedAddresses) > 32 {
		return nil, false
	}
	targets := make([]string, 0, len(resource.TrustedAddresses))
	for _, raw := range resource.TrustedAddresses {
		address, err := netip.ParseAddr(raw)
		if err != nil || !address.IsGlobalUnicast() || address.Is4In6() {
			return nil, false
		}
		target := netip.PrefixFrom(address, address.BitLen())
		if !prefixWithinSpace(target, space.CIDRs) {
			return nil, false
		}
		targets = append(targets, target.String())
	}
	return targets, true
}

func validWireGuardProtocol(protocol string, ports []int) bool {
	if protocol == "any" {
		return len(ports) == 0
	}
	if (protocol != "tcp" && protocol != "udp") || len(ports) == 0 {
		return false
	}
	for _, port := range ports {
		if port < 1 || port > 65535 {
			return false
		}
	}
	return true
}

func wireGuardTargetPrefix(resource Resource) (netip.Prefix, bool) {
	var target netip.Prefix
	var err error
	switch resource.Kind {
	case "ip":
		var address netip.Addr
		address, err = netip.ParseAddr(resource.Target)
		if err == nil && !address.Is4In6() {
			target = netip.PrefixFrom(address, address.BitLen())
		}
	case "cidr":
		target, err = netip.ParsePrefix(resource.Target)
	default:
		return netip.Prefix{}, false
	}
	if err != nil || !target.IsValid() || target.Addr().Is4In6() || target != target.Masked() || target.Bits() == 0 {
		return netip.Prefix{}, false
	}
	return target, true
}

func prefixWithinSpace(target netip.Prefix, cidrs []string) bool {
	for _, raw := range cidrs {
		spacePrefix, parseErr := netip.ParsePrefix(raw)
		if parseErr == nil && spacePrefix.Addr().BitLen() == target.Addr().BitLen() && spacePrefix == spacePrefix.Masked() && spacePrefix.Bits() <= target.Bits() && spacePrefix.Contains(target.Addr()) {
			return true
		}
	}
	return false
}

// ValidOverlayPrefix bounds allocation; IPv6 pools use at most 65534 peers.
func ValidOverlayPrefix(prefix netip.Prefix) bool {
	if !prefix.IsValid() || prefix != prefix.Masked() || !prefix.Addr().IsGlobalUnicast() || prefix.Addr().Is4In6() {
		return false
	}
	if prefix.Addr().Is4() {
		return prefix.Bits() >= 16 && prefix.Bits() <= 30
	}
	return prefix.Bits() >= 64 && prefix.Bits() <= 126
}
