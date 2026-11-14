package proxysource

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"slices"
	"strings"

	"github.com/google/uuid"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxypolicy"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"go.yaml.in/yaml/v3"
)

type Compiler struct{}

func NewCompiler() *Compiler { return &Compiler{} }
func strictJSON(data []byte, dst any) error {
	if err := uniqueJSON(data); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if d.Decode(dst) != nil {
		return domain.Invalid("invalid_or_unknown_field", "content")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return domain.Invalid("invalid_content", "content")
	}
	return nil
}
func (c *Compiler) Compile(input api.NetworkProxySourceInput) (domain.IR, error) {
	if len(input.Content) > domain.MaxSourceBytes {
		return domain.IR{}, domain.Invalid("source_limit", "content")
	}
	ir, err := parseSource(input)
	if err != nil {
		return domain.IR{}, err
	}
	if len(ir.Nodes) == 0 || len(ir.Nodes) > domain.MaxNodes {
		return domain.IR{}, domain.Invalid("node_limit", "nodes")
	}
	seen := map[string]bool{}
	for i := range ir.Nodes {
		if err = normalizeNode(&ir.Nodes[i], i); err != nil {
			return domain.IR{}, err
		}
		if seen[ir.Nodes[i].NodeID] {
			return domain.IR{}, domain.Invalid("duplicate_node", "nodes")
		}
		seen[ir.Nodes[i].NodeID] = true
	}
	targets := []proxypolicy.Target{}
	for _, node := range ir.Nodes {
		targets = append(targets, proxypolicy.Target{ID: node.NodeID, Kind: "node"})
	}
	for _, group := range ir.Groups {
		targets = append(targets, proxypolicy.Target{ID: group.GroupID, Kind: "group"})
	}
	composition, composeErr := proxypolicy.Compose(0, 0, ir.Rules, targets, true)
	if composeErr != nil {
		return domain.IR{}, domain.Invalid("invalid_rules", "rules")
	}
	ir.Rules = composition.EffectiveRules
	if input.DeliveryMode == "mapped_nodes" || input.DeliveryMode == "gateway" {
		for _, rule := range ir.Rules {
			if rule.Action.Type == "direct" {
				return domain.IR{}, domain.Invalid("direct_protection_unproven", "rules")
			}
		}
	}
	if ir.Groups == nil {
		ir.Groups = []api.NetworkProxyGroup{}
	}
	if ir.Rules == nil {
		ir.Rules = []api.NetworkProxyRule{}
	}
	return ir, nil
}
func normalizeNode(n *api.NetworkProxyNode, index int) error {
	p := fmt.Sprintf("nodes[%d]", index)
	if err := validateNodeEndpoint(n, p); err != nil {
		return err
	}
	if err := validateCredentialFields(n, p); err != nil {
		return err
	}
	if err := validateNodeCredentials(n, p); err != nil {
		return err
	}
	if err := validateNodeTLS(n, p); err != nil {
		return err
	}
	if err := validateNodeTransport(n, p); err != nil {
		return err
	}
	// Identity excludes labels and authentication; renaming/rotation preserves mapping.
	identity := struct {
		Protocol  string
		Server    string
		Port      int
		TLS       *api.NetworkProxyTLS
		Transport *api.NetworkProxyTransport
	}{string(n.Protocol), strings.ToLower(n.Server), n.Port, n.TLS, n.Transport}
	raw, _ := json.Marshal(identity)
	sum := sha256.Sum256(raw)
	n.NodeID = "node-" + hex.EncodeToString(sum[:12])
	n.Label = fmt.Sprintf("Node %d", index+1)
	return nil
}

func validateNodeEndpoint(n *api.NetworkProxyNode, p string) error {
	if n.Server == "" || len(n.Server) > 253 || strings.ContainsAny(n.Server, " /\\@?#%\r\n\t") || n.Port < 1 || n.Port > 65535 {
		return domain.Invalid("invalid_endpoint", p)
	}
	if net.ParseIP(n.Server) == nil && strings.Contains(n.Server, ":") {
		return domain.Invalid("invalid_endpoint", p)
	}

	return nil
}

