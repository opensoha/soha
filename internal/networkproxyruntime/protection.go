package networkproxyruntime

import (
	"encoding/json"
	"errors"
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"net/netip"
	"regexp"
	"sigs.k8s.io/yaml"
	"strings"
)

var errUnsafeConfiguration = errors.New("unsafe proxy enforcement configuration")
var bindingID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)
var protectionHost = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9.-]{0,251}[a-z0-9])?$`)

func validateProtection(desired domain.Configuration) error {
	if !desired.Enabled {
		return nil
	}
	p, b := desired.Protection, desired.Binding
	if p == nil || p.PolicyVersion < 1 || p.CIDRs == nil || p.Hosts == nil || len(p.CIDRs) > 4096 || len(p.Hosts) > 2048 {
		return errUnsafeConfiguration
	}
	if err := validateProxyBinding(b); err != nil {
		return err
	}
	if len(b.ResourceIDs) > 0 && len(p.CIDRs) == 0 && len(p.Hosts) == 0 {
		return errUnsafeConfiguration
	}
	if err := validateProtectedCIDRs(p.CIDRs); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, host := range p.Hosts {
		if !protectionHost.MatchString(host) || strings.Contains(host, "..") || seen[host] {
			return errUnsafeConfiguration
		}
		seen[host] = true
	}
	return nil
}
func validateProxyBinding(b *domain.Binding) error {
	if b == nil || !bindingID.MatchString(b.SiteID) || !bindingID.MatchString(b.DeviceID) || b.ResourceIDs == nil || len(b.ResourceIDs) > 128 {
		return errUnsafeConfiguration
	}
	seen := map[string]bool{}
	for _, id := range b.ResourceIDs {
		if !bindingID.MatchString(id) || seen[id] {
			return errUnsafeConfiguration
		}
		seen[id] = true
	}
	return nil
}
func validateProtectedCIDRs(values []string) error {
	seen := map[string]bool{}
	for _, raw := range values {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || prefix.Addr().Is4In6() || !prefix.Addr().IsGlobalUnicast() || prefix.Bits() == 0 || prefix != prefix.Masked() || raw != prefix.String() || seen[raw] {
			return errUnsafeConfiguration
		}
		seen[raw] = true
	}
	return nil
}

func protectConfiguration(engine string, raw []byte, protection *domain.Protection) ([]byte, error) {
	if protection == nil {
		return nil, errUnsafeConfiguration
	}
	var doc map[string]any
	if engine == domain.EngineMihomo {
		if err := yaml.UnmarshalStrict(raw, &doc); err != nil {
			return nil, err
		}
	} else if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	if doc == nil || unsafeDestinationRewrite(doc) {
		return nil, errUnsafeConfiguration
	}
	switch engine {
	case domain.EngineMihomo:
		return protectMihomo(doc, protection)
	case domain.EngineSingBox:
		return protectSingBox(doc, protection)
	case domain.EngineV2Ray:
		return protectV2Ray(doc, protection)
	}
	return nil, errUnsafeConfiguration
}

func protectMihomo(doc map[string]any, protection *domain.Protection) ([]byte, error) {
	if nonemptySection(doc["listeners"]) || nonemptySection(doc["tunnels"]) || enabledTunnel(doc["tun"]) {
		return nil, errUnsafeConfiguration
	}
	rules, err := arraySection(doc["rules"])
	if err != nil {
		return nil, err
	}
	prefix := []any{}
	for _, host := range protection.Hosts {
		prefix = append(prefix, "DOMAIN-SUFFIX,"+host+",REJECT")
	}
	for _, cidr := range protection.CIDRs {
		prefix = append(prefix, "IP-CIDR,"+cidr+",REJECT")
	}
	doc["mode"], doc["ipv6"], doc["rules"] = "rule", true, append(prefix, rules...)
	return yaml.Marshal(doc)
}

func protectSingBox(doc map[string]any, protection *domain.Protection) ([]byte, error) {
	if nonemptySection(doc["endpoints"]) {
		return nil, errUnsafeConfiguration
	}
	inbounds, err := arraySection(doc["inbounds"])
	if err != nil {
		return nil, err
	}
	for _, raw := range inbounds {
		inbound, ok := raw.(map[string]any)
		if !ok || inbound["type"] == "tun" {
			return nil, errUnsafeConfiguration
		}
	}
	route, err := object(doc["route"])
	if err != nil {
		return nil, err
	}
	rules, err := arraySection(route["rules"])
	if err != nil {
		return nil, err
	}
	prefix := []any{}
	if len(protection.Hosts) > 0 {
		prefix = append(prefix, map[string]any{"domain_suffix": protection.Hosts, "action": "reject"})
	}
	prefix = append(prefix, map[string]any{"action": "resolve"}, map[string]any{"ip_cidr": protection.CIDRs, "action": "reject"})
	route["rules"] = append(prefix, rules...)
	doc["route"] = route
	experimental, err := object(doc["experimental"])
	if err != nil {
		return nil, err
	}
	api, err := object(experimental["clash_api"])
	if err != nil {
		return nil, err
	}
	api["default_mode"] = "rule"
	experimental["clash_api"] = api
	doc["experimental"] = experimental
	return json.Marshal(doc)
}

func protectV2Ray(doc map[string]any, protection *domain.Protection) ([]byte, error) {
	if nonemptySection(doc["reverse"]) || nonemptySection(doc["browserForwarder"]) {
		return nil, errUnsafeConfiguration
	}
	outbounds, err := arraySection(doc["outbounds"])
	if err != nil || len(outbounds) == 0 {
		return nil, errUnsafeConfiguration
	}
	for _, raw := range outbounds {
		outbound, ok := raw.(map[string]any)
		if !ok || outbound["tag"] == "soha-protected-deny" || outbound["tag"] == "soha-api" {
			return nil, errUnsafeConfiguration
		}
	}
	doc["outbounds"] = append(outbounds, map[string]any{"tag": "soha-protected-deny", "protocol": "blackhole", "settings": map[string]any{}})
	route, err := object(doc["routing"])
	if err != nil {
		return nil, err
	}
	rules, err := arraySection(route["rules"])
	if err != nil || len(rules) == 0 {
		return nil, errUnsafeConfiguration
	}
	prefix := []any{rules[0], map[string]any{"type": "field", "ip": protection.CIDRs, "outboundTag": "soha-protected-deny"}}
	if len(protection.Hosts) > 0 {
		hosts := []string{}
		for _, host := range protection.Hosts {
			hosts = append(hosts, "domain:"+host)
		}
		prefix = append(prefix, map[string]any{"type": "field", "domain": hosts, "outboundTag": "soha-protected-deny"})
	}
	route["domainStrategy"], route["rules"] = "IPOnDemand", append(prefix, rules[1:]...)
	doc["routing"] = route
	return json.Marshal(doc)
}

func arraySection(value any) ([]any, error) {
	if value == nil {
		return []any{}, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%w: expected an array", errUnsafeConfiguration)
	}
	return values, nil
}
func nonemptySection(value any) bool {
	if value == nil {
		return false
	}
	switch item := value.(type) {
	case []any:
		return len(item) > 0
	case map[string]any:
		return len(item) > 0
	}
	return true
}
func enabledTunnel(value any) bool {
	if value == nil {
		return false
	}
	item, ok := value.(map[string]any)
	if !ok {
		return true
	}
	return item["enable"] != nil && item["enable"] != false
}
func unsafeDestinationRewrite(value any) bool {
	switch item := value.(type) {
	case map[string]any:
		for key, value := range item {
			if key == "override_address" || key == "override-address" || key == "redirect" || key == "redirects" || key == "action" && value == "bypass" {
				return true
			}
			if unsafeDestinationRewrite(value) {
				return true
			}
		}
	case []any:
		for _, child := range item {
			if unsafeDestinationRewrite(child) {
				return true
			}
		}
	}
	return false
}
