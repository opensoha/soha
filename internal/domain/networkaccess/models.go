package networkaccess

import (
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
)

const (
	StatusActive   = "active"
	StatusDisabled = "disabled"

	DeviceStatusActive      = "active"
	DeviceStatusPending     = "pending"
	DeviceStatusQuarantined = "quarantined"
	DeviceStatusRevoked     = "revoked"

	PostureCompliant    = "compliant"
	PostureNoncompliant = "non_compliant"
	PostureUnknown      = "unknown"

	DeviceTypeDesktop = "desktop"
	DeviceTypeLaptop  = "laptop"
	DeviceTypeServer  = "server"
	DeviceTypeMobile  = "mobile"
	DeviceTypeTablet  = "tablet"
	DeviceTypeVirtual = "virtual"
	DeviceTypeUnknown = "unknown"

	DeviceOwnershipCompany    = "company"
	DeviceOwnershipPersonal   = "personal"
	DeviceOwnershipTemporary  = "temporary"
	DeviceOwnershipUnassigned = "unassigned"

	NetworkInterfaceKindPhysical = "physical"
	NetworkInterfaceKindVirtual  = "virtual"
	NetworkInterfaceKindLoopback = "loopback"
	NetworkInterfaceKindUnknown  = "unknown"

	NetworkInterfaceStatusUp      = "up"
	NetworkInterfaceStatusDown    = "down"
	NetworkInterfaceStatusUnknown = "unknown"

	AccessMediumWiFi                    = "wifi"
	AccessMediumWired                   = "wired"
	ConnectionAuthenticationRadius8021X = "radius_802_1x"

	AccessDeviceTypeWirelessController = "wireless_controller"
	AccessDeviceTypeAccessPoint        = "access_point"
	AccessDeviceTypeSwitch             = "switch"
	AccessDeviceTypeOther              = "other"

	GatewayOnline   = "online"
	GatewayDegraded = "degraded"
	GatewayOffline  = "offline"

	GatewayRoutingRouted = "routed"
	GatewayRoutingSNAT   = "snat"

	ModeInternalDirect     = "internal_direct"
	ModeInternalZTNA       = "internal_ztna"
	ModeExternalVPN        = "external_vpn"
	ModeExternalVPNZTNA    = "external_vpn_ztna"
	ModeExternalDirectZTNA = "external_direct_ztna"

	PathAutomatic     = "automatic"
	PathSiteDirect    = "site_direct"
	PathAccessProxy   = "access_proxy"
	PathWireGuard     = "wireguard"
	PathWireGuardZTNA = "wireguard_ztna"
	PathDeny          = "deny"

	DecisionAllow = "allow"
	DecisionDeny  = "deny"

	ProfileOnboarding = "onboarding"
	ProfileFull       = "full"
	ProfileRestricted = "restricted"
	ProfileQuarantine = "quarantine"
	ProfileDeny       = "deny"

	PolicyEffectAllow = "allow"
	PolicyEffectDeny  = "deny"

	ConflictSourceNetworkSpace     = "network_space"
	ConflictSourceNetworkResource  = "network_resource"
	ConflictSourceLAN              = "lan"
	ConflictSourceWireGuardOverlay = "wireguard_overlay"
	ConflictSourceContainer        = "container"
	ConflictSourceKubernetesPod    = "kubernetes_pod"
	ConflictSourceKubernetesSvc    = "kubernetes_service"
	ConflictSourceReserved         = "rfc6598"
	ConflictSourceMihomoFakeIP     = "mihomo_fake_ip"

	ConflictDuplicate = "duplicate"
	ConflictOverlap   = "overlap"

	SessionActionCoA        = "coa"
	SessionActionDisconnect = "disconnect"

	SessionCommandPending     = "pending"
	SessionCommandDelivered   = "delivered"
	SessionCommandApplied     = "applied"
	SessionCommandRejected    = "rejected"
	SessionCommandUnsupported = "unsupported"
	SessionCommandTimedOut    = "timed-out"
	SessionCommandExpired     = "expired"

	MihomoModeManagedFollow         = "managed_follow"
	MihomoModeAppSubscription       = "app_subscription"
	MihomoSourceManagedSubscription = "managed_subscription"
	MihomoSourceManualNode          = "manual_node"
	MihomoManualProtocolHTTP        = "http"
	MihomoManualProtocolHTTPS       = "https"
	MihomoManualProtocolSOCKS5      = "socks5"
	MihomoDNSDisabled               = "disabled"
	MihomoDNSFakeIP                 = "fake_ip"
)