func validateCredentialFields(n *api.NetworkProxyNode, p string) error {
	if n.Cipher != "" && n.Protocol != "ss" {
		return domain.Invalid("unsupported_field", p)
	}
	if n.Username != "" && n.Protocol != "http" && n.Protocol != "socks5" {
		return domain.Invalid("unsupported_field", p)
	}
	if n.Secret != "" && (n.Protocol == "vmess" || n.Protocol == "vless") {
		return domain.Invalid("unsupported_field", p)
	}
	if n.Protocol != "vmess" && n.Protocol != "vless" && n.UUID != uuid.Nil {
		return domain.Invalid("unsupported_field", p)
	}

	return nil
}

func validateNodeCredentials(n *api.NetworkProxyNode, p string) error {
	switch string(n.Protocol) {
	case "http", "socks5":
	case "ss":
		switch n.Cipher {
		case "aes-128-gcm", "aes-256-gcm", "chacha20-ietf-poly1305":
		default:
			return domain.Invalid("unsupported_cipher", p)
		}
		if n.Secret == "" {
			return domain.Invalid("missing_credential", p)
		}
	case "trojan":
		if n.Secret == "" || n.TLS == nil || !n.TLS.Enabled {
			return domain.Invalid("invalid_tls_credential", p)
		}
	case "vmess", "vless":
		if _, err := uuid.Parse(n.UUID.String()); err != nil || n.UUID == uuid.Nil {
			return domain.Invalid("invalid_uuid", p)
		}
	default:
		return domain.Invalid("unsupported_protocol", p)
	}

	return nil
}

func validateNodeTLS(n *api.NetworkProxyNode, p string) error {
	if n.Protocol == "ss" && n.TLS != nil {
		return domain.Invalid("unsupported_tls", p)
	}
	if n.TLS != nil {
		if len(n.TLS.Alpn) > 8 || len(n.TLS.ServerName) > 253 || strings.ContainsAny(n.TLS.ServerName, " /\\@?#%\r\n\t") {
			return domain.Invalid("invalid_tls", p)
		}
		for _, alpn := range n.TLS.Alpn {
			if alpn == "" || len(alpn) > 128 || strings.ContainsAny(alpn, "\r\n\t") {
				return domain.Invalid("invalid_tls", p)
			}
		}
	}
	if n.TLS != nil && (!n.TLS.Enabled || n.TLS.Fingerprint != "") {
		return domain.Invalid("unsupported_tls", p)
	}

	return nil
}

func validateNodeTransport(n *api.NetworkProxyNode, p string) error {
	if n.Transport != nil {
		if len(n.Transport.Host) > 253 || len(n.Transport.Path) > 2048 || len(n.Transport.ServiceName) > 256 || strings.ContainsAny(n.Transport.Host+n.Transport.Path+n.Transport.ServiceName, "\r\n\t") {
			return domain.Invalid("invalid_transport", p)
		}
		if (n.Protocol == "http" || n.Protocol == "socks5" || n.Protocol == "ss") && n.Transport.Type != "tcp" {
			return domain.Invalid("unsupported_transport", p)
		}
		switch string(n.Transport.Type) {
		case "tcp":
			if n.Transport.Host != "" || n.Transport.Path != "" || n.Transport.ServiceName != "" {
				return domain.Invalid("unsupported_transport", p)
			}
		case "ws":
			if n.Transport.ServiceName != "" {
				return domain.Invalid("unsupported_transport", p)
			}
		case "grpc":
			if n.Transport.Host != "" || n.Transport.Path != "" {
				return domain.Invalid("unsupported_transport", p)
			}
		default:
			return domain.Invalid("unsupported_transport", p)
		}
	}

	return nil
}

