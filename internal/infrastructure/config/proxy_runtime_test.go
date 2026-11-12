package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func proxyFixture() RuntimeConfig {
	return RuntimeConfig{
		ProxyRuntimeExecutors: []ProxyRuntimeExecutorConfig{{ExecutorID: "executor", TokenFile: "/private/proxy-token"}},
		ProxySlots:            []ProxyRuntimeSlotConfig{{ID: "slot", ProxyID: "master", ExecutorID: "executor", WorkloadKind: "StatefulSet", Namespace: "default", Name: "proxy", UID: "live-uid", TemplateDigest: "sha256:" + strings.Repeat("a", 64), MaxReplicas: 1, Members: []ProxyRuntimeMemberConfig{{InstanceID: "proxy-0", RuntimeID: "runtime", Ordinal: 0}}, Endpoints: []ProxyEmployeeEndpointConfig{{EndpointID: "ingress", URL: "https://proxy.example"}}}},
	}
}
func TestProxyRuntimeConfig(t *testing.T) {
	if p := validateProxyRuntimeConfig(proxyFixture()); len(p) != 0 {
		t.Fatal(p)
	}
	cases := map[string]func(*RuntimeConfig){
		"duplicate runtime across workloads": func(c *RuntimeConfig) {
			other := c.ProxySlots[0]
			other.ID = "other"
			other.Name = "other"
			other.Members = []ProxyRuntimeMemberConfig{{InstanceID: "other-0", RuntimeID: "runtime", Ordinal: 0}}
			other.Endpoints = []ProxyEmployeeEndpointConfig{{EndpointID: "other", URL: "https://other.example"}}
			c.ProxySlots = append(c.ProxySlots, other)
		},
		"duplicate endpoint": func(c *RuntimeConfig) {
			c.ProxySlots[0].Endpoints = append(c.ProxySlots[0].Endpoints, c.ProxySlots[0].Endpoints[0])
		},
		"duplicate slot": func(c *RuntimeConfig) { c.ProxySlots = append(c.ProxySlots, c.ProxySlots[0]) },
		"shared token": func(c *RuntimeConfig) {
			c.OutpostRuntimeExecutors = []OutpostRuntimeExecutorConfig{{ExecutorID: "outpost", TokenFile: c.ProxyRuntimeExecutors[0].TokenFile}}
		},
		"missing executor":     func(c *RuntimeConfig) { c.ProxyRuntimeExecutors = nil },
		"wrong ordinal":        func(c *RuntimeConfig) { c.ProxySlots[0].Members[0].InstanceID = "proxy-1" },
		"member ceiling":       func(c *RuntimeConfig) { c.ProxySlots[0].MaxReplicas = 2 },
		"unsafe endpoint":      func(c *RuntimeConfig) { c.ProxySlots[0].Endpoints[0].URL = "http://proxy.example" },
		"endpoint credentials": func(c *RuntimeConfig) { c.ProxySlots[0].Endpoints[0].URL = "https://user@proxy.example" },
		"endpoint path":        func(c *RuntimeConfig) { c.ProxySlots[0].Endpoints[0].URL = "https://proxy.example/path" },
		"no endpoint":          func(c *RuntimeConfig) { c.ProxySlots[0].Endpoints = nil },
		"implicit adoption":    func(c *RuntimeConfig) { c.ProxySlots[0].PreviousOwnerStopped = true },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c := proxyFixture()
			mutate(&c)
			if len(validateProxyRuntimeConfig(c)) == 0 {
				t.Fatal("accepted invalid policy")
			}
		})
	}
}
func TestProxyRuntimeReadToken(t *testing.T) {
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte("01234567890123456789012345678901"), 0600); err != nil {
		t.Fatal(err)
	}
	c := ProxyRuntimeExecutorConfig{TokenFile: p}
	if _, err := c.ReadToken(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadToken(); err == nil {
		t.Fatal("accepted public token")
	}
}

func TestProxyRuntimeProbeProfiles(t *testing.T) {
	c := proxyFixture()
	c.ProxyProbeProfiles = []ProxyProbeProfileConfig{{InstanceID: "runtime-0", ProfileID: "known-profile"}}
	if p := validateProxyRuntimeConfig(c); len(p) != 0 {
		t.Fatal(p)
	}
	c.ProxyProbeProfiles = append(c.ProxyProbeProfiles, c.ProxyProbeProfiles[0])
	if len(validateProxyRuntimeConfig(c)) == 0 {
		t.Fatal("duplicate profile accepted")
	}
	c.ProxyProbeProfiles = []ProxyProbeProfileConfig{{InstanceID: "runtime-0", ProfileID: "https://arbitrary.example"}}
	if len(validateProxyRuntimeConfig(c)) == 0 {
		t.Fatal("URL accepted as profile ID")
	}
}
func TestProxyRuntimeNetworkControlCatalog(t *testing.T) {
	path := writeRuntimeConfig(t, `
runtime:
  proxy_runtime_executors:
    - executor_id: executor
      token_file: /private/proxy-token
  proxy_slots:
    - id: slot
      proxy_id: master
      executor_id: executor
      workload_kind: StatefulSet
      namespace: default
      name: proxy
      uid: actual-uid
      template_digest: sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      max_replicas: 1
      members:
        - instance_id: proxy-0
          runtime_id: runtime
          ordinal: 0
      endpoints:
        - endpoint_id: ingress
          url: https://proxy.example
`)
	t.Setenv("SOHA_CONFIG_FILE", path)
	c, err := loadNetworkRuntimeConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.NetworkControl.ProxySlots) != 1 || c.NetworkControl.ProxySlots[0].Members[0].RuntimeID != "runtime" {
		t.Fatal("top-level catalog was not propagated")
	}
}
