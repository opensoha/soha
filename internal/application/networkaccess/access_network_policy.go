package networkaccess

import (
	"net"
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
)

type AccessAttachment struct {
	CalledStationID string
	NASPortID       string
	NASPort         *int64
}

type AccessNetworkDecision struct {
	AccessNetworkID       string `json:"accessNetworkId"`
	Name                  string `json:"name"`
	Allowed               bool   `json:"allowed"`
	ReasonCode            string `json:"reasonCode"`
	AccessProfile         string `json:"accessProfile,omitempty"`
	MatchedRuleIndex      int    `json:"matchedRuleIndex,omitempty"`
	VLANID                int    `json:"-"`
	FilterID              string `json:"-"`
	SessionTimeoutSeconds int    `json:"-"`
	StrictOnline          bool   `json:"-"`
}

// MatchAccessNetwork selects only from the authenticated NAS's site. Retired rows
// keep the site managed so that retiring its last network cannot restore legacy access.
func MatchAccessNetwork(networks []domain.AccessNetwork, binding domain.NASBinding, attachment AccessAttachment) (*domain.AccessNetwork, bool) {
	bssid, ssid := calledStationNetwork(attachment.CalledStationID)
	var matched *domain.AccessNetwork
	managed := false
	for i := range networks {
		network := &networks[i]
		cfg := network.Configuration
		if cfg.SiteID != binding.SiteID || cfg.Kind == domain.AccessNetworkBootstrap {
			continue
		}
		managed = managed || network.ManagesNASAdmission()
		if network.Deleted || !cfg.Enabled || !slices.Contains(cfg.NASBindingIDs, binding.ID) || (binding.AccessMedium != "" && cfg.AccessMedium != binding.AccessMedium) {
			continue
		}
		if !matchesAccessAttachment(cfg, attachment, bssid, ssid) {
			continue
		}
		if matched != nil {
			return nil, true
		}
		matched = network
	}
	return matched, managed
}

func matchesAccessAttachment(cfg domain.AccessNetworkInput, attachment AccessAttachment, bssid, ssid string) bool {
	if cfg.AccessMedium == domain.AccessMediumWiFi {
		return ssid != "" && cfg.SSID == ssid && matchNetworkValue(cfg.BSSIDs, bssid)
	}
	if cfg.AccessMedium != domain.AccessMediumWired || ssid != "" {
		return false
	}
	return (attachment.NASPortID != "" && slices.Contains(cfg.PortIDs, attachment.NASPortID)) ||
		(cfg.PortStart != nil && cfg.PortEnd != nil && attachment.NASPort != nil &&
			*cfg.PortStart <= *attachment.NASPort && *attachment.NASPort <= *cfg.PortEnd)
}

func calledStationNetwork(value string) (string, string) {
	// RFC 3580 uses a 17-character MAC followed by a colon and the case-sensitive SSID.
	if len(value) < 19 || value[17] != ':' {
		return "", ""
	}
	bssid := canonicalBSSID(value[:17])
	if bssid == "" {
		return "", ""
	}
	return bssid, value[18:]
}

func canonicalBSSID(value string) string {
	mac, err := net.ParseMAC(value)
	if err != nil || len(mac) != 6 {
		return ""
	}
	return mac.String()
}

func EvaluateAccessNetwork(now time.Time, network domain.AccessNetwork, method string, subject domain.Subject, device domain.Device, site domain.Site, snapshot domain.PolicySnapshot) AccessNetworkDecision {
	return evaluateAccessNetwork(now, network, method, subject, device, site, snapshot, false)
}

