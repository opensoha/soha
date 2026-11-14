package openapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEndpointChannelControlTransport(t *testing.T) {
	type server struct {
		URL string `json:"url"`
	}
	type operation struct {
		Servers  []server              `json:"servers"`
		Security []map[string][]string `json:"security"`
	}
	var document struct {
		Servers []server `json:"servers"`
		Paths   map[string]struct {
			Servers []server  `json:"servers"`
			Get     operation `json:"get"`
			Post    operation `json:"post"`
		} `json:"paths"`
		Components struct {
			SecuritySchemes map[string]struct {
				Type string `json:"type"`
			} `json:"securitySchemes"`
		} `json:"components"`
	}
	if err := json.Unmarshal(JSON(), &document); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "/receipt"} {
		path := "/network-control/v1/runtimes/{runtimeID}/channel-controls" + suffix
		t.Run(path, func(t *testing.T) {
			item, ok := document.Paths[path]
			if !ok {
				t.Fatal("missing runtime channel control operation")
			}
			op := item.Get
			if suffix != "" {
				op = item.Post
			}
			servers := op.Servers
			if len(servers) == 0 {
				servers = item.Servers
			}
			if len(servers) == 0 {
				servers = document.Servers
			}
			if len(servers) != 1 || strings.TrimSuffix(servers[0].URL, "/")+path != "/api"+path {
				t.Errorf("effective server %v does not target the independent /api/network-control/v1 listener", servers)
			}
			if len(op.Security) != 1 || len(op.Security[0]) != 1 {
				t.Fatal("runtime operations must explicitly require one certificate scheme, without a Bearer alternative")
			}
			for name, scopes := range op.Security[0] {
				if len(scopes) != 0 || document.Components.SecuritySchemes[name].Type != "mutualTLS" {
					t.Errorf("runtime operation requires %q, want a mutualTLS security scheme", name)
				}
			}
		})
	}
}
