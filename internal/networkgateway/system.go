package networkgateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type wireGuardClient interface {
	Device(string) (*wgtypes.Device, error)
	ConfigureDevice(string, wgtypes.Config) error
	Close() error
}

type commandRunner interface {
	Run(context.Context, []byte, string, ...string) ([]byte, error)
}

type linuxSystem struct {
	wg       wireGuardClient
	commands commandRunner
	ipPath   string
	nftPath  string
	egress   string
}

func NewLinuxSystem(ipPath, nftPath, egressInterface string) (*linuxSystem, error) {
	if runtime.GOOS != "linux" {
		return nil, fmt.Errorf("network gateway system executor requires Linux")
	}
	if egressInterface != "" && !linuxInterfacePattern.MatchString(egressInterface) {
		return nil, fmt.Errorf("network gateway egress interface is invalid")
	}
	for name, path := range map[string]string{"ip": ipPath, "nft": nftPath} {
		if !filepath.IsAbs(path) {
			return nil, fmt.Errorf("%s command path must be absolute", name)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
			return nil, fmt.Errorf("%s command is not an executable regular file", name)
		}
	}
	client, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("open WireGuard control client: %w", err)
	}
	system, err := newLinuxSystem(client, execCommands{}, ipPath, nftPath, egressInterface)
	if err != nil {
		_ = client.Close()
	}
	return system, err
}

func newLinuxSystem(wg wireGuardClient, commands commandRunner, ipPath, nftPath, egressInterface string) (*linuxSystem, error) {
	if wg == nil || commands == nil || !filepath.IsAbs(ipPath) || !filepath.IsAbs(nftPath) || (egressInterface != "" && !linuxInterfacePattern.MatchString(egressInterface)) {
		return nil, fmt.Errorf("linux gateway system dependencies are invalid")
	}
	return &linuxSystem{wg: wg, commands: commands, ipPath: ipPath, nftPath: nftPath, egress: egressInterface}, nil
}

func (s *linuxSystem) Close() error { return s.wg.Close() }

func (s *linuxSystem) Baseline(ctx context.Context, interfaceName string) error {
	wg := networkprotocol.WireGuardConfiguration{InterfaceName: interfaceName, RoutingMode: "routed", FirewallDefault: "deny", FirewallRules: []networkprotocol.WireGuardFirewallRule{}}
	return s.applyFirewall(ctx, wg)
}

func (s *linuxSystem) Apply(ctx context.Context, desired networkprotocol.ConfigurationDesired, privateKey wgtypes.Key) error {
	if desired.WireGuard == nil {
		return s.Disable(ctx, defaultInterfaceName, privateKey.PublicKey())
	}
	wg := *desired.WireGuard
	configuration, err := wireGuardDeviceConfig(wg, privateKey)
	if err != nil {
		return err
	}
	link, err := s.link(ctx, wg.InterfaceName)
	if err != nil {
		return err
	}
	created := false
	if link != nil {
		if link.Kind != "wireguard" {
			return fmt.Errorf("interface %q exists and is not WireGuard", wg.InterfaceName)
		}
		device, err := s.wg.Device(wg.InterfaceName)
		if err != nil || device.PublicKey != privateKey.PublicKey() {
			return fmt.Errorf("WireGuard interface %q is not owned by this gateway", wg.InterfaceName)
		}
	} else {
		if _, createErr := s.commands.Run(ctx, nil, s.ipPath, "link", "add", "dev", wg.InterfaceName, "type", "wireguard"); createErr != nil {
			return fmt.Errorf("create WireGuard interface: %w", createErr)
		}
		created = true
	}
	if err := s.wg.ConfigureDevice(wg.InterfaceName, configuration); err != nil {
		if created {
			_, _ = s.commands.Run(ctx, nil, s.ipPath, "link", "delete", "dev", wg.InterfaceName)
		}
		return fmt.Errorf("configure WireGuard interface: %w", err)
	}
	if _, err := s.commands.Run(ctx, nil, s.ipPath, "-4", "address", "flush", "dev", wg.InterfaceName); err != nil {
		return fmt.Errorf("flush WireGuard addresses: %w", err)
	}
	for _, address := range wg.Addresses {
		if _, err := s.commands.Run(ctx, nil, s.ipPath, "-4", "address", "add", address, "dev", wg.InterfaceName); err != nil {
			return fmt.Errorf("add WireGuard address: %w", err)
		}
	}
	if _, err := s.commands.Run(ctx, nil, s.ipPath, "link", "set", "dev", wg.InterfaceName, "mtu", fmt.Sprint(wg.MTU), "up"); err != nil {
		return fmt.Errorf("activate WireGuard interface: %w", err)
	}
	if _, err := s.commands.Run(ctx, nil, s.ipPath, "-4", "route", "flush", "dev", wg.InterfaceName, "proto", "static"); err != nil {
		return fmt.Errorf("flush WireGuard routes: %w", err)
	}
	for _, route := range wg.Routes {
		if _, err := s.commands.Run(ctx, nil, s.ipPath, "-4", "route", "replace", route, "dev", wg.InterfaceName, "proto", "static"); err != nil {
			return fmt.Errorf("replace WireGuard route: %w", err)
		}
	}
	return s.applyFirewall(ctx, wg)
}

