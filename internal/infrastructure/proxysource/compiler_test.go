package proxysource

import (
	"context"
	"encoding/json"
	"errors"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"net/netip"
	"strings"
	"testing"
)

func TestFormatsAndStableIdentity(t *testing.T) {
	fixtures := map[string]string{
		"mihomo_yaml":   "proxies:\n - name: synthetic-secret.example\n   type: http\n   server: origin.example\n   port: 443\n   password: private-token\n",
		"sing_box_json": `{"outbounds":[{"type":"http","server":"origin.example","server_port":443,"password":"private-token"}]}`,
		"v2ray_json":    `{"outbounds":[{"protocol":"http","settings":{"servers":[{"address":"origin.example","port":443,"password":"private-token"}]}}]}`,
		"uri_list":      "http://user:private-token@origin.example:443#synthetic-secret.example",
	}
	compiler := NewCompiler()
	var stable string
	for format, content := range fixtures {
		t.Run(format, func(t *testing.T) {
			ir, err := compiler.Compile(api.NetworkProxySourceInput{Format: api.NetworkProxySourceFormat(format), Content: content})
			if err != nil {
				t.Fatal(err)
			}
			if len(ir.Nodes) != 1 || ir.Nodes[0].Label != "Node 1" {
				t.Fatalf("unexpected nodes: %d", len(ir.Nodes))
			}
			if stable == "" {
				stable = ir.Nodes[0].NodeID
			} else if stable != ir.Nodes[0].NodeID {
				t.Fatal("format changed identity")
			}
		})
	}
	input := api.NetworkProxySourceInput{Format: "manual", Nodes: []api.NetworkProxyNode{{Protocol: "http", Server: "origin.example", Port: 443, Label: "renamed", Secret: "rotated"}}}
	ir, err := compiler.Compile(input)
	if err != nil || ir.Nodes[0].NodeID != stable {
		t.Fatal("credential rotation changed identity", err)
	}
}
func TestWholeConversionRejectsUnknownUnsafeAndQuota(t *testing.T) {
	cases := []api.NetworkProxySourceInput{
		{Format: "mihomo_yaml", Content: "proxies: []\nexternal-controller: secret.example\n"},
		{Format: "mihomo_yaml", Content: "proxies:\n- name: x\n  type: http\n  server: origin.example\n  port: 443\n  skip-cert-verify: true\n"},
		{Format: "sing_box_json", Content: `{"outbounds":[],"route":{"final":"DIRECT"}}`},
		{Format: "v2ray_json", Content: `{"outbounds":[],"inbounds":[]}`},
		{Format: "uri_list", Content: "trojan://secret@origin.example:443?allowInsecure=1"},
		{Format: "uri_list", Content: "http://origin.example:443\nvless://bad@origin.example:443"},
		{Format: "manual", Nodes: make([]api.NetworkProxyNode, 129)},
	}
	for i, input := range cases {
		ir, err := NewCompiler().Compile(input)
		var typed *domain.ConversionError
		if !errors.As(err, &typed) || len(ir.Nodes) != 0 {
			t.Fatalf("case %d not atomic typed reject: %v", i, err)
		}
		if strings.Contains(err.Error(), "secret.example") {
			t.Fatal("error leaked source")
		}
	}
}
func TestSafeAliasesAndGroupReferences(t *testing.T) {
	content := "proxies:\n- {name: secret.example, type: http, server: origin.example, port: 443}\nproxy-groups:\n- {name: private-token, type: select, proxies: [secret.example]}\nrules: [\"DOMAIN,example.com,private-token\"]\n"
	ir, err := NewCompiler().Compile(api.NetworkProxySourceInput{Format: "mihomo_yaml", Content: content})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(ir.Groups)
	if strings.Contains(string(raw), "private-token") || strings.Contains(string(raw), "secret.example") {
		t.Fatal("alias leaked")
	}
	if ir.Rules[0].Action.TargetID != ir.Groups[0].GroupID {
		t.Fatal("unmapped rule")
	}
}
func TestFetchAddressGuard(t *testing.T) {
	options := Options{ProtectedHost: func(context.Context, string) bool { return false }, PrivateAllowlist: []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}, Protected: func(_ context.Context, ip netip.Addr) bool { return ip == netip.MustParseAddr("10.2.3.4") }}
	for _, raw := range []string{"127.0.0.1", "::1", "169.254.169.254", "100.100.100.200", "10.2.3.4", "192.168.1.2", "::ffff:127.0.0.1"} {
		if allowedAddress(context.Background(), netip.MustParseAddr(raw), options) {
			t.Fatalf("allowed %s", raw)
		}
	}
	if !allowedAddress(context.Background(), netip.MustParseAddr("10.1.1.1"), options) {
		t.Fatal("installer allowlist rejected")
	}
	f, err := NewFetcher(options)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.Fetch(context.Background(), "https://user:synthetic-secret@source.example/config"); err == nil || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("URL credential accepted/leaked")
	}
}

func TestDirectExportContainsOnlyReviewedProxies(t *testing.T) {
	compiler := NewCompiler()
	ir, err := compiler.Compile(api.NetworkProxySourceInput{Format: "uri_list", Content: "http://user:synthetic-secret@upstream.example:443#sensitive-alias"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := compiler.ExportDirect(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "upstream.example") || !strings.Contains(out, "synthetic-secret") || strings.Contains(out, "sensitive-alias") || strings.Contains(out, "proxy-groups") || strings.Contains(out, "rules:") {
		t.Fatal("direct disclosure or subset incorrect")
	}
}

func TestHostOnlyProtectionAndRedirectsRejectBeforeFetch(t *testing.T) {
	calls := 0
	f, err := NewFetcher(Options{Protected: func(context.Context, netip.Addr) bool { return false }, ProtectedHost: func(_ context.Context, host string) bool { calls++; return host == "protected.example" }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Fetch(context.Background(), "https://PROTECTED.example./private?token=synthetic-secret")
	if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-secret") {
		t.Fatal("host-only protected URL fetched or leaked", err)
	}
	if err = f.client.CheckRedirect(nil, nil); err == nil {
		t.Fatal("redirects allowed")
	}
	if _, err = NewFetcher(Options{Protected: func(context.Context, netip.Addr) bool { return false }}); err == nil {
		t.Fatal("missing host protection accepted")
	}
}
