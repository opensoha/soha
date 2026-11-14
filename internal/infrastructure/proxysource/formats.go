package proxysource

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
)

type boxOutbound struct {
	Type     string `json:"type"`
	Tag      string `json:"tag"`
	Server   string `json:"server"`
	Port     int    `json:"server_port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Method   string `json:"method"`
	UUID     string `json:"uuid"`
	TLS      *struct {
		Enabled    bool     `json:"enabled"`
		ServerName string   `json:"server_name"`
		ALPN       []string `json:"alpn"`
	} `json:"tls"`
	Transport *struct {
		Type        string `json:"type"`
		Path        string `json:"path"`
		ServiceName string `json:"service_name"`
		Headers     *struct {
			Host string `json:"Host"`
		} `json:"headers"`
	} `json:"transport"`
}

func parseSingBox(data []byte) (domain.IR, error) {
	var doc struct {
		Outbounds []boxOutbound `json:"outbounds"`
	}
	if err := strictJSON(data, &doc); err != nil {
		return domain.IR{}, err
	}
	ir := domain.IR{}
	for _, o := range doc.Outbounds {
		protocol := o.Type
		if protocol == "shadowsocks" {
			protocol = "ss"
		}
		if protocol == "socks" {
			protocol = "socks5"
		}
		n := api.NetworkProxyNode{Protocol: api.NetworkProxyNodeProtocol(protocol), Server: o.Server, Port: o.Port, Username: o.Username, Secret: o.Password, Cipher: o.Method}
		if o.UUID != "" {
			id, e := uuid.Parse(o.UUID)
			if e != nil {
				return domain.IR{}, domain.Invalid("invalid_uuid", "outbounds")
			}
			n.UUID = id
		}
		if o.TLS != nil {
			n.TLS = &api.NetworkProxyTLS{Enabled: o.TLS.Enabled, ServerName: o.TLS.ServerName, Alpn: o.TLS.ALPN}
		}
		if o.Transport != nil {
			t := o.Transport
			n.Transport = &api.NetworkProxyTransport{Type: api.NetworkProxyTransportType(t.Type), Path: t.Path, ServiceName: t.ServiceName}
			if t.Headers != nil {
				n.Transport.Host = t.Headers.Host
			}
		}
		ir.Nodes = append(ir.Nodes, n)
	}
	return ir, nil
}

type v2User struct {
	ID         string `json:"id"`
	Security   string `json:"security"`
	Encryption string `json:"encryption"`
	AlterID    int    `json:"alterId"`
}
type v2Server struct {
	Address  string   `json:"address"`
	Port     int      `json:"port"`
	Method   string   `json:"method"`
	Password string   `json:"password"`
	Users    []v2User `json:"users"`
}
type v2Outbound struct {
	Protocol string `json:"protocol"`
	Tag      string `json:"tag"`
	Settings struct {
		Servers []v2Server `json:"servers"`
		VNext   []v2Server `json:"vnext"`
	} `json:"settings"`
	Stream *struct {
		Network  string `json:"network"`
		Security string `json:"security"`
		TLS      *struct {
			ServerName string   `json:"serverName"`
			ALPN       []string `json:"alpn"`
		} `json:"tlsSettings"`
		WS *struct {
			Path    string `json:"path"`
			Headers *struct {
				Host string `json:"Host"`
			} `json:"headers"`
		} `json:"wsSettings"`
		GRPC *struct {
			ServiceName string `json:"serviceName"`
		} `json:"grpcSettings"`
	} `json:"streamSettings"`
}

func parseV2Ray(data []byte) (domain.IR, error) {
	var doc struct {
		Outbounds []v2Outbound `json:"outbounds"`
	}
	if err := strictJSON(data, &doc); err != nil {
		return domain.IR{}, err
	}
	ir := domain.IR{}
	for _, o := range doc.Outbounds {
		servers := o.Settings.Servers
		if len(o.Settings.VNext) > 0 {
			if len(servers) > 0 {
				return domain.IR{}, domain.Invalid("ambiguous_source", "outbounds")
			}
			servers = o.Settings.VNext
		}
		if len(servers) != 1 {
			return domain.IR{}, domain.Invalid("unsupported_servers", "outbounds")
		}
		v := servers[0]
		protocol := o.Protocol
		if protocol == "shadowsocks" {
			protocol = "ss"
		}
		if protocol == "socks" {
			protocol = "socks5"
		}
		n := api.NetworkProxyNode{Protocol: api.NetworkProxyNodeProtocol(protocol), Server: v.Address, Port: v.Port, Secret: v.Password, Cipher: v.Method}
		if protocol == "vmess" || protocol == "vless" {
			if len(v.Users) != 1 || v.Users[0].AlterID != 0 || (v.Users[0].Security != "" && v.Users[0].Security != "auto") || (v.Users[0].Encryption != "" && v.Users[0].Encryption != "none") {
				return domain.IR{}, domain.Invalid("unsupported_user", "outbounds")
			}
			id, e := uuid.Parse(v.Users[0].ID)
			if e != nil {
				return domain.IR{}, domain.Invalid("invalid_uuid", "outbounds")
			}
			n.UUID = id
		} else if len(v.Users) > 0 {
			return domain.IR{}, domain.Invalid("unsupported_user", "outbounds")
		}
		if err := v2RayStream(&n, o); err != nil {
			return domain.IR{}, err
		}
		ir.Nodes = append(ir.Nodes, n)
	}
	return ir, nil
}
func decode64(s string) ([]byte, error) {
	for _, e := range []*base64.Encoding{base64.RawURLEncoding, base64.URLEncoding, base64.RawStdEncoding, base64.StdEncoding} {
		if b, err := e.DecodeString(s); err == nil {
			return b, nil
		}
	}
	return nil, domain.Invalid("invalid_encoding", "content")
}
func parseURIs(content string) (domain.IR, error) {
	ir := domain.IR{}
	for i, line := range strings.Split(strings.TrimSpace(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if i > domain.MaxNodes {
			return domain.IR{}, domain.Invalid("node_limit", "nodes")
		}
		n, err := parseURI(line)
		if err != nil {
			return domain.IR{}, err
		}
		ir.Nodes = append(ir.Nodes, n)
	}
	return ir, nil
}
func parseURI(raw string) (api.NetworkProxyNode, error) {
	var n api.NetworkProxyNode
	if strings.HasPrefix(raw, "vmess://") {
		return parseVMess(strings.TrimPrefix(raw, "vmess://"))
	}
	u, err := url.Parse(raw)
	if err != nil || u.Opaque != "" || u.Hostname() == "" {
		return n, domain.Invalid("invalid_uri", "content")
	}
	n.Protocol = api.NetworkProxyNodeProtocol(u.Scheme)
	n.Server = u.Hostname()
	n.Port, err = strconv.Atoi(u.Port())
	if err != nil {
		return n, domain.Invalid("invalid_endpoint", "content")
	}
	if u.Path != "" && u.Path != "/" {
		return n, domain.Invalid("unsupported_field", "content")
	}
	if err := uriCredentials(&n, u); err != nil {
		return n, err
	}
	q, err := uriQuery(u.RawQuery)
	if err != nil {
		return n, err
	}
	if err := uriTLS(&n, u, q); err != nil {
		return n, err
	}
	if err := uriTransport(&n, q); err != nil {
		return n, err
	}
	return n, nil
}
func parseVMess(encoded string) (api.NetworkProxyNode, error) {
	var n api.NetworkProxyNode
	b, e := decode64(encoded)
	if e != nil {
		return n, e
	}
	var v struct {
		V    string      `json:"v"`
		PS   string      `json:"ps"`
		Add  string      `json:"add"`
		Port json.Number `json:"port"`
		ID   string      `json:"id"`
		Aid  json.Number `json:"aid"`
		SCY  string      `json:"scy"`
		Net  string      `json:"net"`
		Type string      `json:"type"`
		Host string      `json:"host"`
		Path string      `json:"path"`
		TLS  string      `json:"tls"`
		SNI  string      `json:"sni"`
		ALPN string      `json:"alpn"`
	}
	if e = strictJSON(b, &v); e != nil {
		return n, e
	}
	if v.V != "2" || (v.Aid != "" && v.Aid != "0") || (v.SCY != "" && v.SCY != "auto") || (v.Type != "" && v.Type != "none") {
		return n, domain.Invalid("unsupported_vmess", "content")
	}
	n.Protocol = "vmess"
	n.Server = v.Add
	n.Port, _ = strconv.Atoi(string(v.Port))
	n.UUID, e = uuid.Parse(v.ID)
	if e != nil {
		return n, domain.Invalid("invalid_uuid", "content")
	}
	if v.TLS == "tls" {
		n.TLS = &api.NetworkProxyTLS{Enabled: true, ServerName: v.SNI}
		if v.ALPN != "" {
			n.TLS.Alpn = strings.Split(v.ALPN, ",")
		}
	} else if v.SNI != "" || v.ALPN != "" {
		return n, domain.Invalid("invalid_tls", "content")
	} else if v.TLS != "" {
		return n, domain.Invalid("unsupported_tls", "content")
	}
	if v.Net == "" && (v.Host != "" || v.Path != "") {
		return n, domain.Invalid("invalid_transport", "content")
	}
	if v.Net != "" {
		n.Transport = &api.NetworkProxyTransport{Type: api.NetworkProxyTransportType(v.Net), Host: v.Host, Path: v.Path}
	}
	return n, nil
}

func uriCredentials(n *api.NetworkProxyNode, u *url.URL) error {
	if u.User != nil {
		password, hasPassword := u.User.Password()
		switch u.Scheme {
		case "ss":
			credentials := u.User.Username() + ":" + password
			if !hasPassword {
				decoded, e := decode64(u.User.Username())
				if e != nil {
					return e
				}
				credentials = string(decoded)
			}
			parts := strings.SplitN(credentials, ":", 2)
			if len(parts) != 2 {
				return domain.Invalid("invalid_credential", "content")
			}
			n.Cipher = parts[0]
			n.Secret = parts[1]
		case "trojan":
			if hasPassword {
				return domain.Invalid("invalid_credential", "content")
			}
			n.Secret = u.User.Username()
		case "vless":
			id, e := uuid.Parse(u.User.Username())
			if e != nil || hasPassword {
				return domain.Invalid("invalid_uuid", "content")
			}
			n.UUID = id
		case "http", "socks5":
			n.Username = u.User.Username()
			n.Secret = password
		default:
			return domain.Invalid("unsupported_protocol", "content")
		}
	}

	return nil
}

func uriQuery(raw string) (url.Values, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, domain.Invalid("invalid_uri", "content")
	}
	for k, v := range q {
		if len(v) != 1 {
			return nil, domain.Invalid("duplicate_field", "content")
		}
		switch k {
		case "security", "sni", "alpn", "type", "host", "path", "serviceName", "encryption":
		default:
			return nil, domain.Invalid("unsupported_field", "content")
		}
	}

	return q, nil
}

func uriTLS(n *api.NetworkProxyNode, u *url.URL, q url.Values) error {
	security := q.Get("security")
	if security == "tls" || u.Scheme == "trojan" {
		n.TLS = &api.NetworkProxyTLS{Enabled: true, ServerName: q.Get("sni")}
		if q.Get("alpn") != "" {
			n.TLS.Alpn = strings.Split(q.Get("alpn"), ",")
		}
	} else if security != "" && security != "none" {
		return domain.Invalid("unsupported_tls", "content")
	} else if q.Get("sni") != "" || q.Get("alpn") != "" {
		return domain.Invalid("invalid_tls", "content")
	}

	return nil
}

func uriTransport(n *api.NetworkProxyNode, q url.Values) error {
	if q.Get("encryption") != "" && q.Get("encryption") != "none" {
		return domain.Invalid("unsupported_encryption", "content")
	}
	if q.Get("type") != "" {
		n.Transport = &api.NetworkProxyTransport{Type: api.NetworkProxyTransportType(q.Get("type")), Host: q.Get("host"), Path: q.Get("path"), ServiceName: q.Get("serviceName")}
	} else if q.Get("host") != "" || q.Get("path") != "" || q.Get("serviceName") != "" {
		return domain.Invalid("invalid_transport", "content")
	}

	return nil
}

func v2RayStream(n *api.NetworkProxyNode, o v2Outbound) error {
	if o.Stream != nil {
		s := o.Stream
		if s.Security != "" && s.Security != "none" && s.Security != "tls" {
			return domain.Invalid("unsupported_tls", "outbounds")
		}
		if s.Security == "tls" {
			n.TLS = &api.NetworkProxyTLS{Enabled: true}
			if s.TLS != nil {
				n.TLS.ServerName = s.TLS.ServerName
				n.TLS.Alpn = s.TLS.ALPN
			}
		} else if s.TLS != nil {
			return domain.Invalid("invalid_tls", "outbounds")
		}
		network := s.Network
		if network == "" {
			network = "tcp"
		}
		n.Transport = &api.NetworkProxyTransport{Type: api.NetworkProxyTransportType(network)}
		if s.WS != nil {
			if network != "ws" {
				return domain.Invalid("invalid_transport", "outbounds")
			}
			n.Transport.Path = s.WS.Path
			if s.WS.Headers != nil {
				n.Transport.Host = s.WS.Headers.Host
			}
		}
		if s.GRPC != nil {
			if network != "grpc" {
				return domain.Invalid("invalid_transport", "outbounds")
			}
			n.Transport.ServiceName = s.GRPC.ServiceName
		}
	}

	return nil
}
