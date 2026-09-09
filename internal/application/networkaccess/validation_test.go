package networkaccess

import (
	"errors"
	"strings"
	"testing"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestValidateSpaceAndResourceInputs(t *testing.T) {
	if err := validateSpaceInput(domainnetworkaccess.SpaceInput{SiteID: "site-1", Name: "corp", Status: domainnetworkaccess.StatusActive, CIDRs: []string{"10.0.0.0/24"}}); err != nil {
		t.Fatalf("valid space rejected: %v", err)
	}
	for _, cidr := range []string{"10.0.0.1/24", "2001:db8::/64", "not-a-cidr"} {
		err := validateSpaceInput(domainnetworkaccess.SpaceInput{SiteID: "site-1", Name: "corp", Status: domainnetworkaccess.StatusActive, CIDRs: []string{cidr}})
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("CIDR %q error = %v, want invalid argument", cidr, err)
		}
	}
	if err := validateSpaceInput(domainnetworkaccess.SpaceInput{SiteID: "site-1", Name: "overlap", Status: domainnetworkaccess.StatusActive, CIDRs: []string{"10.0.0.0/16", "10.0.1.0/24"}}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("overlapping CIDRs error = %v, want invalid argument", err)
	}

	valid := domainnetworkaccess.ResourceInput{SpaceID: "space-1", Name: "git", Kind: "fqdn", Target: "git.corp.example", Protocol: "tcp", Ports: []int{443}, Protected: true, PathMode: domainnetworkaccess.PathAccessProxy}
	if err := validateResourceInput(valid); err != nil {
		t.Fatalf("valid resource rejected: %v", err)
	}
	invalid := []domainnetworkaccess.ResourceInput{
		{SpaceID: "space-1", Name: "bad fqdn", Kind: "fqdn", Target: "-bad.example", Protocol: "tcp", Ports: []int{443}, PathMode: domainnetworkaccess.PathAutomatic},
		{SpaceID: "space-1", Name: "missing ports", Kind: "ip", Target: "10.0.0.10", Protocol: "tcp", PathMode: domainnetworkaccess.PathAutomatic},
		{SpaceID: "space-1", Name: "unexpected ports", Kind: "cidr", Target: "10.0.0.0/24", Protocol: "icmp", Ports: []int{1}, PathMode: domainnetworkaccess.PathAutomatic},
		{SpaceID: "space-1", Name: "protected bypass", Kind: "ip", Target: "10.0.0.10", Protocol: "any", Protected: true, PathMode: domainnetworkaccess.PathSiteDirect},
	}
	for _, input := range invalid {
		if err := validateResourceInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("resource %q error = %v, want invalid argument", input.Name, err)
		}
	}
}

func TestValidateDevicePostureInput(t *testing.T) {
	base := domainnetworkaccess.DeviceInput{Name: "Laptop", Status: domainnetworkaccess.DeviceStatusActive, DeviceType: domainnetworkaccess.DeviceTypeLaptop, OwnershipType: domainnetworkaccess.DeviceOwnershipCompany}
	if err := validateDeviceInput(base); err != nil {
		t.Fatalf("omitted posture status rejected: %v", err)
	}
	base.PostureStatus = domainnetworkaccess.PostureCompliant
	if err := validateDeviceInput(base); err != nil {
		t.Fatalf("valid posture status rejected: %v", err)
	}
	base.PostureStatus = "trusted"
	if err := validateDeviceInput(base); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("invalid posture status error = %v, want invalid argument", err)
	}
}

func TestValidateDeviceRegistrationFacts(t *testing.T) {
	valid := domainnetworkaccess.DeviceRegistrationInput{
		Name: "Mac", Platform: "darwin", DeviceType: domainnetworkaccess.DeviceTypeLaptop,
		ReportedFacts: &domainnetworkaccess.DeviceReportedFacts{
			Architecture: "arm64", AgentVersion: "0.2.0", CollectedAt: time.Now().UTC(),
			NetworkInterfaces: []domainnetworkaccess.DeviceNetworkInterface{{
				Name: "en0", Kind: domainnetworkaccess.NetworkInterfaceKindPhysical, Status: domainnetworkaccess.NetworkInterfaceStatusUp,
				MACAddress: "00:11:22:33:44:55", IPv4Addresses: []string{"192.168.1.10"}, IPv6Addresses: []string{"fe80::1"},
			}},
		},
	}
	if err := validateDeviceRegistration("endpoint-mac-1", valid); err != nil {
		t.Fatalf("valid endpoint facts rejected: %v", err)
	}
	valid.ReportedFacts.NetworkInterfaces[0].IPv4Addresses[0] = "not-an-ip"
	if err := validateDeviceRegistration("endpoint-mac-1", valid); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("invalid endpoint facts error = %v, want invalid argument", err)
	}
}

