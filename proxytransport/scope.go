package proxytransport

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
)

type Scope struct {
	prefixes []netip.Prefix
	hosts    []string
}

func NewScope(cidrs, hosts []string) (*Scope, error) {
	if len(cidrs) > 4096 || len(hosts) > 2048 {
		return nil, errors.New("proxy destination scope exceeds its limit")
	}
	scope := &Scope{hosts: append([]string(nil), hosts...)}
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().IsGlobalUnicast() || prefix.Addr().Is4In6() || prefix.Bits() == 0 || prefix != prefix.Masked() || prefix.String() != raw {
			return nil, errors.New("invalid proxy destination prefix")
		}
		scope.prefixes = append(scope.prefixes, prefix)
	}
	for _, host := range hosts {
		if !validHost(host) || strings.ToLower(host) != host {
			return nil, errors.New("invalid protected hostname")
		}
	}
	return scope, nil
}

// Resolve checks every answer before choosing one. Returning a literal IP to a
// transport prevents a second, remote DNS result from changing the destination.
func (s *Scope) Resolve(ctx context.Context, host string) (netip.Addr, error) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, protected := range s.hosts {
		if host == protected || strings.HasSuffix(host, "."+protected) {
			return netip.Addr{}, errors.New("protected proxy hostname")
		}
	}
	addresses := []netip.Addr{}
	if address, err := netip.ParseAddr(host); err == nil {
		addresses = append(addresses, address)
	} else {
		if !validHost(host) {
			return netip.Addr{}, errors.New("invalid proxy hostname")
		}
		var err error
		addresses, err = net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		if err != nil {
			return netip.Addr{}, errors.New("proxy DNS unavailable")
		}
	}
	if len(addresses) == 0 || len(addresses) > 32 {
		return netip.Addr{}, errors.New("invalid proxy DNS answer count")
	}
	for _, address := range addresses {
		if !address.IsGlobalUnicast() || address.Is4In6() {
			return netip.Addr{}, errors.New("unsafe proxy address")
		}
		for _, prefix := range s.prefixes {
			if prefix.Contains(address) {
				return netip.Addr{}, errors.New("protected proxy address")
			}
		}
	}
	return addresses[0], nil
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, char := range label {
			if char != '-' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
				return false
			}
		}
	}
	return true
}
