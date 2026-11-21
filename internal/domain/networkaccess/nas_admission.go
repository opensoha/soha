package networkaccess

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/opensoha/soha/internal/networkprotocol"
)

type NASAttachment struct {
	CalledStationID string
	NASPortID       string
	NASPort         *int64
}

func AccessNetworkCatalogHash(networks []AccessNetwork) string {
	ordered := slices.Clone(networks)
	slices.SortFunc(ordered, func(left, right AccessNetwork) int { return strings.Compare(left.ID, right.ID) })
	versions := make([][3]any, 0, len(ordered))
	for _, network := range ordered {
		versions = append(versions, [3]any{network, network.Deleted, network.Activated})
	}
	raw, _ := json.Marshal(versions)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

// NASRadiusProfile applies network attributes to full profiles while retaining
// the site isolation mapping for restricted, quarantine, and onboarding profiles.
func NASRadiusProfile(networkID, siteID, profile string, network *networkprotocol.RadiusAttributes, site *SiteProfileBinding) *SiteProfileBinding {
	if network == nil {
		return site
	}
	if site == nil && (profile != ProfileFull || network.SessionTimeoutSeconds < 1 || network.VLANID == 0 && network.FilterID == "") {
		return nil
	}
	result := SiteProfileBinding{SiteID: siteID, AccessProfile: profile}
	if site != nil {
		result = *site
	}
	result.ID = networkID
	if profile == ProfileFull {
		if network.VLANID != 0 {
			result.VLANID = network.VLANID
		}
		if network.FilterID != "" {
			result.FilterID = network.FilterID
		}
	}
	if network.SessionTimeoutSeconds != 0 && (result.SessionTimeoutSeconds == 0 || network.SessionTimeoutSeconds < result.SessionTimeoutSeconds) {
		result.SessionTimeoutSeconds = network.SessionTimeoutSeconds
	}
	return &result
}

func (network AccessNetwork) ManagesNASAdmission() bool {
	return network.Activated || network.Configuration.Enabled && network.Configuration.Kind != AccessNetworkBootstrap
}

func (network AccessNetwork) ActiveNASBinding(binding NASBinding) bool {
	cfg := network.Configuration
	return !network.Deleted && cfg.Enabled && (cfg.Kind == AccessNetworkStandard || cfg.Kind == AccessNetworkStrict && binding.SupportsStrictOnline()) && cfg.SiteID == binding.SiteID &&
		binding.Status == StatusActive && (binding.AccessMedium == "" || cfg.AccessMedium == binding.AccessMedium) &&
		slices.Contains(cfg.NASBindingIDs, binding.ID)
}

func (binding NASBinding) SupportsStrictOnline() bool {
	return binding.StrictOnlineQualified && binding.Status == StatusActive && binding.DisconnectSupported && binding.EAPServerTrust != nil &&
		(binding.EAPServerTrust.EapMethod == "" || binding.EAPServerTrust.EapMethod == "tls")
}

// Even a reauthentication just before the lease expires must fit the 60-second trial target.
const StrictNASReauthenticationSeconds = 25

func (network AccessNetwork) ProfileRevision(profile networkprotocol.AdmissionProfile) string {
	revision := networkprotocol.AdmissionProfileRevision(profile)
	raw := []byte(revision + "\n" + AccessNetworkCatalogHash([]AccessNetwork{network}))
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}

type NASAdmissionSnapshot struct {
	AuthenticationMethod            string
	AccessNetworkID, BindingID      string
	Attachment                      NASAttachment
	AccessNetworks                  []AccessNetwork
	Session                         Session
	Subject                         Subject
	Device                          Device
	Site                            Site
	Binding                         NASBinding
	Policies                        PolicySnapshot
	Profiles                        map[string]SiteProfileBinding
	StrongBinding, CredentialActive bool
	FloorProfile, InputsHash        string
	RadiusAttributes                *networkprotocol.RadiusAttributes
	Revision                        int64
	Attempts                        int
	StrictUI                        StrictUILease
	StrictUIEpoch                   string
}

type NASAdmissionMutation struct {
	Profile, InputsHash, Reason string
	PolicyVersion               int
	RadiusAttributes            *networkprotocol.RadiusAttributes
	Command                     *SessionCommand
	Attempts                    int
}