func evaluateAccessNetwork(now time.Time, network domain.AccessNetwork, method string, subject domain.Subject, device domain.Device, site domain.Site, snapshot domain.PolicySnapshot, strictQualified bool) AccessNetworkDecision {
	cfg := network.Configuration
	decision := AccessNetworkDecision{AccessNetworkID: network.ID, Name: cfg.Name, ReasonCode: "access_network_disabled", StrictOnline: cfg.Kind == domain.AccessNetworkStrict}
	if network.Deleted || !cfg.Enabled || cfg.SiteID != site.ID {
		return decision
	}
	method = strings.ReplaceAll(method, "-", "_")
	if cfg.Kind != domain.AccessNetworkStandard && (!decision.StrictOnline || !strictQualified || method != "eap_tls") {
		decision.ReasonCode = "access_network_requires_other_admission"
		return decision
	}
	if method == "eap_ttls_pap" {
		input := AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: domain.ModeInternalDirect}
		if admissionDenyReason(input, subject, device, site, snapshot) != "" {
			decision.ReasonCode = "site_admission_denied"
			return decision
		}
		device = domain.Device{ID: device.ID, OwnerUserID: device.OwnerUserID, SiteID: device.SiteID, Status: domain.DeviceStatusActive, PostureStatus: domain.PostureUnknown, ManagementState: domain.DeviceManagementUnmanaged}
		snapshot = domain.PasswordAdmissionPolicies(snapshot)
	}
	admission := evaluateAdmissionAt(now, AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: domain.ModeInternalDirect}, subject, device, site, snapshot)
	if admission.Decision != domain.DecisionAllow {
		decision.ReasonCode = "site_admission_denied"
		return decision
	}
	attrs, reason, ruleIndex := evaluateAccessNetworkRules(now, cfg, method, subject, device)
	decision.ReasonCode, decision.MatchedRuleIndex = reason, ruleIndex
	if reason == "access_network_allowed" {
		decision.Allowed, decision.AccessProfile = true, admission.NetworkProfile
		decision.VLANID, decision.FilterID, decision.SessionTimeoutSeconds = attrs.VLANID, attrs.FilterID, attrs.SessionTimeoutSeconds
	}
	return decision
}

func evaluateAccessNetworkRules(now time.Time, cfg domain.AccessNetworkInput, method string, subject domain.Subject, device domain.Device) (domain.AccessNetworkRule, string, int) {
	var allowed domain.AccessNetworkRule
	index := 0
	for i, rule := range cfg.Rules {
		if !matchesAccessNetworkRule(now, rule, method, subject) {
			continue
		}
		if method == "eap_ttls_pap" && hasAccessDeviceSelectors(rule) {
			if rule.Effect == domain.PolicyEffectDeny {
				return domain.AccessNetworkRule{}, "access_network_device_proof_required", i + 1
			}
			continue
		}
		if !matchNetworkValue(rule.DeviceIDs, device.ID) || !matchNetworkValue(rule.DeviceStatuses, device.Status) || !matchNetworkValue(rule.PostureStatuses, device.PostureStatus) {
			continue
		}
		if rule.Effect == domain.PolicyEffectDeny {
			return domain.AccessNetworkRule{}, "access_network_explicit_deny", i + 1
		}
		if rule.Effect != domain.PolicyEffectAllow {
			continue
		}
		matched := effectiveNetworkRule(cfg, rule)
		if index == 0 {
			allowed, index = matched, i+1
			continue
		}
		if allowed.VLANID != matched.VLANID || allowed.FilterID != matched.FilterID {
			return domain.AccessNetworkRule{}, "access_network_rule_conflict", i + 1
		}
		if matched.SessionTimeoutSeconds > 0 && (allowed.SessionTimeoutSeconds == 0 || matched.SessionTimeoutSeconds < allowed.SessionTimeoutSeconds) {
			allowed.SessionTimeoutSeconds = matched.SessionTimeoutSeconds
		}
	}
	if index == 0 {
		return domain.AccessNetworkRule{}, "access_network_missing_allow", 0
	}
	return allowed, "access_network_allowed", index
}

func effectiveNetworkRule(cfg domain.AccessNetworkInput, rule domain.AccessNetworkRule) domain.AccessNetworkRule {
	if rule.VLANID == 0 {
		rule.VLANID = cfg.VLANID
	}
	if rule.FilterID == "" {
		rule.FilterID = cfg.FilterID
	}
	if rule.SessionTimeoutSeconds == 0 {
		rule.SessionTimeoutSeconds = cfg.SessionTimeoutSeconds
	}
	return rule
}

func hasAccessDeviceSelectors(rule domain.AccessNetworkRule) bool {
	return len(rule.DeviceIDs)+len(rule.DeviceStatuses)+len(rule.PostureStatuses) > 0
}

func matchesAccessNetworkRule(now time.Time, rule domain.AccessNetworkRule, method string, subject domain.Subject) bool {
	return matchNetworkValue(rule.Subjects.Users, subject.UserID) &&
		matchNetworkSlice(rule.Subjects.Roles, subject.Roles) &&
		matchNetworkSlice(rule.Subjects.Teams, subject.Teams) &&
		matchNetworkSlice(rule.Subjects.Tags, subject.Tags) &&
		matchNetworkValue(rule.AuthenticationMethods, method) &&
		insideAccessWindow(now.UTC().Hour()*60+now.UTC().Minute(), rule.ScheduleStartMinute, rule.ScheduleEndMinute)
}

func insideAccessWindow(minute int, start, end *int) bool {
	if start == nil || end == nil {
		return start == end
	}
	if *end <= *start {
		return minute >= *start || minute < *end
	}
	return minute >= *start && minute < *end
}