type mihomoDocument struct {
	Proxies []mihomoNode `yaml:"proxies"`
	Groups  []struct {
		Name    string   `yaml:"name"`
		Type    string   `yaml:"type"`
		Proxies []string `yaml:"proxies"`
	} `yaml:"proxy-groups"`
	Rules []string `yaml:"rules"`
}
type mihomoNode struct {
	Name       string   `yaml:"name"`
	Type       string   `yaml:"type"`
	Server     string   `yaml:"server"`
	Port       int      `yaml:"port"`
	Username   string   `yaml:"username"`
	Password   string   `yaml:"password"`
	Cipher     string   `yaml:"cipher"`
	UUID       string   `yaml:"uuid"`
	TLS        bool     `yaml:"tls"`
	ServerName string   `yaml:"servername"`
	SNI        string   `yaml:"sni"`
	ALPN       []string `yaml:"alpn"`
	Network    string   `yaml:"network"`
	WS         *struct {
		Path    string `yaml:"path"`
		Headers *struct {
			Host string `yaml:"Host"`
		} `yaml:"headers"`
	} `yaml:"ws-opts"`
	GRPC *struct {
		ServiceName string `yaml:"grpc-service-name"`
	} `yaml:"grpc-opts"`
}

func parseMihomo(data []byte) (domain.IR, error) {
	var doc mihomoDocument
	d := yaml.NewDecoder(bytes.NewReader(data))
	d.KnownFields(true)
	if d.Decode(&doc) != nil {
		return domain.IR{}, domain.Invalid("invalid_or_unknown_field", "content")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return domain.IR{}, domain.Invalid("invalid_content", "content")
	}
	ir := domain.IR{}
	names := map[string]string{}
	for i, m := range doc.Proxies {
		n, err := convertMihomoNode(m, i)
		if err != nil {
			return domain.IR{}, err
		}
		if m.Name == "" || names[m.Name] != "" {
			return domain.IR{}, domain.Invalid("duplicate_alias", "nodes")
		}
		names[m.Name] = n.NodeID
		ir.Nodes = append(ir.Nodes, n)
	}
	for i, g := range doc.Groups {
		if g.Type != "select" {
			return domain.IR{}, domain.Invalid("unsupported_group", "groups")
		}
		group := api.NetworkProxyGroup{GroupID: fmt.Sprintf("group-%d", i+1), Label: fmt.Sprintf("Group %d", i+1), Type: "select", NodeIDs: []string{}}
		for _, name := range g.Proxies {
			id := names[name]
			if id == "" {
				return domain.IR{}, domain.Invalid("invalid_target", "groups")
			}
			group.NodeIDs = append(group.NodeIDs, id)
		}
		if len(group.NodeIDs) == 0 || names[g.Name] != "" {
			return domain.IR{}, domain.Invalid("invalid_group", "groups")
		}
		stableNodes := slices.Clone(group.NodeIDs)
		slices.Sort(stableNodes)
		digest := sha256.Sum256([]byte(strings.Join(stableNodes, ",")))
		group.GroupID = "group-" + hex.EncodeToString(digest[:12])
		names[g.Name] = group.GroupID
		ir.Groups = append(ir.Groups, group)
	}
	for i, line := range doc.Rules {
		r, err := parseRule(line, i, names)
		if err != nil {
			return domain.IR{}, err
		}
		ir.Rules = append(ir.Rules, r)
	}
	return ir, nil
}
func parseRule(line string, i int, names map[string]string) (api.NetworkProxyRule, error) {
	r := api.NetworkProxyRule{RuleID: fmt.Sprintf("source-rule-%d", i+1), Order: int64(i), Enabled: true, Source: "organization_default"}
	parts := strings.Split(line, ",")
	if len(parts) < 2 || len(parts) > 3 || i >= 2048 {
		return r, domain.Invalid("unsupported_rule", "rules")
	}
	switch parts[0] {
	case "DOMAIN":
		r.Matcher.Type = "domain"
	case "DOMAIN-SUFFIX":
		r.Matcher.Type = "domain_suffix"
	case "IP-CIDR", "IP-CIDR6":
		r.Matcher.Type = "cidr"
	case "MATCH":
		r.Matcher.Type = "all"
	default:
		return r, domain.Invalid("unsupported_rule", "rules")
	}
	if r.Matcher.Type != "all" {
		if len(parts) != 3 {
			return r, domain.Invalid("unsupported_rule", "rules")
		}
		r.Matcher.Value = parts[1]
	} else if len(parts) != 2 {
		return r, domain.Invalid("unsupported_rule", "rules")
	}
	target := parts[len(parts)-1]
	switch target {
	case "DIRECT":
		r.Action.Type = "direct"
	case "REJECT":
		r.Action.Type = "reject"
	default:
		id := names[target]
		if id == "" {
			return r, domain.Invalid("invalid_target", "rules")
		}
		r.Action.TargetID = id
		r.Action.Type = "node"
		if strings.HasPrefix(id, "group-") {
			r.Action.Type = "group"
		}
	}
	return r, nil
}

