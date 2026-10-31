package networkprotocol

import "time"

const CapabilitySiteDNS = "site-dns-v1"

type DNSResolutionRequest struct {
	ResourceID string   `json:"resourceId"`
	SiteID     string   `json:"siteId"`
	Hostname   string   `json:"hostname"`
	Resolvers  []string `json:"resolvers"`
}

type DNSResolutionObservation struct {
	ResourceID string    `json:"resourceId"`
	Hostname   string    `json:"hostname"`
	Addresses  []string  `json:"addresses"`
	ObservedAt time.Time `json:"observedAt"`
	TTLSeconds int       `json:"ttlSeconds"`
}

type DNSObservationBatch struct {
	ConfigurationVersion int                        `json:"configurationVersion"`
	PolicyVersion        int                        `json:"policyVersion"`
	Observations         []DNSResolutionObservation `json:"observations"`
}