func (s *linuxSystem) Disable(ctx context.Context, interfaceName string, expectedPublicKey wgtypes.Key) error {
	link, err := s.link(ctx, interfaceName)
	if err != nil {
		return err
	}
	if link == nil {
		return nil
	}
	if link.Kind != "wireguard" {
		return fmt.Errorf("refuse to delete non-WireGuard interface %q", interfaceName)
	}
	device, err := s.wg.Device(interfaceName)
	if err != nil {
		return fmt.Errorf("read WireGuard interface ownership: %w", err)
	}
	if expectedPublicKey == (wgtypes.Key{}) || device.PublicKey != expectedPublicKey {
		return fmt.Errorf("refuse to delete WireGuard interface %q owned by another key", interfaceName)
	}
	confirmed, err := s.link(ctx, interfaceName)
	if err != nil || confirmed == nil || confirmed.Index != link.Index || confirmed.Kind != "wireguard" {
		return fmt.Errorf("WireGuard interface %q changed before deletion", interfaceName)
	}
	device, err = s.wg.Device(interfaceName)
	if err != nil || device.PublicKey != expectedPublicKey {
		return fmt.Errorf("WireGuard interface %q ownership changed before deletion", interfaceName)
	}
	// ponytail: NET_ADMIN is exclusive to this container network namespace. If
	// shared privileged namespaces are supported, delete atomically by ifindex.
	_, err = s.commands.Run(ctx, nil, s.ipPath, "link", "delete", "dev", interfaceName)
	return err
}

type networkLink struct {
	Index int
	Kind  string
}

