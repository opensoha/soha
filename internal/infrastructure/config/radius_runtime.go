package config

import (
	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"path/filepath"
)

type RadiusRuntimeExecutorConfig struct {
	ExecutorID string `mapstructure:"executor_id"`
	TokenFile  string `mapstructure:"token_file"`
}

func (c RadiusRuntimeExecutorConfig) ReadToken() (string, error) {
	return (OutpostRuntimeExecutorConfig{ExecutorID: c.ExecutorID, TokenFile: c.TokenFile}).ReadToken()
}

type RadiusRuntimeMemberConfig struct {
	MemberID      string `mapstructure:"member_id"`
	RuntimeID     string `mapstructure:"runtime_id"`
	Ordinal       int32  `mapstructure:"ordinal"`
	Endpoint      string `mapstructure:"endpoint"`
	CredentialRef string `mapstructure:"credential_ref"`
}
type RadiusEAPTrustConfig struct {
	ServerNames      []string `mapstructure:"server_names"`
	CACertificates   []string `mapstructure:"ca_certificates"`
	WifiSecurity     string   `mapstructure:"wifi_security"`
	EAPMethod        string   `mapstructure:"eap_method"`
	InnerMethod      string   `mapstructure:"inner_method"`
	CredentialSource string   `mapstructure:"credential_source"`
}
type RadiusConfigurationConfig struct {
	Reference               string               `mapstructure:"reference"`
	TreeHash                string               `mapstructure:"tree_hash"`
	NASSharedSecretRef      string               `mapstructure:"nas_shared_secret_ref"`
	EAPServerCertificateRef string               `mapstructure:"eap_server_certificate_ref"`
	EAPServerKeyRef         string               `mapstructure:"eap_server_key_ref"`
	EAPCARef                string               `mapstructure:"eap_ca_ref"`
	HooksAttested           bool                 `mapstructure:"hooks_attested"`
	EAPServerTrust          RadiusEAPTrustConfig `mapstructure:"eap_server_trust"`
}
type RadiusRuntimeSlotConfig struct {
	ID             string                      `mapstructure:"id"`
	SiteID         string                      `mapstructure:"site_id"`
	NASBindingID   string                      `mapstructure:"nas_binding_id"`
	NASID          string                      `mapstructure:"nas_id"`
	NASName        string                      `mapstructure:"nas_name"`
	AccessMedium   string                      `mapstructure:"access_medium"`
	SSID           string                      `mapstructure:"ssid"`
	ExecutorID     string                      `mapstructure:"executor_id"`
	Namespace      string                      `mapstructure:"namespace"`
	Name           string                      `mapstructure:"name"`
	UID            string                      `mapstructure:"uid"`
	WorkloadKind   string                      `mapstructure:"workload_kind"`
	TemplateDigest string                      `mapstructure:"template_digest"`
	ServiceAccount string                      `mapstructure:"service_account"`
	MaxReplicas    int32                       `mapstructure:"max_replicas"`
	Members        []RadiusRuntimeMemberConfig `mapstructure:"members"`
	Configurations []RadiusConfigurationConfig `mapstructure:"configurations"`
}

func (c RadiusRuntimeSlotConfig) Slot() domain.Slot {
	s := domain.Slot{ID: c.ID, SiteID: c.SiteID, NASBindingID: c.NASBindingID, NASID: c.NASID, NASName: c.NASName, AccessMedium: c.AccessMedium, SSID: c.SSID, ExecutorID: c.ExecutorID, Namespace: c.Namespace, Name: c.Name, UID: c.UID, WorkloadKind: c.WorkloadKind, TemplateDigest: c.TemplateDigest, ServiceAccount: c.ServiceAccount, MaxReplicas: c.MaxReplicas}
	for _, m := range c.Members {
		s.Members = append(s.Members, domain.Member{MemberID: m.MemberID, RuntimeID: m.RuntimeID, Ordinal: m.Ordinal, Endpoint: m.Endpoint, CredentialRef: m.CredentialRef})
	}
	for _, v := range c.Configurations {
		t := v.EAPServerTrust
		s.Configurations = append(s.Configurations, domain.Configuration{Material: api.NetworkRADIUSConfiguration{Reference: v.Reference, TreeHash: v.TreeHash, NasSharedSecretRef: v.NASSharedSecretRef, EapServerCertificateRef: v.EAPServerCertificateRef, EapServerKeyRef: v.EAPServerKeyRef, EapCARef: v.EAPCARef, HooksAttested: v.HooksAttested}, EAPServerTrust: api.NetworkEAPServerTrust{ServerNames: t.ServerNames, CaCertificates: t.CACertificates, WifiSecurity: api.NetworkEAPServerTrustWifiSecurity(t.WifiSecurity), EapMethod: api.NetworkEAPServerTrustEapMethod(t.EAPMethod), InnerMethod: t.InnerMethod, CredentialSource: t.CredentialSource}})
	}
	return s
}
func validateRadiusRuntimeConfig(runtime RuntimeConfig) []string {
	problems := []string{}
	seen := map[string]bool{}
	executors := map[string]bool{}
	for _, e := range runtime.OutpostRuntimeExecutors {
		seen["token:"+filepath.Clean(e.TokenFile)] = true
	}
	for _, e := range runtime.ProxyRuntimeExecutors {
		seen["token:"+filepath.Clean(e.TokenFile)] = true
	}
	for _, e := range runtime.RadiusRuntimeExecutors {
		key := "token:" + filepath.Clean(e.TokenFile)
		if e.ExecutorID == "" || executors[e.ExecutorID] || !filepath.IsAbs(e.TokenFile) || seen[key] {
			problems = append(problems, "runtime.radius_runtime_executors requires independent IDs and dedicated absolute token files")
		}
		executors[e.ExecutorID] = true
		seen[key] = true
	}
	for _, slot := range runtime.ProxySlots {
		seen["uid:"+slot.UID] = true
	}
	for _, slot := range runtime.OutpostSlots {
		seen["uid:"+slot.UID] = true
	}
	for _, slot := range runtime.RadiusSlots {
		if domain.ValidateSlot(slot.Slot()) != nil || !executors[slot.ExecutorID] || seen["slot:"+slot.ID] || seen["uid:"+slot.UID] {
			problems = append(problems, "runtime.radius_slots requires unique approved policy, scope, workload UID and dedicated executor")
		}
		seen["slot:"+slot.ID], seen["uid:"+slot.UID] = true, true
		for _, m := range slot.Members {
			if seen["member:"+m.RuntimeID] {
				problems = append(problems, "runtime.radius_slots member runtime IDs must be independent")
			}
			seen["member:"+m.RuntimeID] = true
		}
	}
	return problems
}
