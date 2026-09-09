package networkaccess

import "net/netip"

// WireGuardResourceTarget returns the canonical IPv4 route for a resource that
// the current L3 WireGuard enforcement plane can protect.
func WireGuardResourceTarget(resource Resource, space Space) (string, bool) {
	if resource.SpaceID != space.ID {
		return "", false
	}
	if !validWireGuardProtocol(resource.Protocol, resource.Ports) {
		return "", false
	}
	target, ok := wireGuardTargetPrefix(resource)
	if !ok {
		return "", false
	}
	if !prefixWithinSpace(target, space.CIDRs) {
		return "", false
	}
	return target.String(), true
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
		if err == nil && address.Is4() {
			target = netip.PrefixFrom(address, 32)
		}
	case "cidr":
		target, err = netip.ParsePrefix(resource.Target)
	default:
		return netip.Prefix{}, false
	}
	if err != nil || !target.Addr().Is4() || target != target.Masked() || target.Bits() == 0 {
		return netip.Prefix{}, false
	}
	return target, true
}

func prefixWithinSpace(target netip.Prefix, cidrs []string) bool {
	for _, raw := range cidrs {
		spacePrefix, parseErr := netip.ParsePrefix(raw)
		if parseErr == nil && spacePrefix.Addr().Is4() && spacePrefix == spacePrefix.Masked() && spacePrefix.Bits() <= target.Bits() && spacePrefix.Contains(target.Addr()) {
			return true
		}
	}
	return false
}
