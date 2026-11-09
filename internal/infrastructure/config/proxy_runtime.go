package config

import (
	"fmt"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

type ProxyProbeProfileConfig struct {
	InstanceID string `mapstructure:"instance_id"`
	ProfileID  string `mapstructure:"profile_id"`
}

type ProxyRuntimeMemberConfig struct {
	InstanceID string `mapstructure:"instance_id"`
	RuntimeID  string `mapstructure:"runtime_id"`
	Ordinal    int32  `mapstructure:"ordinal"`
}
type ProxyEmployeeEndpointConfig struct {
	EndpointID string `mapstructure:"endpoint_id"`
	URL        string `mapstructure:"url"`
}
type ProxyRuntimeSlotConfig struct {
	ID                   string                        `mapstructure:"id"`
	ProxyID              string                        `mapstructure:"proxy_id"`
	ExecutorID           string                        `mapstructure:"executor_id"`
	WorkloadKind         string                        `mapstructure:"workload_kind"`
	Members              []ProxyRuntimeMemberConfig    `mapstructure:"members"`
	Endpoints            []ProxyEmployeeEndpointConfig `mapstructure:"endpoints"`
	Namespace            string                        `mapstructure:"namespace"`
	Name                 string                        `mapstructure:"name"`
	UID                  string                        `mapstructure:"uid"`
	TemplateDigest       string                        `mapstructure:"template_digest"`
	MaxReplicas          int32                         `mapstructure:"max_replicas"`
	AllowAdoption        bool                          `mapstructure:"allow_adoption"`
	PreviousOwnerStopped bool                          `mapstructure:"previous_owner_stopped"`
}
type ProxyRuntimeExecutorConfig struct {
	ExecutorID string `mapstructure:"executor_id"`
	TokenFile  string `mapstructure:"token_file"`
}

func (c ProxyRuntimeExecutorConfig) ReadToken() (string, error) {
	return (OutpostRuntimeExecutorConfig{ExecutorID: c.ExecutorID, TokenFile: c.TokenFile}).ReadToken()
}
func (c ProxyRuntimeSlotConfig) Slot() domain.Slot {
	s := domain.Slot{ID: c.ID, ProxyID: c.ProxyID, ExecutorID: c.ExecutorID, WorkloadKind: c.WorkloadKind, Namespace: c.Namespace, Name: c.Name, UID: c.UID, TemplateDigest: c.TemplateDigest, MaxReplicas: c.MaxReplicas, AllowAdoption: c.AllowAdoption, PreviousOwnerStopped: c.PreviousOwnerStopped}
	for _, m := range c.Members {
		s.Members = append(s.Members, domain.Member{InstanceID: m.InstanceID, RuntimeID: m.RuntimeID, Ordinal: m.Ordinal})
	}
	return s
}
func validateProxyRuntimeConfig(c RuntimeConfig) []string {
	problems := validateProxyProbeProfiles(c.ProxyProbeProfiles)
	executors, credentialProblems := validateProxyExecutorCredentials(c)
	problems = append(problems, credentialProblems...)
	return append(problems, validateProxyRuntimeSlots(c.ProxySlots, executors)...)
}

func validateProxyProbeProfiles(profiles []ProxyProbeProfileConfig) []string {
	var problems []string
	identifier := regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
	seen := map[string]bool{}
	for _, p := range profiles {
		key := p.InstanceID + "/" + p.ProfileID
		if !identifier.MatchString(p.InstanceID) || !identifier.MatchString(p.ProfileID) || seen[key] {
			problems = append(problems, "runtime.proxy_probe_profiles requires valid independent instance/profile IDs")
		}
		seen[key] = true
	}
	return problems
}

func validateProxyExecutorCredentials(c RuntimeConfig) (map[string]bool, []string) {
	var problems []string
	executors, tokens := map[string]bool{}, map[string]bool{}
	for _, e := range c.OutpostRuntimeExecutors {
		tokens[filepath.Clean(e.TokenFile)] = true
	}
	for _, e := range c.ProxyRuntimeExecutors {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`).MatchString(e.ExecutorID) || executors[e.ExecutorID] || !filepath.IsAbs(e.TokenFile) || tokens[filepath.Clean(e.TokenFile)] {
			problems = append(problems, "runtime.proxy_runtime_executors requires unique IDs and dedicated absolute token files")
		}
		executors[e.ExecutorID] = true
		tokens[filepath.Clean(e.TokenFile)] = true
	}
	return executors, problems
}

func validateProxyRuntimeSlots(slots []ProxyRuntimeSlotConfig, executors map[string]bool) []string {
	var problems []string
	ids, targets, instances, runtimes, endpoints := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, s := range slots {
		target := s.Namespace + "/" + s.Name
		if !regexp.MustCompile(`^sha256:[a-f0-9]{64}$`).MatchString(s.TemplateDigest) || domain.ValidatePolicy(s.Slot()) != nil || !executors[s.ExecutorID] || ids[s.ID] || targets[target] || (s.PreviousOwnerStopped && !s.AllowAdoption) {
			problems = append(problems, "runtime.proxy_slots requires unique approved workloads, identities, executor and explicit adoption policy")
		}
		ids[s.ID] = true
		targets[target] = true
		problems = append(problems, validateProxyRuntimeMembers(s.Members, instances, runtimes)...)
		problems = append(problems, validateProxyEmployeeEndpoints(s.Endpoints, endpoints)...)
	}
	return problems
}

func validateProxyRuntimeMembers(members []ProxyRuntimeMemberConfig, instances, runtimes map[string]bool) []string {
	var problems []string
	for _, m := range members {
		if instances[m.InstanceID] || runtimes[m.RuntimeID] {
			problems = append(problems, "runtime.proxy_slots members require globally independent identities")
		}
		instances[m.InstanceID] = true
		runtimes[m.RuntimeID] = true
	}
	return problems
}

func validateProxyEmployeeEndpoints(endpoints []ProxyEmployeeEndpointConfig, seen map[string]bool) []string {
	var problems []string
	if len(endpoints) == 0 {
		problems = append(problems, "runtime.proxy_slots requires a verified HTTPS employee endpoint")
	}
	for _, e := range endpoints {
		u, err := url.Parse(e.URL)
		if strings.TrimSpace(e.EndpointID) == "" || seen[e.EndpointID] || err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
			problems = append(problems, fmt.Sprintf("runtime.proxy_slots invalid or duplicate HTTPS endpoint %q", e.EndpointID))
		}
		seen[e.EndpointID] = true
	}
	return problems
}
