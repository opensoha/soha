package networkaccess

import (
	"slices"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

func validateAccessNetwork(input *domain.AccessNetworkInput) error {
	input.Name = strings.TrimSpace(input.Name)
	if err := requiredText("name", input.Name, 200); err != nil {
		return err
	}
	if !oneOf(input.Kind, domain.AccessNetworkStandard, domain.AccessNetworkStrict, domain.AccessNetworkBootstrap) || input.Priority < 1 || input.Priority > 10000 {
		return invalid("kind or priority is invalid")
	}
	if len(input.NASBindingIDs) > 128 || len(input.BSSIDs) > 32 || len(input.PortIDs) > 128 || len(input.Rules) > 128 {
		return invalid("network selector or rule count exceeds its limit")
	}
	if err := validateAccessNetworkIDs(input.NASBindingIDs); err != nil {
		return err
	}
	if err := validateAccessNetworkSelectors(input); err != nil {
		return err
	}
	if input.NASBindingIDs == nil {
		input.NASBindingIDs = []string{}
	}
	if input.Rules == nil {
		input.Rules = []domain.AccessNetworkRule{}
	}
	if err := validateAccessNetworkAttributes(input.VLANID, input.FilterID, input.SessionTimeoutSeconds); err != nil {
		return err
	}
	for _, rule := range input.Rules {
		if err := validateAccessNetworkRule(rule); err != nil {
			return err
		}
	}
	return validateAccessNetworkRuleConflicts(*input)
}

func validateAccessNetworkSelectors(input *domain.AccessNetworkInput) error {
	if input.Kind == domain.AccessNetworkBootstrap {
		if input.AccessMedium != domain.AccessMediumWiFi || len(input.NASBindingIDs)+len(input.Rules)+len(input.BSSIDs) != 0 || input.VLANID != 0 || input.FilterID != "" || input.SessionTimeoutSeconds != 0 {
			return invalid("an open bootstrap network has no RADIUS bindings, rules, or attributes")
		}
	} else if len(input.NASBindingIDs) == 0 {
		return invalid("at least one nasBindingId is required")
	}
	if input.AccessMedium == domain.AccessMediumWiFi {
		if len(input.SSID) == 0 || len(input.SSID) > 32 || containsASCIIControl(input.SSID) || len(input.PortIDs) != 0 || input.PortStart != nil || input.PortEnd != nil {
			return invalid("Wi-Fi requires a valid ssid and may only use wireless selectors")
		}
		for i, bssid := range input.BSSIDs {
			canonical := canonicalBSSID(bssid)
			if canonical == "" || slices.Contains(input.BSSIDs[:i], canonical) {
				return invalid("bssids must contain distinct MAC addresses")
			}
			input.BSSIDs[i] = canonical
		}
		return nil
	}
	if input.AccessMedium != domain.AccessMediumWired || input.SSID != "" || len(input.BSSIDs) != 0 {
		return invalid("wired networks may only use port selectors")
	}
	return validateAccessPortRange(input.PortIDs, input.PortStart, input.PortEnd)
}

func validateAccessPortRange(ids []string, start, end *int64) error {
	for _, id := range ids {
		if id == "" || len(id) > 128 || containsASCIIControl(id) || strings.TrimSpace(id) != id {
			return invalid("portIds contain an invalid identifier")
		}
	}
	if start == nil && end == nil && len(ids) != 0 {
		return nil
	}
	if start == nil || end == nil || *start < 0 || *end > 4294967295 || *start > *end {
		return invalid("specify wired port IDs or a complete ascending port range")
	}
	return nil
}

func validateAccessNetworkIDs(ids []string) error {
	for i, id := range ids {
		if !runtimeIdentifierPattern.MatchString(id) || slices.Contains(ids[:i], id) {
			return invalid("identifiers must be valid and distinct")
		}
	}
	return nil
}

func containsASCIIControl(text string) bool {
	return strings.ContainsFunc(text, func(r rune) bool { return r < ' ' || r == 127 })
}

func validateAccessNetworkRule(rule domain.AccessNetworkRule) error {
	if !oneOf(rule.Effect, domain.PolicyEffectAllow, domain.PolicyEffectDeny) {
		return invalid("rule effect is invalid")
	}
	if rule.Subjects.Users == nil || rule.Subjects.Teams == nil || rule.Subjects.Tags == nil {
		return invalid("rule subjects must specify users, teams, and tags explicitly")
	}
	if len(rule.DeviceIDs) > 256 || len(rule.AuthenticationMethods) > 2 {
		return invalid("rule selector count exceeds its limit")
	}
	for _, method := range rule.AuthenticationMethods {
		if !oneOf(method, "eap_tls", "eap_ttls_pap") {
			return invalid("rule authentication method is invalid")
		}
	}
	if hasAccessDeviceSelectors(rule) && !slices.Equal(rule.AuthenticationMethods, []string{"eap_tls"}) {
		return invalid("device selectors require explicitly selecting EAP-TLS")
	}
	start, end := rule.ScheduleStartMinute, rule.ScheduleEndMinute
	if (start == nil) != (end == nil) || (start != nil && (*start == *end || *start < 0 || *start > 1439 || *end < 0 || *end > 1439)) {
		return invalid("specify two different UTC schedule minutes between 0 and 1439")
	}
	return validateAccessNetworkAttributes(rule.VLANID, rule.FilterID, rule.SessionTimeoutSeconds)
}

func validateAccessNetworkAttributes(vlan int, filter string, timeout int) error {
	if vlan < 0 || vlan > 4094 || timeout < 0 || timeout > 86400 || len(filter) > 128 || containsASCIIControl(filter) {
		return invalid("VLAN, filter, or session timeout is invalid")
	}
	return nil
}

func validateAccessNetworkRuleConflicts(input domain.AccessNetworkInput) error {
	for i, rule := range input.Rules {
		if rule.Effect != domain.PolicyEffectAllow {
			continue
		}
		first := effectiveNetworkRule(input, rule)
		for _, other := range input.Rules[:i] {
			if other.Effect != domain.PolicyEffectAllow {
				continue
			}
			second := effectiveNetworkRule(input, other)
			if (first.VLANID != second.VLANID || first.FilterID != second.FilterID) && accessRulesCanOverlap(first, second) {
				return invalid("overlapping allow rules yield conflicting VLAN or filter")
			}
		}
	}
	return nil
}

func accessRulesCanOverlap(left, right domain.AccessNetworkRule) bool {
	for _, values := range [][2][]string{
		{left.Subjects.Users, right.Subjects.Users}, {left.DeviceIDs, right.DeviceIDs},
		{left.DeviceStatuses, right.DeviceStatuses}, {left.PostureStatuses, right.PostureStatuses},
		{left.AuthenticationMethods, right.AuthenticationMethods},
	} {
		if len(values[0]) != 0 && len(values[1]) != 0 && !matchNetworkSlice(values[0], values[1]) {
			return false
		}
	}
	if left.ScheduleStartMinute == nil || right.ScheduleStartMinute == nil {
		return true
	}
	return insideAccessWindow(*left.ScheduleStartMinute, right.ScheduleStartMinute, right.ScheduleEndMinute) ||
		insideAccessWindow(*right.ScheduleStartMinute, left.ScheduleStartMinute, left.ScheduleEndMinute)
}
