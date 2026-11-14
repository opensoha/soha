package proxysource

import (
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"go.yaml.in/yaml/v3"
	"strings"
)

// Export emits only the reviewed Mihomo source subset; host adds guarded listeners.
func (c *Compiler) Export(ir domain.IR) (string, error) {
	nodes := []map[string]any{}
	targets := []proxypolicy.Target{}
	for i, n := range ir.Nodes {
		if err := normalizeNode(&n, i); err != nil {
			return "", err
		}
		m := map[string]any{"name": n.NodeID, "type": string(n.Protocol), "server": n.Server, "port": n.Port}
		if n.Username != "" {
			m["username"] = n.Username
		}
		if n.Secret != "" {
			m["password"] = n.Secret
		}
		if n.Cipher != "" {
			m["cipher"] = n.Cipher
		}
		if n.Protocol == "vless" || n.Protocol == "vmess" {
			m["uuid"] = n.UUID.String()
		}
		if n.TLS != nil {
			m["tls"] = true
			m["servername"] = n.TLS.ServerName
			if len(n.TLS.Alpn) > 0 {
				m["alpn"] = n.TLS.Alpn
			}
		}
		if n.Transport != nil {
			t := n.Transport
			m["network"] = string(t.Type)
			switch t.Type {
			case "ws":
				ws := map[string]any{"path": t.Path}
				if t.Host != "" {
					ws["headers"] = map[string]string{"Host": t.Host}
				}
				m["ws-opts"] = ws
			case "grpc":
				m["grpc-opts"] = map[string]string{"grpc-service-name": t.ServiceName}
			}
		}
		nodes = append(nodes, m)
		targets = append(targets, proxypolicy.Target{ID: n.NodeID, Kind: "node"})
	}
	groups := []map[string]any{}
	for _, g := range ir.Groups {
		groups = append(groups, map[string]any{"name": g.GroupID, "type": "select", "proxies": g.NodeIDs})
		targets = append(targets, proxypolicy.Target{ID: g.GroupID, Kind: "group"})
	}
	merged, err := proxypolicy.Compose(0, 0, ir.Rules, targets, true)
	if err != nil {
		return "", domain.Invalid("invalid_rules", "rules")
	}
	rules := []string{}
	for _, r := range merged.EffectiveRules {
		if !r.Enabled {
			continue
		}
		line, err := mihomoRule(r)
		if err != nil {
			return "", err
		}
		rules = append(rules, line)
	}
	raw, err := yaml.Marshal(map[string]any{"proxies": nodes, "proxy-groups": groups, "rules": rules})
	if err != nil {
		return "", domain.Invalid("export_failed", "content")
	}
	return string(raw), nil
}
func mihomoRule(r api.NetworkProxyRule) (string, error) {
	var matcher string
	switch r.Matcher.Type {
	case "domain":
		matcher = "DOMAIN," + r.Matcher.Value
	case "domain_suffix":
		matcher = "DOMAIN-SUFFIX," + r.Matcher.Value
	case "cidr":
		matcher = "IP-CIDR," + r.Matcher.Value
		if strings.Contains(r.Matcher.Value, ":") {
			matcher = "IP-CIDR6," + r.Matcher.Value
		}
	case "all":
		matcher = "MATCH"
	default:
		return "", domain.Invalid("unsupported_matcher", "rules")
	}
	action := r.Action.TargetID
	switch r.Action.Type {
	case "reject":
		action = "REJECT"
	case "direct":
		return "", domain.Invalid("direct_protection_unproven", "rules")
	case "node", "group":
	default:
		return "", domain.Invalid("unsupported_action", "rules")
	}
	return matcher + "," + action, nil
}

func (c *Compiler) ExportDirect(ir domain.IR) (string, error) {
	raw, err := c.Export(domain.IR{Nodes: ir.Nodes})
	if err != nil {
		return "", err
	}
	var document map[string]any
	if yaml.Unmarshal([]byte(raw), &document) != nil {
		return "", domain.Invalid("export_failed", "content")
	}
	data, err := yaml.Marshal(map[string]any{"proxies": document["proxies"]})
	if err != nil || len(data) > domain.MaxSourceBytes {
		return "", domain.Invalid("source_limit", "content")
	}
	return string(data), nil
}