func TestValidateListFilters(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"device status", validateDeviceFilter(domainnetworkaccess.DeviceFilter{Status: "lost"})},
		{"site status", validateSiteFilter(domainnetworkaccess.SiteFilter{Status: "lost"})},
		{"space site ID", validateSpaceFilter(domainnetworkaccess.SpaceFilter{SiteID: strings.Repeat("x", 129)})},
		{"resource kind", validateResourceFilter(domainnetworkaccess.ResourceFilter{Kind: "url"})},
		{"gateway status", validateGatewayFilter(domainnetworkaccess.GatewayFilter{Status: "lost"})},
		{"search length", validateSiteFilter(domainnetworkaccess.SiteFilter{Search: strings.Repeat("x", 201)})},
		{"limit", validateSiteFilter(domainnetworkaccess.SiteFilter{Limit: 201})},
	}
	for _, test := range tests {
		if !errors.Is(test.err, apperrors.ErrInvalidArgument) {
			t.Errorf("%s error = %v, want invalid argument", test.name, test.err)
		}
	}
	if err := validateResourceFilter(domainnetworkaccess.ResourceFilter{Search: "git", SpaceID: "space-1", Kind: "fqdn", Limit: 200}); err != nil {
		t.Fatalf("valid filter rejected: %v", err)
	}
}

func TestValidateGatewayInput(t *testing.T) {
	valid := domainnetworkaccess.GatewayInput{
		RuntimeID: "gateway-hq", SiteID: "site-hq", Name: "HQ gateway", AdministrativeStatus: domainnetworkaccess.StatusActive,
		PublicEndpointHost: "vpn.example.com", PublicEndpointPort: 51820, OverlayCIDR: "100.96.0.0/24",
		RoutingMode: domainnetworkaccess.GatewayRoutingRouted, MTU: 1420, PersistentKeepaliveSeconds: 25,
		DNSServers: []string{"10.0.0.53"}, AdvertisedCIDRs: []string{"10.0.0.0/24"},
	}
	if err := validateGatewayInput(valid); err != nil {
		t.Fatalf("valid gateway rejected: %v", err)
	}
	for name, mutate := range map[string]func(*domainnetworkaccess.GatewayInput){
		"runtime":        func(input *domainnetworkaccess.GatewayInput) { input.RuntimeID = "bad runtime" },
		"host":           func(input *domainnetworkaccess.GatewayInput) { input.PublicEndpointHost = "-bad.example" },
		"host bits":      func(input *domainnetworkaccess.GatewayInput) { input.OverlayCIDR = "100.96.0.0/15" },
		"canonical CIDR": func(input *domainnetworkaccess.GatewayInput) { input.OverlayCIDR = "100.96.0.1/24" },
		"default route":  func(input *domainnetworkaccess.GatewayInput) { input.OverlayCIDR = "0.0.0.0/0" },
		"dns":            func(input *domainnetworkaccess.GatewayInput) { input.DNSServers = []string{"2001:db8::53"} },
		"hub ID":         func(input *domainnetworkaccess.GatewayInput) { input.HubGatewayID = "bad hub" },
		"advertised":     func(input *domainnetworkaccess.GatewayInput) { input.AdvertisedCIDRs = []string{"10.0.0.1/24"} },
		"overlap": func(input *domainnetworkaccess.GatewayInput) {
			input.AdvertisedCIDRs = []string{"10.0.0.0/16", "10.0.1.0/24"}
		},
		"spoke SNAT": func(input *domainnetworkaccess.GatewayInput) {
			input.HubGatewayID, input.RoutingMode = "gateway-hub", domainnetworkaccess.GatewayRoutingSNAT
		},
	} {
		input := valid
		mutate(&input)
		if err := validateGatewayInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Errorf("%s error = %v, want invalid argument", name, err)
		}
	}
}

func TestValidateNACBindings(t *testing.T) {
	validNAS := domainnetworkaccess.NASBindingInput{NASID: "nas-hq-wifi", RuntimeID: "freeradius-hq", SiteID: "site-hq", Name: "HQ Wi-Fi", AccessMedium: domainnetworkaccess.AccessMediumWiFi, DeviceType: domainnetworkaccess.AccessDeviceTypeWirelessController, SSID: "Soha-Staff", ManagementAddress: "10.0.10.2", Status: domainnetworkaccess.StatusActive, CoASupported: true, DisconnectSupported: true}
	if err := validateNASBindingInput(validNAS); err != nil {
		t.Fatalf("valid NAS binding rejected: %v", err)
	}
	for _, input := range []domainnetworkaccess.NASBindingInput{
		{NASID: "", RuntimeID: "freeradius-hq", SiteID: "site-hq", Name: "HQ", Status: domainnetworkaccess.StatusActive},
		{NASID: "nas-hq", RuntimeID: "freeradius-hq", SiteID: "site-hq", Name: "HQ", Status: "unknown"},
		{NASID: "nas-hq", RuntimeID: "freeradius-hq", SiteID: "site-hq", Name: "HQ", AccessMedium: domainnetworkaccess.AccessMediumWired, SSID: "Soha-Staff", Status: domainnetworkaccess.StatusActive},
	} {
		if err := validateNASBindingInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("NAS binding %#v error = %v, want invalid argument", input, err)
		}
	}

	validProfile := domainnetworkaccess.SiteProfileBindingInput{SiteID: "site-hq", AccessProfile: domainnetworkaccess.ProfileRestricted, VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 3600}
	if err := validateSiteProfileBindingInput(validProfile); err != nil {
		t.Fatalf("valid site profile binding rejected: %v", err)
	}
	for _, input := range []domainnetworkaccess.SiteProfileBindingInput{
		{SiteID: "site-hq", AccessProfile: domainnetworkaccess.ProfileFull, VLANID: 4095, SessionTimeoutSeconds: 3600},
		{SiteID: "site-hq", AccessProfile: "admin", SessionTimeoutSeconds: 3600},
		{SiteID: "site-hq", AccessProfile: domainnetworkaccess.ProfileFull, SessionTimeoutSeconds: 0},
	} {
		if err := validateSiteProfileBindingInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("site profile binding %#v error = %v, want invalid argument", input, err)
		}
	}
}