// Duplicate keys are rejected before decoding: last-value wins is unsafe for config.
func uniqueJSON(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(decoder, 0); err != nil {
		return domain.Invalid("invalid_or_duplicate_field", "content")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return domain.Invalid("invalid_content", "content")
	}
	return nil
}
func uniqueValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return fmt.Errorf("depth limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate field")
			}
			seen[name] = true
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err = uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid delimiter")
	}
	_, err = decoder.Token()
	return err
}

func convertMihomoNode(m mihomoNode, i int) (api.NetworkProxyNode, error) {
	n := api.NetworkProxyNode{Server: m.Server, Port: m.Port, Protocol: api.NetworkProxyNodeProtocol(m.Type), Username: m.Username, Secret: m.Password, Cipher: m.Cipher}
	if m.UUID != "" {
		u, e := uuid.Parse(m.UUID)
		if e != nil {
			return api.NetworkProxyNode{}, domain.Invalid("invalid_uuid", "nodes")
		}
		n.UUID = u
	}
	if m.ServerName != "" && m.SNI != "" && m.ServerName != m.SNI {
		return api.NetworkProxyNode{}, domain.Invalid("invalid_tls", "nodes")
	}
	if !m.TLS && m.Type != "trojan" && (m.ServerName != "" || m.SNI != "" || len(m.ALPN) > 0) {
		return api.NetworkProxyNode{}, domain.Invalid("invalid_tls", "nodes")
	}
	if m.TLS || m.Type == "trojan" {
		sni := m.ServerName
		if sni == "" {
			sni = m.SNI
		}
		n.TLS = &api.NetworkProxyTLS{Enabled: true, ServerName: sni, Alpn: m.ALPN}
	}
	if err := mihomoTransport(&n, m); err != nil {
		return api.NetworkProxyNode{}, err
	}
	if err := normalizeNode(&n, i); err != nil {
		return api.NetworkProxyNode{}, err
	}

	return n, nil
}

func mihomoTransport(n *api.NetworkProxyNode, m mihomoNode) error {
	if m.Network != "" {
		n.Transport = &api.NetworkProxyTransport{Type: api.NetworkProxyTransportType(m.Network)}
	}
	if m.WS != nil {
		if m.Network != "ws" {
			return domain.Invalid("invalid_transport", "nodes")
		}
		n.Transport.Path = m.WS.Path
		if m.WS.Headers != nil {
			n.Transport.Host = m.WS.Headers.Host
		}
	}
	if m.GRPC != nil {
		if m.Network != "grpc" {
			return domain.Invalid("invalid_transport", "nodes")
		}
		n.Transport.ServiceName = m.GRPC.ServiceName
	}

	return nil
}

func parseSource(input api.NetworkProxySourceInput) (domain.IR, error) {
	var ir domain.IR
	var err error
	switch string(input.Format) {
	case "manual":
		if input.Content != "" || input.URL != "" {
			return ir, domain.Invalid("ambiguous_source", "content")
		}
		ir.Nodes = append([]api.NetworkProxyNode(nil), input.Nodes...)
	case "mihomo_yaml":
		ir, err = parseMihomo([]byte(input.Content))
	case "sing_box_json":
		ir, err = parseSingBox([]byte(input.Content))
	case "v2ray_json":
		ir, err = parseV2Ray([]byte(input.Content))
	case "uri_list":
		ir, err = parseURIs(input.Content)
	default:
		return ir, domain.Invalid("unsupported_format", "format")
	}

	return ir, err
}