func (s *linuxSystem) link(ctx context.Context, interfaceName string) (*networkLink, error) {
	raw, err := s.commands.Run(ctx, nil, s.ipPath, "-j", "-d", "link", "show")
	if err != nil {
		return nil, fmt.Errorf("inspect network interfaces: %w", err)
	}
	var links []struct {
		Index int    `json:"ifindex"`
		Name  string `json:"ifname"`
		Info  struct {
			Kind string `json:"info_kind"`
		} `json:"linkinfo"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, fmt.Errorf("decode network interfaces: %w", err)
	}
	var found *networkLink
	for _, link := range links {
		if link.Name == interfaceName {
			if found != nil || link.Index < 1 {
				return nil, fmt.Errorf("network interface readback is ambiguous")
			}
			found = &networkLink{Index: link.Index, Kind: link.Info.Kind}
		}
	}
	return found, nil
}

func (s *linuxSystem) Readback(ctx context.Context, desired networkprotocol.ConfigurationDesired) (string, error) {
	if desired.WireGuard == nil {
		return s.readDisabled(ctx)
	}
	wg := *desired.WireGuard
	device, actualPeers, err := s.readWireGuard(wg)
	if err != nil {
		return "", err
	}
	addresses, routes, err := s.readIPConfiguration(ctx, wg)
	if err != nil {
		return "", err
	}
	nftRaw, err := s.commands.Run(ctx, nil, s.nftPath, "-j", "list", "table", "inet", firewallTable)
	if err != nil {
		return "", fmt.Errorf("read gateway firewall: %w", err)
	}
	ruleIDs, err := verifyNftReadback(nftRaw, wg, s.egress)
	if err != nil {
		return "", err
	}
	return hashReadbackState(device, wg, actualPeers, addresses, routes, ruleIDs)
}

func (s *linuxSystem) readDisabled(ctx context.Context) (string, error) {
	link, err := s.link(ctx, defaultInterfaceName)
	if err != nil {
		return "", fmt.Errorf("verify disabled WireGuard interface: %w", err)
	}
	if link != nil {
		return "", fmt.Errorf("disabled WireGuard interface still exists")
	}
	digest := sha256.Sum256([]byte(`{"wireguard":"disabled"}`))
	return fmt.Sprintf("sha256:%x", digest), nil
}

func (s *linuxSystem) readWireGuard(wg networkprotocol.WireGuardConfiguration) (*wgtypes.Device, []readbackPeer, error) {
	device, err := s.wg.Device(wg.InterfaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("read WireGuard device: %w", err)
	}
	actualPeers := make([]readbackPeer, 0, len(device.Peers))
	for _, peer := range device.Peers {
		allowedIPs := make([]string, len(peer.AllowedIPs))
		for index := range peer.AllowedIPs {
			allowedIPs[index] = peer.AllowedIPs[index].String()
		}
		slices.Sort(allowedIPs)
		endpoint := ""
		if peer.Endpoint != nil {
			endpoint = peer.Endpoint.String()
		}
		actualPeers = append(actualPeers, readbackPeer{PublicKey: peer.PublicKey.String(), Endpoint: endpoint, KeepaliveSeconds: int(peer.PersistentKeepaliveInterval / time.Second), AllowedIPs: allowedIPs})
	}
	slices.SortFunc(actualPeers, func(left, right readbackPeer) int { return strings.Compare(left.PublicKey, right.PublicKey) })
	expectedPeers, err := expectedReadbackPeers(wg.Peers)
	if err != nil {
		return nil, nil, err
	}
	if device.PublicKey.String() != wg.PublicKey || device.ListenPort != wg.ListenPort || !slices.EqualFunc(actualPeers, expectedPeers, sameReadbackPeer) {
		return nil, nil, fmt.Errorf("WireGuard device readback differs from desired state")
	}
	return device, actualPeers, nil
}

func expectedReadbackPeers(peers []networkprotocol.WireGuardPeer) ([]readbackPeer, error) {
	expected := make([]readbackPeer, 0, len(peers))
	for _, peer := range peers {
		allowedIPs := slices.Clone(peer.AllowedIPs)
		slices.Sort(allowedIPs)
		endpoint, err := resolvePeerEndpoint(peer)
		if err != nil {
			return nil, err
		}
		endpointText := ""
		if endpoint != nil {
			endpointText = endpoint.String()
		}
		expected = append(expected, readbackPeer{PublicKey: peer.PublicKey, Endpoint: endpointText, KeepaliveSeconds: peer.PersistentKeepaliveSeconds, AllowedIPs: allowedIPs})
	}
	slices.SortFunc(expected, func(left, right readbackPeer) int { return strings.Compare(left.PublicKey, right.PublicKey) })
	return expected, nil
}

func sameReadbackPeer(left, right readbackPeer) bool {
	return left.PublicKey == right.PublicKey && left.Endpoint == right.Endpoint && left.KeepaliveSeconds == right.KeepaliveSeconds && slices.Equal(left.AllowedIPs, right.AllowedIPs)
}

func (s *linuxSystem) readIPConfiguration(ctx context.Context, wg networkprotocol.WireGuardConfiguration) ([]string, []string, error) {
	addressesRaw, err := s.commands.Run(ctx, nil, s.ipPath, "-j", "-4", "address", "show", "dev", wg.InterfaceName)
	if err != nil {
		return nil, nil, fmt.Errorf("read WireGuard addresses: %w", err)
	}
	addresses, err := parseIPAddresses(addressesRaw)
	if err != nil || !sameStrings(addresses, wg.Addresses) {
		return nil, nil, fmt.Errorf("WireGuard address readback differs from desired state")
	}
	routesRaw, err := s.commands.Run(ctx, nil, s.ipPath, "-j", "-4", "route", "show", "dev", wg.InterfaceName, "proto", "static")
	if err != nil {
		return nil, nil, fmt.Errorf("read WireGuard routes: %w", err)
	}
	routes, err := parseIPRoutes(routesRaw)
	if err != nil || !sameStrings(routes, wg.Routes) {
		return nil, nil, fmt.Errorf("WireGuard route readback differs from desired state")
	}
	return addresses, routes, nil
}

func hashReadbackState(device *wgtypes.Device, wg networkprotocol.WireGuardConfiguration, peers []readbackPeer, addresses, routes, ruleIDs []string) (string, error) {
	state := readbackState{PublicKey: device.PublicKey.String(), ListenPort: device.ListenPort, Addresses: sortedStrings(addresses), Routes: sortedStrings(routes), Peers: peers, RuleIDs: ruleIDs, RoutingMode: wg.RoutingMode}
	slices.Sort(state.RuleIDs)
	encoded, err := json.Marshal(state)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest), nil
}

func (s *linuxSystem) applyFirewall(ctx context.Context, wg networkprotocol.WireGuardConfiguration) error {
	tables, err := s.commands.Run(ctx, nil, s.nftPath, "-j", "list", "tables")
	if err != nil {
		return fmt.Errorf("list nftables tables: %w", err)
	}
	exists, err := nftTableExists(tables)
	if err != nil {
		return err
	}
	program, err := BuildNftProgram(wg, exists, s.egress)
	if err != nil {
		return err
	}
	if _, err := s.commands.Run(ctx, []byte(program), s.nftPath, "-f", "-"); err != nil {
		return fmt.Errorf("apply nftables transaction: %w", err)
	}
	return nil
}

func wireGuardDeviceConfig(wg networkprotocol.WireGuardConfiguration, privateKey wgtypes.Key) (wgtypes.Config, error) {
	listenPort := wg.ListenPort
	configuration := wgtypes.Config{PrivateKey: &privateKey, ListenPort: &listenPort, ReplacePeers: true, Peers: make([]wgtypes.PeerConfig, 0, len(wg.Peers))}
	for _, peer := range wg.Peers {
		publicKey, err := wgtypes.ParseKey(peer.PublicKey)
		if err != nil {
			return wgtypes.Config{}, fmt.Errorf("parse WireGuard peer public key: %w", err)
		}
		item := wgtypes.PeerConfig{PublicKey: publicKey, ReplaceAllowedIPs: true, AllowedIPs: make([]net.IPNet, 0, len(peer.AllowedIPs))}
		for _, raw := range peer.AllowedIPs {
			_, network, err := net.ParseCIDR(raw)
			if err != nil {
				return wgtypes.Config{}, fmt.Errorf("parse WireGuard peer AllowedIP: %w", err)
			}
			item.AllowedIPs = append(item.AllowedIPs, *network)
		}
		if peer.PersistentKeepaliveSeconds > 0 {
			keepalive := time.Duration(peer.PersistentKeepaliveSeconds) * time.Second
			item.PersistentKeepaliveInterval = &keepalive
		}
		item.Endpoint, err = resolvePeerEndpoint(peer)
		if err != nil {
			return wgtypes.Config{}, err
		}
		configuration.Peers = append(configuration.Peers, item)
	}
	return configuration, nil
}

func resolvePeerEndpoint(peer networkprotocol.WireGuardPeer) (*net.UDPAddr, error) {
	if peer.EndpointHost == "" {
		return nil, nil
	}
	endpoint, err := net.ResolveUDPAddr("udp", net.JoinHostPort(peer.EndpointHost, strconv.Itoa(peer.EndpointPort)))
	if err != nil {
		return nil, fmt.Errorf("resolve WireGuard peer endpoint: %w", err)
	}
	return endpoint, nil
}

func nftTableExists(raw []byte) (bool, error) {
	var document struct {
		Nftables []struct {
			Table *struct {
				Family string `json:"family"`
				Name   string `json:"name"`
			} `json:"table"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return false, fmt.Errorf("decode nftables table list: %w", err)
	}
	for _, item := range document.Nftables {
		if item.Table != nil && item.Table.Family == "inet" && item.Table.Name == firewallTable {
			return true, nil
		}
	}
	return false, nil
}

