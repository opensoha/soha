package networkgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/securefile"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

type workloadControl struct {
	client                             *ControlClient
	binding                            domain.WorkloadBinding
	apiURL, tokenFile, devicePublicKey string
	key                                wgtypes.Key
	credentialUntil, sessionUntil      time.Time
}

// RunWorkload is a resource-only Linux client. Its short credential is minted
// from the current service-account token; every reconnect reauthorizes resources.
func RunWorkload(ctx context.Context, config Config, apiURL, tokenFile, bindingFile string, logger *slog.Logger) error {
	origin, err := runtimeOrigin(apiURL)
	if err != nil {
		return err
	}
	raw, err := securefile.Read(bindingFile, 16384, false)
	if err != nil {
		return err
	}
	var binding domain.WorkloadBinding
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&binding) != nil || decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("invalid workload binding file")
	}
	if binding.RuntimeID != config.RuntimeID || binding.DeviceID != config.DeviceID || !identifierPattern.MatchString(binding.ID) || !identifierPattern.MatchString(binding.ServiceAccountID) || len(binding.ResourceIDs) == 0 || len(binding.ResourceIDs) > 256 {
		return errors.New("workload binding identity mismatch")
	}
	key, err := LoadOrCreatePrivateKey(config.WireGuardPrivateKeyFile)
	if err != nil {
		return err
	}
	httpClient, publicKey, err := config.ControlHTTPClient()
	if err != nil {
		return err
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		return err
	}
	client, err := NewControlClient(config.ControlURL, config.RuntimeID, httpClient, schemas, config.MaxClockSkew)
	if err != nil {
		return err
	}
	client.runtimeKind = "endpoint"
	system, err := NewLinuxSystem(config.IPPath, config.NFTPath, "")
	if err != nil {
		return err
	}
	defer func() { _ = system.Close() }()
	executor, err := NewExecutor(key, system, "")
	if err != nil {
		return err
	}
	executor.endpoint = true
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	err = executor.Disable(cleanup)
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_ = executor.Disable(cleanup)
	}()
	control := &workloadControl{client: client, binding: binding, apiURL: origin, tokenFile: tokenFile, devicePublicKey: publicKey, key: key}
	runtime, err := NewRuntime(config.RuntimeID, control, executor, nil, min(config.PollInterval, 30*time.Second), config.HeartbeatInterval, config.MaxClockSkew, logger)
	if err != nil {
		return err
	}
	runtime.runtimeKind = "endpoint"
	if err := runtime.ConfigureState(filepath.Join(filepath.Dir(config.WireGuardPrivateKeyFile), "workload-configuration-floor.json"), client.origin, key.PublicKey().String()); err != nil {
		return err
	}
	return runtime.Run(ctx)
}

func (c *workloadControl) Configuration(ctx context.Context) (networkprotocol.RuntimeMessage, error) {
	if !c.credentialUntil.After(time.Now().Add(time.Minute)) {
		if err := c.enroll(ctx); err != nil {
			return networkprotocol.RuntimeMessage{}, err
		}
		c.sessionUntil = time.Time{}
	}
	if !c.sessionUntil.After(time.Now().Add(30 * time.Second)) {
		if err := c.connect(ctx); err != nil {
			return networkprotocol.RuntimeMessage{}, err
		}
	}
	return c.client.Configuration(ctx)
}
func (c *workloadControl) Report(ctx context.Context, applied networkprotocol.ConfigurationApplied) error {
	return c.client.Report(ctx, applied)
}

