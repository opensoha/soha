package networkaccess

import "time"

const (
	AccessNetworkStandard  = "standard"
	AccessNetworkStrict    = "strict_online"
	AccessNetworkBootstrap = "open_guest_bootstrap"
)

type AccessNetwork struct {
	ID            string             `json:"id"`
	Configuration AccessNetworkInput `json:"configuration"`
	CreatedAt     time.Time          `json:"createdAt"`
	UpdatedAt     time.Time          `json:"updatedAt"`
	Deleted       bool               `json:"-"`
	Activated     bool               `json:"-"`
}

type AccessNetworkInput struct {
	SiteID                string              `json:"siteId"`
	Name                  string              `json:"name"`
	AccessMedium          string              `json:"accessMedium"`
	Kind                  string              `json:"kind"`
	Enabled               bool                `json:"enabled"`
	NASBindingIDs         []string            `json:"nasBindingIds"`
	Priority              int                 `json:"priority"`
	AutoConnect           bool                `json:"autoConnect"`
	SSID                  string              `json:"ssid,omitempty"`
	BSSIDs                []string            `json:"bssids,omitempty"`
	PortIDs               []string            `json:"portIds,omitempty"`
	PortStart             *int64              `json:"portStart,omitempty"`
	PortEnd               *int64              `json:"portEnd,omitempty"`
	VLANID                int                 `json:"vlanId,omitempty"`
	FilterID              string              `json:"filterId,omitempty"`
	SessionTimeoutSeconds int                 `json:"sessionTimeoutSeconds,omitempty"`
	Rules                 []AccessNetworkRule `json:"rules"`
}

type AccessNetworkRule struct {
	Effect                string         `json:"effect"`
	Subjects              PolicySubjects `json:"subjects"`
	DeviceIDs             []string       `json:"deviceIds,omitempty"`
	DeviceStatuses        []string       `json:"deviceStatuses,omitempty"`
	PostureStatuses       []string       `json:"postureStatuses,omitempty"`
	AuthenticationMethods []string       `json:"authenticationMethods,omitempty"`
	ScheduleStartMinute   *int           `json:"scheduleStartMinute,omitempty"`
	ScheduleEndMinute     *int           `json:"scheduleEndMinute,omitempty"`
	VLANID                int            `json:"vlanId,omitempty"`
	FilterID              string         `json:"filterId,omitempty"`
	SessionTimeoutSeconds int            `json:"sessionTimeoutSeconds,omitempty"`
}