type nftReadbackChain struct {
	Family string `json:"family"`
	Table  string `json:"table"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Hook   string `json:"hook"`
	Policy string `json:"policy"`
}

type nftReadbackRule struct {
	Family  string `json:"family"`
	Table   string `json:"table"`
	Chain   string `json:"chain"`
	Comment string `json:"comment"`
	Expr    any    `json:"expr"`
}

type expectedNftRule struct {
	comment string
	expr    []any
}

type nftReadbackState struct {
	tables int
	chains map[string]nftReadbackChain
	rules  map[string][]nftReadbackRule
}

func verifyNftReadback(raw []byte, wg networkprotocol.WireGuardConfiguration, egressInterface string) ([]string, error) {
	state, err := decodeNftReadback(raw)
	if err != nil {
		return nil, err
	}
	if err := validateNftTopology(state, wg.RoutingMode); err != nil {
		return nil, err
	}
	expectedForward, ruleIDs, err := expectedNftForward(wg)
	if err != nil {
		return nil, err
	}
	if err := matchNftRules(state.rules["forward"], expectedForward); err != nil {
		return nil, err
	}
	if wg.RoutingMode == "snat" {
		if err := matchNftRules(state.rules["postrouting"], expectedNftPostrouting(wg, egressInterface)); err != nil {
			return nil, err
		}
	}
	for name := range state.rules {
		if name != "forward" && (name != "postrouting" || wg.RoutingMode != "snat") {
			return nil, fmt.Errorf("gateway firewall readback contains rules in an unexpected chain")
		}
	}
	return ruleIDs, nil
}

func decodeNftReadback(raw []byte) (nftReadbackState, error) {
	var document struct {
		Nftables []struct {
			Table *struct {
				Family string `json:"family"`
				Name   string `json:"name"`
			} `json:"table"`
			Chain *nftReadbackChain `json:"chain"`
			Rule  *nftReadbackRule  `json:"rule"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(raw, &document); err != nil {
		return nftReadbackState{}, fmt.Errorf("decode gateway firewall readback: %w", err)
	}
	state := nftReadbackState{chains: map[string]nftReadbackChain{}, rules: map[string][]nftReadbackRule{}}
	for _, item := range document.Nftables {
		if item.Table != nil {
			if item.Table.Family != "inet" || item.Table.Name != firewallTable {
				return nftReadbackState{}, fmt.Errorf("gateway firewall readback contains an unexpected table")
			}
			state.tables++
		}
		if item.Chain != nil {
			if item.Chain.Family != "inet" || item.Chain.Table != firewallTable {
				return nftReadbackState{}, fmt.Errorf("gateway firewall readback contains an unexpected chain")
			}
			if _, exists := state.chains[item.Chain.Name]; exists {
				return nftReadbackState{}, fmt.Errorf("gateway firewall readback contains a duplicate chain")
			}
			state.chains[item.Chain.Name] = *item.Chain
		}
		if item.Rule != nil {
			if item.Rule.Family != "inet" || item.Rule.Table != firewallTable {
				return nftReadbackState{}, fmt.Errorf("gateway firewall readback contains an unexpected rule")
			}
			state.rules[item.Rule.Chain] = append(state.rules[item.Rule.Chain], *item.Rule)
		}
	}
	return state, nil
}