func (c *workloadControl) enroll(ctx context.Context) error {
	raw, err := securefile.Read(c.tokenFile, 4096, true)
	if err != nil {
		return err
	}
	defer clear(raw)
	token := strings.TrimSpace(string(raw))
	if !strings.HasPrefix(token, "soha_sat_") || strings.ContainsAny(token, "\r\n\t ") {
		return errors.New("invalid workload service-account token file")
	}
	input := sohaapi.NetworkRuntimeEnrollmentInput{RuntimeID: c.binding.RuntimeID, RuntimeKind: sohaapi.NetworkRuntimeKindEndpoint, DeviceID: c.binding.DeviceID, SubjectID: "service_account:" + c.binding.ServiceAccountID, TTLSeconds: 300}
	body, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.apiURL+"/api/v1/network-access/enrollments", bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := c.client.http.Do(request)
	if err != nil {
		return errors.New("workload enrollment API unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err = readRuntimeResponse(response, http.StatusCreated)
	if err != nil {
		return err
	}
	defer clear(raw)
	var result struct {
		Data sohaapi.NetworkRuntimeEnrollmentSecret `json:"data"`
	}
	if json.Unmarshal(raw, &result) != nil {
		return errors.New("invalid workload enrollment response")
	}
	accepted, err := c.client.Enroll(ctx, Enrollment{EnrollmentID: result.Data.Enrollment.ID, ChallengeID: result.Data.Enrollment.ChallengeID, DeviceID: c.binding.DeviceID, DevicePublicKey: c.devicePublicKey, WireGuardPublicKey: c.key.PublicKey().String(), ClientVersion: "network-workload/v1", Capabilities: []string{"workload-identity-v1", "wireguard-ipv6-v1"}, Token: result.Data.Token})
	if err != nil {
		return err
	}
	if accepted.CredentialExpiresAt == nil || !accepted.CredentialExpiresAt.After(time.Now()) {
		return errors.New("workload credential expiry missing")
	}
	c.credentialUntil = *accepted.CredentialExpiresAt
	return nil
}

func (c *workloadControl) connect(ctx context.Context) error {
	payload := networkprotocol.VPNConnectRequest{WorkloadID: c.binding.ID, RequestID: uuid.NewString(), SiteID: c.binding.SiteID, NetworkSpaceID: c.binding.NetworkSpaceID, Mode: domain.ModeExternalDirectZTNA, ResourceIDs: slices.Clone(c.binding.ResourceIDs)}
	_, raw, err := c.client.runtimeMessage(networkprotocol.MessageVPNConnectRequest, payload)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.client.origin+"/api/network-control/v1/runtimes/"+url.PathEscape(c.binding.RuntimeID)+"/vpn:connect", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.http.Do(request)
	if err != nil {
		return errors.New("workload control unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	raw, err = readRuntimeResponse(response, http.StatusOK)
	if err != nil {
		return err
	}
	if err := c.client.schemas.ValidateRuntime(raw); err != nil {
		return err
	}
	message, err := networkprotocol.DecodeRuntimeMessage(raw)
	if err != nil || message.RuntimeID != c.binding.RuntimeID || message.RuntimeKind != "endpoint" || message.ProducerID != "network-control" || message.MessageType != networkprotocol.MessageVPNConnectResult {
		return errors.New("workload connection response identity mismatch")
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, time.Now(), c.client.maxClockSkew); err != nil {
		return err
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.VPNConnectResult](message.Payload)
	if err != nil || result.Decision != "allow" || result.SessionID == "" {
		return errors.New("workload resource authorization denied")
	}
	c.sessionUntil = result.ValidUntil
	return nil
}

func validateWorkloadDesired(desired networkprotocol.ConfigurationDesired, key wgtypes.Key, now time.Time) error {
	if desired.ConfigurationVersion < 1 || desired.PolicyVersion < 1 || !desired.ValidUntil.After(now) || desired.Mihomo != nil || len(desired.NetworkLeases) != 0 {
		return errors.New("invalid resource-only workload configuration")
	}
	if desired.WireGuard == nil {
		if len(desired.ResourceLeases) != 0 {
			return errors.New("workload lease without tunnel")
		}
		return nil
	}
	return validateWorkloadTunnel(*desired.WireGuard, key, len(desired.ResourceLeases))
}
func validateWorkloadTunnel(configuration networkprotocol.WireGuardConfiguration, key wgtypes.Key, resourceCount int) error {
	wg := &configuration
	if wg.Role != "endpoint" || wg.InterfaceName != defaultInterfaceName || wg.PublicKey != key.String() || wg.RoutingMode != "routed" || wg.FirewallDefault != "deny" || wg.MTU < 1280 || wg.MTU > 1500 || len(wg.Addresses) != 1 || len(wg.Peers) != 1 || len(wg.Routes) == 0 || len(wg.Routes) > 128 || resourceCount == 0 {
		return errors.New("invalid workload tunnel")
	}
	if _, err := canonicalIPPrefix(wg.Addresses[0], -2); err != nil {
		return err
	}
	if err := validateWorkloadPeer(wg.Peers[0], wg.Routes); err != nil {
		return err
	}

	for _, raw := range wg.Routes {
		prefix, err := canonicalIPPrefix(raw, -1)
		if err != nil || prefix.Bits() == 0 {
			return errors.New("invalid workload route")
		}
	}
	return nil
}

func validWorkloadEndpoint(host string) bool {
	if address, err := netip.ParseAddr(host); err == nil {
		return address.IsGlobalUnicast() && !address.Is4In6()
	}
	return endpointHostPattern.MatchString(host)
}

func validateWorkloadPeer(peer networkprotocol.WireGuardPeer, routes []string) error {
	if _, err := wgtypes.ParseKey(peer.PublicKey); err != nil {
		return err
	}
	if !validWorkloadEndpoint(peer.EndpointHost) || peer.EndpointPort < 1 || peer.EndpointPort > 65535 || !slices.Equal(sortedStrings(peer.AllowedIPs), sortedStrings(routes)) {
		return errors.New("invalid workload peer")
	}
	return nil
}