func TestValidateMihomoProfileInput(t *testing.T) {
	subscriptionURL := "https://subscription.example/api?token=secret"
	valid := domainnetworkaccess.MihomoProfileInput{
		DeviceID: "device-1", Name: "Managed proxy", Mode: domainnetworkaccess.MihomoModeManagedFollow,
		SourceType: domainnetworkaccess.MihomoSourceManagedSubscription,
		Status:     domainnetworkaccess.StatusActive, SubscriptionURL: &subscriptionURL, MixedPort: 7890,
		ControllerPort: 9090, DNSMode: domainnetworkaccess.MihomoDNSFakeIP, FakeIPRange: "198.18.0.0/16",
		SelectorGroup: "SOHA", SelectedProxy: "Hong Kong", BypassCIDRs: []string{"10.0.0.0/8"},
		BypassHosts: []string{"control.example.com", "10.0.0.53"}, FailClosed: true,
	}
	if err := validateMihomoProfileInput(valid); err != nil {
		t.Fatalf("valid managed profile rejected: %v", err)
	}
	app := valid
	app.Mode, app.SourceType, app.SubscriptionURL, app.SelectedProxy = domainnetworkaccess.MihomoModeAppSubscription, "", nil, ""
	app.DNSMode, app.FakeIPRange, app.FailClosed = domainnetworkaccess.MihomoDNSDisabled, "", false
	if err := validateMihomoProfileInput(app); err != nil {
		t.Fatalf("valid app-subscription profile rejected: %v", err)
	}
	manual := valid
	manual.SourceType, manual.SubscriptionURL, manual.SelectedProxy = domainnetworkaccess.MihomoSourceManualNode, nil, ""
	manual.ManualNode = &domainnetworkaccess.MihomoManualNode{Protocol: domainnetworkaccess.MihomoManualProtocolSOCKS5, Server: "proxy.example.com", Port: 1080, Username: "alice", Password: "secret"}
	if err := validateMihomoProfileInput(manual); err != nil {
		t.Fatalf("valid manual-node profile rejected: %v", err)
	}

	for name, mutate := range map[string]func(*domainnetworkaccess.MihomoProfileInput){
		"insecure subscription": func(input *domainnetworkaccess.MihomoProfileInput) {
			value := "http://subscription.example/config"
			input.SubscriptionURL = &value
		},
		"subscription userinfo": func(input *domainnetworkaccess.MihomoProfileInput) {
			value := "https://user:secret@subscription.example/config"
			input.SubscriptionURL = &value
		},
		"same ports":   func(input *domainnetworkaccess.MihomoProfileInput) { input.ControllerPort = input.MixedPort },
		"managed open": func(input *domainnetworkaccess.MihomoProfileInput) { input.FailClosed = false },
		"fake IP overlap": func(input *domainnetworkaccess.MihomoProfileInput) {
			input.BypassCIDRs = []string{"198.18.1.0/24"}
		},
		"default bypass": func(input *domainnetworkaccess.MihomoProfileInput) { input.BypassCIDRs = []string{"0.0.0.0/0"} },
		"invalid host":   func(input *domainnetworkaccess.MihomoProfileInput) { input.BypassHosts = []string{"*.example.com"} },
		"manual mixed with subscription": func(input *domainnetworkaccess.MihomoProfileInput) {
			input.SourceType = domainnetworkaccess.MihomoSourceManualNode
			input.ManualNode = &domainnetworkaccess.MihomoManualNode{Protocol: domainnetworkaccess.MihomoManualProtocolHTTP, Server: "proxy.example.com", Port: 8080}
		},
		"manual partial credentials": func(input *domainnetworkaccess.MihomoProfileInput) {
			input.SourceType, input.SubscriptionURL, input.SelectedProxy = domainnetworkaccess.MihomoSourceManualNode, nil, ""
			input.ManualNode = &domainnetworkaccess.MihomoManualNode{Protocol: domainnetworkaccess.MihomoManualProtocolHTTP, Server: "proxy.example.com", Port: 8080, Username: "alice"}
		},
	} {
		input := valid
		mutate(&input)
		if err := validateMihomoProfileInput(input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Errorf("%s error = %v, want invalid argument", name, err)
		}
	}
}