func validateNftTopology(state nftReadbackState, routingMode string) error {
	if state.tables != 1 || (len(state.chains) != 1 && routingMode != "snat") || (len(state.chains) != 2 && routingMode == "snat") {
		return fmt.Errorf("gateway firewall table or chain count differs from desired state")
	}
	forward, ok := state.chains["forward"]
	if !ok || forward.Type != "filter" || forward.Hook != "forward" || forward.Policy != "accept" {
		return fmt.Errorf("gateway firewall forward chain differs from desired state")
	}
	if routingMode == "snat" {
		postrouting, ok := state.chains["postrouting"]
		if !ok || postrouting.Type != "nat" || postrouting.Hook != "postrouting" || postrouting.Policy != "accept" {
			return fmt.Errorf("gateway firewall postrouting chain differs from desired state")
		}
	} else if len(state.rules["postrouting"]) != 0 {
		return fmt.Errorf("routed gateway firewall contains postrouting rules")
	}
	return nil
}

func matchNftRules(actual []nftReadbackRule, expected []expectedNftRule) error {
	if len(actual) != len(expected) {
		return fmt.Errorf("gateway firewall rule count differs from desired state")
	}
	for index, wanted := range expected {
		got := actual[index]
		if got.Comment != wanted.comment || !reflect.DeepEqual(got.Expr, wanted.expr) {
			return fmt.Errorf("gateway firewall rule %d differs from desired semantics or order", index)
		}
	}
	return nil
}

