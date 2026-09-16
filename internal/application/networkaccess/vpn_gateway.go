package networkaccess

import (
	"net/url"
	"regexp"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

var vpnProviderPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{0,64}$`)

func validateGatewaySelection(input domain.GatewayInput) error {
	for name, value := range map[string]*string{"region": input.Region, "providerName": input.ProviderName} {
		if value != nil {
			if err := optionalText(name, *value, 128); err != nil {
				return err
			}
		}
	}
	if input.ProviderCode != nil && !vpnProviderPattern.MatchString(*input.ProviderCode) {
		return invalid("providerCode is invalid")
	}
	if input.SelectionPriority != nil && (*input.SelectionPriority < 0 || *input.SelectionPriority > 10000) {
		return invalid("selectionPriority must be between 0 and 10000")
	}
	if input.MaxSessions != nil && (*input.MaxSessions < 0 || *input.MaxSessions > 1000000) {
		return invalid("maxSessions must be between 0 and 1000000")
	}
	if input.ProbeURL != nil && *input.ProbeURL != "" && !ValidVPNProbeURL(*input.ProbeURL, input.PublicEndpointHost) {
		return invalid("probeURL must be HTTPS on the gateway endpoint host at /vpn/probe without credentials, query or fragment")
	}
	return nil
}

func ValidVPNProbeURL(raw, gatewayHost string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && len(raw) <= 2048 && parsed.Scheme == "https" && parsed.Host != "" && strings.EqualFold(parsed.Hostname(), gatewayHost) && parsed.User == nil && parsed.RawQuery == "" && !parsed.ForceQuery && parsed.Fragment == "" && parsed.Path == "/vpn/probe" && parsed.RawPath == ""
}

func gatewayWithSelection(item domain.Gateway, input domain.GatewayInput) domain.Gateway {
	item.SelectionPriority, item.AcceptNewConnections = 100, true
	if input.Region != nil {
		item.Region = *input.Region
	}
	if input.ProviderCode != nil {
		item.ProviderCode = *input.ProviderCode
	}
	if input.ProviderName != nil {
		item.ProviderName = *input.ProviderName
	}
	if input.SelectionPriority != nil {
		item.SelectionPriority = *input.SelectionPriority
	}
	if input.AcceptNewConnections != nil {
		item.AcceptNewConnections = *input.AcceptNewConnections
	}
	if input.MaxSessions != nil {
		item.MaxSessions = *input.MaxSessions
	}
	if input.ProbeURL != nil {
		item.ProbeURL = *input.ProbeURL
	}
	return item
}