type Site struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Location    string    `json:"location,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type SiteInput struct {
	Name        string
	Description string
	Location    string
	Status      string
}

type Space struct {
	ID        string    `json:"id"`
	SiteID    string    `json:"siteId"`
	Name      string    `json:"name"`
	CIDRs     []string  `json:"cidrs"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type SpaceInput struct {
	SiteID string
	Name   string
	CIDRs  []string
	Status string
}

type Resource struct {
	ID        string    `json:"id"`
	SpaceID   string    `json:"spaceId"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"`
	Target    string    `json:"target"`
	Protocol  string    `json:"protocol"`
	Ports     []int     `json:"ports,omitempty"`
	Protected bool      `json:"protected"`
	PathMode  string    `json:"pathMode"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ResourceInput struct {
	SpaceID   string
	Name      string
	Kind      string
	Target    string
	Protocol  string
	Ports     []int
	Protected bool
	PathMode  string
}

type Device struct {
	ID                   string               `json:"id"`
	OwnerUserID          string               `json:"ownerUserId"`
	Name                 string               `json:"name"`
	Hostname             string               `json:"hostname,omitempty"`
	Platform             string               `json:"platform"`
	DeviceType           string               `json:"deviceType,omitempty"`
	OwnershipType        string               `json:"ownershipType,omitempty"`
	SiteID               string               `json:"siteId,omitempty"`
	Status               string               `json:"status"`
	PostureStatus        string               `json:"postureStatus,omitempty"`
	PostureVersion       int                  `json:"postureVersion"`
	CredentialGeneration int                  `json:"credentialGeneration,omitempty"`
	LastSeenAt           *time.Time           `json:"lastSeenAt,omitempty"`
	ReportedFacts        *DeviceReportedFacts `json:"reportedFacts,omitempty"`
	CreatedAt            time.Time            `json:"createdAt"`
	UpdatedAt            time.Time            `json:"updatedAt"`
}

type DeviceInput struct {
	Name          string
	SiteID        string
	Status        string
	PostureStatus string
	DeviceType    string
	OwnershipType string
}

type DeviceRegistrationInput struct {
	Name          string
	Hostname      string
	Platform      string
	DeviceType    string
	ReportedFacts *DeviceReportedFacts
}

type DeviceReportedFacts struct {
	OSName            string                   `json:"osName,omitempty"`
	OSVersion         string                   `json:"osVersion,omitempty"`
	OSBuild           string                   `json:"osBuild,omitempty"`
	Architecture      string                   `json:"architecture"`
	Manufacturer      string                   `json:"manufacturer,omitempty"`
	Model             string                   `json:"model,omitempty"`
	SerialNumber      string                   `json:"serialNumber,omitempty"`
	AgentVersion      string                   `json:"agentVersion"`
	CollectedAt       time.Time                `json:"collectedAt"`
	NetworkInterfaces []DeviceNetworkInterface `json:"networkInterfaces"`
}

type DeviceNetworkInterface struct {
	Name          string   `json:"name"`
	DisplayName   string   `json:"displayName,omitempty"`
	Kind          string   `json:"kind"`
	Status        string   `json:"status"`
	MACAddress    string   `json:"macAddress,omitempty"`
	IPv4Addresses []string `json:"ipv4Addresses"`
	IPv6Addresses []string `json:"ipv6Addresses"`
	DNSServers    []string `json:"dnsServers,omitempty"`
}

type DeviceFilter struct {
	Search      string
	OwnerUserID string
	SiteID      string
	Status      string
	Limit       int
}

type Gateway struct {
	Region                     string     `json:"region"`
	ProviderCode               string     `json:"providerCode"`
	ProviderName               string     `json:"providerName"`
	SelectionPriority          int        `json:"selectionPriority"`
	AcceptNewConnections       bool       `json:"acceptNewConnections"`
	MaxSessions                int        `json:"maxSessions"`
	ProbeURL                   string     `json:"probeURL,omitempty"`
	ID                         string     `json:"id"`
	RuntimeID                  string     `json:"runtimeId"`
	SiteID                     string     `json:"siteId"`
	Name                       string     `json:"name"`
	AdministrativeStatus       string     `json:"administrativeStatus"`
	Status                     string     `json:"status"`
	PublicEndpointHost         string     `json:"publicEndpointHost"`
	PublicEndpointPort         int        `json:"publicEndpointPort"`
	OverlayCIDR                string     `json:"overlayCidr"`
	RoutingMode                string     `json:"routingMode"`
	HubGatewayID               string     `json:"hubGatewayId,omitempty"`
	AdvertisedCIDRs            []string   `json:"advertisedCidrs"`
	MTU                        int        `json:"mtu"`
	PersistentKeepaliveSeconds int        `json:"persistentKeepaliveSeconds"`
	DNSServers                 []string   `json:"dnsServers"`
	WireGuardPublicKey         string     `json:"wireguardPublicKey,omitempty"`
	Version                    string     `json:"version,omitempty"`
	Capabilities               []string   `json:"capabilities,omitempty"`
	PolicyVersion              int        `json:"policyVersion,omitempty"`
	AppliedAt                  *time.Time `json:"appliedAt,omitempty"`
	LastHeartbeatAt            *time.Time `json:"lastHeartbeatAt,omitempty"`
	CreatedAt                  time.Time  `json:"createdAt"`
	UpdatedAt                  time.Time  `json:"updatedAt"`
}

type GatewayInput struct {
	Region                     *string
	ProviderCode               *string
	ProviderName               *string
	SelectionPriority          *int
	AcceptNewConnections       *bool
	MaxSessions                *int
	ProbeURL                   *string
	RuntimeID                  string
	SiteID                     string
	Name                       string
	AdministrativeStatus       string
	PublicEndpointHost         string
	PublicEndpointPort         int
	OverlayCIDR                string
	RoutingMode                string
	HubGatewayID               string
	AdvertisedCIDRs            []string
	MTU                        int
	PersistentKeepaliveSeconds int
	DNSServers                 []string
}

type MihomoProfile struct {
	ID                        string    `json:"id"`
	DeviceID                  string    `json:"deviceId"`
	Name                      string    `json:"name"`
	Mode                      string    `json:"mode"`
	SourceType                string    `json:"sourceType,omitempty"`
	Status                    string    `json:"status"`
	SubscriptionConfigured    bool      `json:"subscriptionConfigured"`
	SubscriptionURLCiphertext string    `json:"-"`
	ManualNodeConfigured      bool      `json:"manualNodeConfigured"`
	ManualNodeCiphertext      string    `json:"-"`
	Revision                  int       `json:"revision"`
	MixedPort                 int       `json:"mixedPort"`
	ControllerPort            int       `json:"controllerPort"`
	DNSMode                   string    `json:"dnsMode"`
	FakeIPRange               string    `json:"fakeIpRange,omitempty"`
	SelectorGroup             string    `json:"selectorGroup"`
	SelectedProxy             string    `json:"selectedProxy,omitempty"`
	BypassCIDRs               []string  `json:"bypassCidrs"`
	BypassHosts               []string  `json:"bypassHosts"`
	FailClosed                bool      `json:"failClosed"`
	CreatedAt                 time.Time `json:"createdAt"`
	UpdatedAt                 time.Time `json:"updatedAt"`
}

type MihomoProfileInput struct {
	DeviceID        string
	Name            string
	Mode            string
	SourceType      string
	Status          string
	SubscriptionURL *string
	ManualNode      *MihomoManualNode
	MixedPort       int
	ControllerPort  int
	DNSMode         string
	FakeIPRange     string
	SelectorGroup   string
	SelectedProxy   string
	BypassCIDRs     []string
	BypassHosts     []string
	FailClosed      bool
}

type MihomoManualNode struct {
	Protocol string `json:"protocol"`
	Server   string `json:"server"`
	Port     int    `json:"port"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type MihomoProfileFilter struct {
	Search   string
	DeviceID string
	Mode     string
	Status   string
	Limit    int
}

type NASBinding struct {
	ID                  string    `json:"id"`
	NASID               string    `json:"nasId"`
	RuntimeID           string    `json:"runtimeId"`
	SiteID              string    `json:"siteId"`
	Name                string    `json:"name"`
	AccessMedium        string    `json:"accessMedium,omitempty"`
	DeviceType          string    `json:"deviceType,omitempty"`
	SSID                string    `json:"ssid,omitempty"`
	ManagementAddress   string    `json:"managementAddress,omitempty"`
	Status              string    `json:"status"`
	CoASupported        bool      `json:"coaSupported"`
	DisconnectSupported bool      `json:"disconnectSupported"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type NASBindingInput struct {
	NASID               string
	RuntimeID           string
	SiteID              string
	Name                string
	AccessMedium        string
	DeviceType          string
	SSID                string
	ManagementAddress   string
	Status              string
	CoASupported        bool
	DisconnectSupported bool
}

type NASBindingFilter struct {
	SiteID    string
	RuntimeID string
	Status    string
	Limit     int
}

type ConnectionOption struct {
	SiteID         string `json:"siteId"`
	SiteName       string `json:"siteName"`
	AccessMedium   string `json:"accessMedium"`
	SSID           string `json:"ssid,omitempty"`
	Authentication string `json:"authentication"`
	AccessProfile  string `json:"accessProfile"`
	PolicyVersion  int    `json:"policyVersion"`
}

type SiteProfileBinding struct {
	ID                    string    `json:"id"`
	SiteID                string    `json:"siteId"`
	AccessProfile         string    `json:"accessProfile"`
	VLANID                int       `json:"vlanId,omitempty"`
	FilterID              string    `json:"filterId,omitempty"`
	SessionTimeoutSeconds int       `json:"sessionTimeoutSeconds"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type SiteProfileBindingInput struct {
	SiteID                string
	AccessProfile         string
	VLANID                int
	FilterID              string
	SessionTimeoutSeconds int
}

type SiteProfileBindingFilter struct {
	SiteID        string
	AccessProfile string
	Limit         int
}

type Session struct {
	ID               string    `json:"id"`
	RuntimeID        string    `json:"-"`
	SubjectID        string    `json:"subjectId"`
	DeviceID         string    `json:"deviceId"`
	SiteID           string    `json:"siteId,omitempty"`
	GatewayID        string    `json:"gatewayId,omitempty"`
	NASID            string    `json:"nasId,omitempty"`
	Mode             string    `json:"mode"`
	Path             string    `json:"path"`
	AccessProfile    string    `json:"accessProfile"`
	Status           string    `json:"status"`
	PolicyVersion    int       `json:"policyVersion"`
	NetworkLeaseIDs  []string  `json:"networkLeaseIds"`
	ResourceLeaseIDs []string  `json:"resourceLeaseIds"`
	ReasonCode       string    `json:"reasonCode,omitempty"`
	StartedAt        time.Time `json:"startedAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
	UpdatedAt        time.Time `json:"-"`
}

type SessionFilter struct {
	SiteID    string
	RuntimeID string
	SubjectID string
	DeviceID  string
	Status    string
	Limit     int
}

type SessionActionInput struct {
	Action              string
	TargetAccessProfile string
	ReasonCode          string
	PlanHash            string
}

type SessionActionPlan struct {
	SessionID            string                            `json:"sessionId"`
	RuntimeID            string                            `json:"runtimeId"`
	NASID                string                            `json:"nasId"`
	RequestedAction      string                            `json:"requestedAction"`
	EffectiveAction      string                            `json:"effectiveAction"`
	CurrentAccessProfile string                            `json:"currentAccessProfile"`
	TargetAccessProfile  string                            `json:"targetAccessProfile"`
	ReasonCode           string                            `json:"reasonCode"`
	WillDisconnect       bool                              `json:"willDisconnect"`
	CommandExpiresAt     time.Time                         `json:"commandExpiresAt"`
	PlanHash             string                            `json:"planHash"`
	RadiusAttributes     *networkprotocol.RadiusAttributes `json:"-"`
}

type SessionCommand struct {
	ID                  string                            `json:"id"`
	SessionID           string                            `json:"sessionId"`
	RuntimeID           string                            `json:"runtimeId"`
	NASID               string                            `json:"nasId"`
	SubjectID           string                            `json:"-"`
	DeviceID            string                            `json:"-"`
	Action              string                            `json:"action"`
	TargetAccessProfile string                            `json:"targetAccessProfile"`
	PolicyVersion       int                               `json:"policyVersion"`
	Status              string                            `json:"status"`
	ReasonCode          string                            `json:"reasonCode"`
	PlanHash            string                            `json:"-"`
	RadiusAttributes    *networkprotocol.RadiusAttributes `json:"-"`
	EffectiveAt         time.Time                         `json:"effectiveAt"`
	ExpiresAt           time.Time                         `json:"expiresAt"`
	CompletedAt         *time.Time                        `json:"completedAt,omitempty"`
	CreatedAt           time.Time                         `json:"createdAt"`
}

type SiteFilter struct {
	Search string
	Status string
	Limit  int
}

type SpaceFilter struct {
	Search string
	SiteID string
	Status string
	Limit  int
}

type ResourceFilter struct {
	Search    string
	SpaceID   string
	Kind      string
	Protected *bool
	Limit     int
}

type GatewayFilter struct {
	Search string
	SiteID string
	Status string
	Limit  int
}

type Subject struct {
	UserID string   `json:"userId"`
	Status string   `json:"status"`
	Teams  []string `json:"teams"`
	Tags   []string `json:"tags"`
}

type PolicySubjects struct {
	Users []string `json:"users"`
	Teams []string `json:"teams"`
	Tags  []string `json:"tags"`
}

type Policy struct {
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	Enabled         bool           `json:"enabled"`
	Priority        int            `json:"priority"`
	Effect          string         `json:"effect"`
	Subjects        PolicySubjects `json:"subjects"`
	SiteIDs         []string       `json:"siteIds"`
	ResourceIDs     []string       `json:"resourceIds"`
	Modes           []string       `json:"modes"`
	DeviceStatuses  []string       `json:"deviceStatuses"`
	PostureStatuses []string       `json:"postureStatuses"`
	AccessProfile   string         `json:"accessProfile"`
	Version         int            `json:"version"`
	CreatedAt       time.Time      `json:"createdAt"`
	UpdatedAt       time.Time      `json:"updatedAt"`
}

type PolicyInput struct {
	Name            string
	Enabled         bool
	Priority        int
	Effect          string
	Subjects        PolicySubjects
	SiteIDs         []string
	ResourceIDs     []string
	Modes           []string
	DeviceStatuses  []string
	PostureStatuses []string
	AccessProfile   string
}

type PolicyFilter struct {
	Search  string
	Enabled *bool
	Effect  string
	Limit   int
}

type PolicySnapshot struct {
	PolicyVersion          int       `json:"policyVersion"`
	ContentHash            string    `json:"contentHash"`
	PolicyCount            int       `json:"policyCount"`
	ProtectedResourceCount int       `json:"protectedResourceCount"`
	PublishedAt            time.Time `json:"publishedAt"`
	Policies               []Policy  `json:"-"`
	ProtectedResourceIDs   []string  `json:"-"`
}

type ConflictRange struct {
	SourceType string `json:"sourceType"`
	SourceID   string `json:"sourceId,omitempty"`
	Name       string `json:"name"`
	CIDR       string `json:"cidr"`
}

type Conflict struct {
	Left   ConflictRange `json:"left"`
	Right  ConflictRange `json:"right"`
	Reason string        `json:"reason"`
}

type ConflictAnalysis struct {
	Valid          bool       `json:"valid"`
	RangesAnalyzed int        `json:"rangesAnalyzed"`
	Conflicts      []Conflict `json:"conflicts"`
	Warnings       []string   `json:"warnings"`
}

type PolicyPreview struct {
	Decision              string   `json:"decision"`
	Path                  string   `json:"path"`
	NetworkProfile        string   `json:"networkProfile,omitempty"`
	PolicyVersion         int      `json:"policyVersion"`
	Protected             bool     `json:"protected"`
	NetworkLeaseRequired  bool     `json:"networkLeaseRequired"`
	ResourceLeaseRequired bool     `json:"resourceLeaseRequired"`
	Reasons               []string `json:"reasons"`
}