func expectedNftForward(wg networkprotocol.WireGuardConfiguration) ([]expectedNftRule, []string, error) {
	expected := []expectedNftRule{{comment: "return-established", expr: []any{
		nftMatch(nftMeta("oifname"), wg.InterfaceName),
		map[string]any{"match": map[string]any{"op": "in", "left": map[string]any{"ct": map[string]any{"key": "state"}}, "right": []any{"established", "related"}}},
		nftVerdict("accept"),
	}}}
	ruleIDs := make([]string, 0, len(wg.FirewallRules))
	for _, configured := range wg.FirewallRules {
		source, sourceErr := canonicalIPv4Prefix(configured.SourceCIDR, -1)
		destination, destinationErr := canonicalIPv4Prefix(configured.DestinationCIDR, -1)
		if sourceErr != nil || destinationErr != nil {
			return nil, nil, fmt.Errorf("gateway firewall desired rule is invalid")
		}
		interfaceKey := "iifname"
		if configured.Direction == "to_wireguard" {
			interfaceKey = "oifname"
		}
		expressions := []any{
			nftMatch(nftMeta(interfaceKey), wg.InterfaceName),
			nftMatch(nftPayload("ip", "saddr"), nftPrefixValue(source)),
			nftMatch(nftPayload("ip", "daddr"), nftPrefixValue(destination)),
		}
		if configured.Protocol != "any" {
			if len(configured.Ports) == 0 {
				expressions = append(expressions, nftMatch(nftPayload("ip", "protocol"), configured.Protocol))
			} else {
				expressions = append(expressions, nftMatch(nftPayload(configured.Protocol, "dport"), nftPortValue(configured.Ports)))
			}
		}
		expressions = append(expressions, nftVerdict(map[string]string{"allow": "accept", "deny": "drop"}[configured.Effect]))
		expected = append(expected, expectedNftRule{comment: configured.ID, expr: expressions})
		ruleIDs = append(ruleIDs, configured.ID)
	}
	expected = append(expected,
		expectedNftRule{comment: "default-deny-from-wireguard", expr: []any{nftMatch(nftMeta("iifname"), wg.InterfaceName), nftVerdict("drop")}},
		expectedNftRule{comment: "default-deny-to-wireguard", expr: []any{nftMatch(nftMeta("oifname"), wg.InterfaceName), nftVerdict("drop")}},
	)
	return expected, ruleIDs, nil
}

func expectedNftPostrouting(wg networkprotocol.WireGuardConfiguration, egressInterface string) []expectedNftRule {
	expected := []expectedNftRule{}
	for _, configured := range wg.FirewallRules {
		if configured.Effect != "allow" {
			continue
		}
		source, _ := canonicalIPv4Prefix(configured.SourceCIDR, -1)
		destination, _ := canonicalIPv4Prefix(configured.DestinationCIDR, -1)
		expected = append(expected, expectedNftRule{comment: "snat-" + configured.ID, expr: []any{
			nftMatch(nftPayload("ip", "saddr"), nftPrefixValue(source)),
			nftMatch(nftPayload("ip", "daddr"), nftPrefixValue(destination)),
			nftMatch(nftMeta("oifname"), egressInterface),
			nftVerdict("masquerade"),
		}})
	}
	return expected
}

func nftMatch(left, right any) map[string]any {
	return map[string]any{"match": map[string]any{"op": "==", "left": left, "right": right}}
}

func nftMeta(key string) map[string]any {
	return map[string]any{"meta": map[string]any{"key": key}}
}

func nftPayload(protocol, field string) map[string]any {
	return map[string]any{"payload": map[string]any{"protocol": protocol, "field": field}}
}

func nftPrefixValue(prefix netip.Prefix) any {
	if prefix.Bits() == 32 {
		return prefix.Addr().String()
	}
	return map[string]any{"prefix": map[string]any{"addr": prefix.Addr().String(), "len": float64(prefix.Bits())}}
}

func nftPortValue(ports []int) any {
	values := slices.Clone(ports)
	slices.Sort(values)
	if len(values) == 1 {
		return float64(values[0])
	}
	set := make([]any, len(values))
	for index, port := range values {
		set[index] = float64(port)
	}
	return map[string]any{"set": set}
}

func nftVerdict(action string) map[string]any {
	return map[string]any{action: nil}
}

func parseIPAddresses(raw []byte) ([]string, error) {
	var links []struct {
		Addresses []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		return nil, err
	}
	addresses := []string{}
	for _, link := range links {
		for _, address := range link.Addresses {
			if address.Family == "inet" {
				addresses = append(addresses, fmt.Sprintf("%s/%d", address.Local, address.PrefixLen))
			}
		}
	}
	return addresses, nil
}

func parseIPRoutes(raw []byte) ([]string, error) {
	var rows []struct {
		Destination string `json:"dst"`
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, err
	}
	routes := make([]string, 0, len(rows))
	for _, row := range rows {
		if row.Destination != "" {
			routes = append(routes, row.Destination)
		}
	}
	return routes, nil
}

func sameStrings(left, right []string) bool {
	return slices.Equal(sortedStrings(left), sortedStrings(right))
}

func sortedStrings(values []string) []string {
	copy := slices.Clone(values)
	slices.Sort(copy)
	return copy
}

type readbackPeer struct {
	PublicKey        string   `json:"publicKey"`
	Endpoint         string   `json:"endpoint,omitempty"`
	KeepaliveSeconds int      `json:"keepaliveSeconds"`
	AllowedIPs       []string `json:"allowedIPs"`
}

type readbackState struct {
	PublicKey   string         `json:"publicKey"`
	ListenPort  int            `json:"listenPort"`
	Addresses   []string       `json:"addresses"`
	Routes      []string       `json:"routes"`
	Peers       []readbackPeer `json:"peers"`
	RuleIDs     []string       `json:"ruleIds"`
	RoutingMode string         `json:"routingMode"`
}

type execCommands struct{}

func (execCommands) Run(ctx context.Context, stdin []byte, path string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, path, args...)
	if stdin != nil {
		command.Stdin = bytes.NewReader(stdin)
	}
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w", filepath.Base(path), err)
	}
	return output, nil
}
