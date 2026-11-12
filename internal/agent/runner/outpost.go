package runner

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/buildinfo"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractauth "github.com/opensoha/soha-contracts/auth"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

const maxOutpostEventAttempts = 3
const outpostConfigClockSkew = time.Minute

var (
	errOutpostUnavailable = errors.New("outpost configuration unavailable")
	errOutpostDenied      = errors.New("outpost access denied")
)

type ForwardAuthRequest struct {
	Method       string
	OriginalURL  string
	RequestHost  string
	RequestPath  string
	SessionToken string
	SourceIP     string
}

type ForwardAuthResult struct {
	Decision    string
	Headers     map[string]string
	RedirectURL string
	StatusCode  int
}

type OutpostStatus struct {
	InstanceID               string `json:"instanceId,omitempty"`
	ProcessEpoch             string `json:"processEpoch,omitempty"`
	AcceptingRequests        bool   `json:"acceptingRequests"`
	ActiveChecks             int    `json:"activeChecks"`
	AppliedControlGeneration int64  `json:"appliedControlGeneration"`
	Enabled                  bool   `json:"enabled"`
	Ready                    bool   `json:"ready"`
	ErrorCode                string `json:"errorCode,omitempty"`
	OutpostID                string `json:"outpostId,omitempty"`
	ConfigurationVersion     int64  `json:"configurationVersion,omitempty"`
	DesiredVersion           int64  `json:"desiredConfigurationVersion,omitempty"`
	ExpiresAt                string `json:"expiresAt,omitempty"`
	LastClaimAt              string `json:"lastClaimAt,omitempty"`
	LastHeartbeatAt          string `json:"lastHeartbeatAt,omitempty"`
	ClaimFailures            int64  `json:"claimFailures"`
	HeartbeatFailures        int64  `json:"heartbeatFailures"`
	CheckFailures            int64  `json:"checkFailures"`
	Denied                   int64  `json:"denied"`
}

type outpostRuntime struct {
	mu                 sync.RWMutex
	instanceID         string
	processEpoch       string
	controlEnabled     bool
	control            *sohaapi.IdentityOutpostRuntimeControl
	controlDeadline    time.Time
	controlError       string
	activeChecks       int
	config             *sohaapi.IdentityOutpostRuntimeConfig
	leaseDeadline      time.Time
	publicKey          ed25519.PublicKey
	trustedKeys        map[string]ed25519.PublicKey
	floor              outpostConfigFloor
	state              *outpostStateStore
	lastClaimAt        time.Time
	lastHeartbeatAt    time.Time
	claimFailures      int64
	heartbeatFailures  int64
	checkFailures      int64
	denied             int64
	desiredVersion     int64
	contactOutpostID   string
	configurationError string
	persistenceFailed  bool
}

func newOutpostRuntime(encodedPublicKey string) (*outpostRuntime, error) {
	key, err := decodeBase64(strings.TrimSpace(encodedPublicKey))
	if err != nil || len(key) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("decode outpost trust public key: expected %d-byte Ed25519 key", ed25519.PublicKeySize)
	}
	return &outpostRuntime{publicKey: ed25519.PublicKey(key)}, nil
}

func newConfiguredOutpostRuntime(cfg cfgpkg.OutpostConfig, baseURL string) (*outpostRuntime, error) {
	keys, err := cfg.TrustedKeys()
	if err != nil {
		return nil, err
	}
	instanceID := strings.TrimSpace(cfg.InstanceID)
	if instanceID == "" {
		instanceID, err = os.Hostname()
		if err != nil || strings.TrimSpace(instanceID) == "" {
			return nil, errors.New("outpost instance identity unavailable")
		}
	}
	if len(instanceID) > 128 {
		return nil, errors.New("outpost instance identity exceeds 128 bytes")
	}
	epoch, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("create outpost process epoch: %w", err)
	}
	store, floor, err := openOutpostInstanceState(cfg.StateDir, baseURL, cfg.AgentID, instanceID)
	if err != nil {
		return nil, err
	}
	if floor.Managed && !cfg.RuntimeControlEnabled {
		store.close()
		return nil, errors.New("managed outpost runtime control cannot be disabled")
	}
	if cfg.RuntimeControlEnabled && !floor.Managed {
		floor.Managed = true
		if err := store.save(floor); err != nil {
			store.close()
			return nil, err
		}
	}
	return &outpostRuntime{trustedKeys: keys, state: store, floor: floor, instanceID: instanceID, processEpoch: epoch.String(), controlEnabled: cfg.RuntimeControlEnabled}, nil
}

func (r *Runner) startOutpost(ctx context.Context) {
	if !r.cfg.Outpost.Enabled {
		return
	}
	runtime, err := newConfiguredOutpostRuntime(r.cfg.Outpost, r.cfg.BaseURL)
	if err != nil {
		r.outpost = &outpostRuntime{configurationError: "configuration_state_unavailable"}
		r.logger.Error("outpost runtime disabled", zap.Error(err))
		return
	}
	r.outpost = runtime
	go r.outpostLoop(ctx)
}

func (r *Runner) outpostLoop(ctx context.Context) {
	defer r.outpost.state.close()
	poll := positiveDuration(r.cfg.Outpost.PollInterval, 5*time.Second)
	heartbeat := positiveDuration(r.cfg.Outpost.HeartbeatInterval, 15*time.Second)
	pollTicker := time.NewTicker(poll)
	heartbeatTicker := time.NewTicker(heartbeat)
	defer pollTicker.Stop()
	defer heartbeatTicker.Stop()
	r.claimOutpostConfig(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-pollTicker.C:
			r.claimOutpostConfig(ctx)
		case <-heartbeatTicker.C:
			r.heartbeatOutpost(ctx)
		}
	}
}

func (r *Runner) claimOutpostConfig(ctx context.Context) {
	current := r.OutpostStatus().ConfigurationVersion
	request := sohaapi.IdentityOutpostClaimRequest{
		AgentID: r.cfg.Outpost.AgentID, CurrentConfigurationVersion: current,
		SupportedProtocolVersion: r.cfg.Outpost.ProtocolVersion,
		RuntimeVersion:           buildinfo.Version,
	}
	if r.outpost.controlEnabled {
		request.InstanceID, request.ProcessEpoch = r.outpost.instanceID, r.outpost.processEpoch
		request.SupportedRuntimeControlVersion = "v1"
	}
	config, err := r.apiClient().ClaimIdentityOutpostConfig(ctx, request)
	if err != nil {
		r.outpost.markClaimFailure()
		return
	}
	if config == nil {
		return
	}
	// The authenticated claim identifies the heartbeat target, even if its signed
	// configuration cannot be applied. It grants no routing authority.
	r.outpost.mu.Lock()
	r.outpost.contactOutpostID = config.OutpostID
	r.outpost.mu.Unlock()
	if err := r.outpost.apply(*config, r.cfg.Outpost.TrustKeyID, time.Now()); err != nil {
		r.outpost.mu.Lock()
		r.outpost.claimFailures++
		r.outpost.configurationError = "configuration_rejected"
		r.outpost.mu.Unlock()
		return
	}
	if config.ConfigurationVersion != current {
		r.reportOutpostEvent(ctx, sohaapi.ConfigurationApplied, "configuration_applied", "")
	}
}

func (r *Runner) heartbeatOutpost(ctx context.Context) {
	status := r.OutpostStatus()
	if status.OutpostID == "" {
		return
	}
	state := sohaapi.IdentityOutpostHeartbeatRequestStatusHealthy
	errorCode := status.ErrorCode
	if !status.Ready {
		state = sohaapi.IdentityOutpostHeartbeatRequestStatusUnavailable
		if errorCode == "" {
			errorCode = "configuration_unavailable"
		}
	}
	var expiresAt *time.Time
	if parsed, err := time.Parse(time.RFC3339, status.ExpiresAt); err == nil {
		expiresAt = &parsed
	}
	request := sohaapi.IdentityOutpostHeartbeatRequest{
		AgentID: r.cfg.Outpost.AgentID, CheckedAt: time.Now().UTC(), ConfigurationVersion: status.ConfigurationVersion,
		Status: state, ErrorCode: errorCode, RuntimeVersion: buildinfo.Version, ConfigurationExpiresAt: expiresAt,
	}
	if r.outpost.controlEnabled {
		request.InstanceID, request.ProcessEpoch = status.InstanceID, status.ProcessEpoch
		request.AcceptingRequests, request.ActiveChecks, request.AppliedControlGeneration = &status.AcceptingRequests, &status.ActiveChecks, &status.AppliedControlGeneration
	}
	response, err := r.apiClient().HeartbeatIdentityOutpost(ctx, status.OutpostID, request)
	if err == nil {
		r.outpost.markDesiredVersion(response.DesiredConfigurationVersion)
	}
	if err != nil || !response.Accepted {
		r.outpost.markHeartbeatFailure()
		return
	}
	r.outpost.markHeartbeat()
	if response.DesiredConfigurationVersion > status.ConfigurationVersion {
		r.claimOutpostConfig(ctx)
	}
}

func (r *Runner) CheckOutpostAccess(ctx context.Context, request ForwardAuthRequest) (ForwardAuthResult, error) {
	if r == nil || r.outpost == nil {
		return ForwardAuthResult{Decision: "deny", StatusCode: http.StatusServiceUnavailable}, errOutpostUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := r.outpost.beginCheck(time.Now()); err != nil {
		return ForwardAuthResult{Decision: "deny", StatusCode: http.StatusServiceUnavailable}, err
	}
	defer r.outpost.finishCheck()
	config, route, skip, err := r.outpost.route(request.RequestHost, request.RequestPath, time.Now())
	if err != nil {
		return ForwardAuthResult{Decision: "deny", StatusCode: http.StatusServiceUnavailable}, err
	}
	if skip {
		return ForwardAuthResult{Decision: "allow", StatusCode: http.StatusNoContent}, nil
	}
	checked, err := r.apiClient().CheckIdentityOutpostAccess(ctx, config.OutpostID, sohaapi.IdentityOutpostAccessCheckRequest{
		ConfigurationVersion: config.ConfigurationVersion, Method: request.Method, OriginalURL: request.OriginalURL,
		ProviderID: route.ProviderID, RequestHost: request.RequestHost, RequestPath: request.RequestPath,
		SessionToken: request.SessionToken, SourceIP: request.SourceIP,
	})
	if err != nil || !validOutpostAccessCheck(checked) {
		r.outpost.markCheckFailure()
		r.reportOutpostEvent(ctx, sohaapi.CheckFailed, "check_failed", "core_check_failed")
		return ForwardAuthResult{Decision: "deny", StatusCode: http.StatusServiceUnavailable}, errOutpostUnavailable
	}
	result := ForwardAuthResult{
		Decision: string(checked.Decision), Headers: cleanOutpostHeaders(checked.Headers),
		RedirectURL: checked.RedirectURL, StatusCode: checked.StatusCode,
	}
	if checked.Decision == sohaapi.IdentityOutpostAccessCheckDecisionAllow {
		return result, nil
	}
	r.outpost.markDenied()
	r.reportOutpostEvent(ctx, sohaapi.CheckDenied, "check_denied", checked.Reason)
	return result, errOutpostDenied
}

func validOutpostAccessCheck(checked sohaapi.IdentityOutpostAccessCheck) bool {
	if !checked.Decision.Valid() {
		return false
	}
	switch checked.Decision {
	case sohaapi.IdentityOutpostAccessCheckDecisionAllow:
		return checked.StatusCode >= 200 && checked.StatusCode < 300
	case sohaapi.IdentityOutpostAccessCheckDecisionDeny:
		return checked.StatusCode >= 400 && checked.StatusCode < 500
	case sohaapi.IdentityOutpostAccessCheckDecisionRedirect:
		if checked.StatusCode < 300 || checked.StatusCode >= 400 {
			return false
		}
		redirect := strings.TrimSpace(checked.RedirectURL)
		if strings.HasPrefix(redirect, "/") {
			return !strings.HasPrefix(redirect, "//")
		}
		parsed, err := url.Parse(redirect)
		return err == nil && parsed.Scheme == "https" && parsed.Host != ""
	default:
		return false
	}
}

func (r *Runner) OutpostStatus() OutpostStatus {
	if r == nil || !r.cfg.Outpost.Enabled {
		return OutpostStatus{}
	}
	if r.outpost == nil {
		return OutpostStatus{Enabled: true}
	}
	return r.outpost.status(time.Now())
}

func (r *Runner) reportOutpostEvent(ctx context.Context, eventType sohaapi.IdentityOutpostRuntimeEventType, code, message string) {
	status := r.OutpostStatus()
	if status.OutpostID == "" {
		return
	}
	request := sohaapi.IdentityOutpostEventBatchRequest{AgentID: r.cfg.Outpost.AgentID, Events: []sohaapi.IdentityOutpostRuntimeEvent{{
		ID: uuid.NewString(), Type: eventType, Code: code, Message: truncate(message, 256),
		ConfigurationVersion: status.ConfigurationVersion, OccurredAt: time.Now().UTC(),
	}}}
	for attempt := 0; attempt < maxOutpostEventAttempts; attempt++ {
		if _, err := r.apiClient().AppendIdentityOutpostEvents(ctx, status.OutpostID, request); err == nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt+1) * 100 * time.Millisecond):
		}
	}
}

func (o *outpostRuntime) apply(config sohaapi.IdentityOutpostRuntimeConfig, trustKeyID string, now time.Time) error {
	key := o.publicKey
	if o.trustedKeys != nil {
		key = o.trustedKeys[config.KeyID]
	} else if strings.TrimSpace(config.KeyID) != strings.TrimSpace(trustKeyID) {
		key = nil
	}
	if len(key) != ed25519.PublicKeySize {
		return errors.New("outpost configuration uses an untrusted key")
	}
	if strings.TrimSpace(config.OutpostID) == "" || config.ProtocolVersion != "v1" || strings.TrimSpace(config.CheckURL) == "" ||
		config.ConfigurationVersion <= 0 || !config.IssuedAt.Before(config.ExpiresAt) || config.IssuedAt.After(now.Add(outpostConfigClockSkew)) || !now.Before(config.ExpiresAt) {
		return errors.New("outpost configuration is malformed or expired")
	}
	if err := validateOutpostRoutes(config.Routes); err != nil {
		return err
	}
	signature, err := decodeBase64(config.Signature)
	if err != nil || !ed25519.Verify(key, outpostConfigPayload(config), signature) {
		return errors.New("outpost configuration signature is invalid")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.persistenceFailed {
		return errors.New("outpost durable state requires restart after persistence failure")
	}
	if err := o.applyConfigurationLocked(config, now); err != nil {
		return err
	}
	if o.controlEnabled {
		return o.applyControlLocked(config.RuntimeControl, config.OutpostID, trustKeyID, now)
	}
	return nil
}

func (o *outpostRuntime) applyConfigurationLocked(config sohaapi.IdentityOutpostRuntimeConfig, now time.Time) error {
	next := newOutpostConfigFloor(config)
	// Control renewals do not renew or resurrect the configuration lease.
	if o.controlEnabled && o.config != nil && o.floor.sameLease(next) {
		return nil
	}
	if err := o.floor.accepts(next); err != nil {
		return err
	}
	next.Managed, next.Control = o.floor.Managed, o.floor.Control
	if o.state != nil {
		if err := o.state.save(next); err != nil {
			o.configurationError = "configuration_persistence_failed"
			o.persistenceFailed = true
			return fmt.Errorf("persist outpost configuration floor: %w", err)
		}
	}
	o.floor = next
	copy := config
	o.config = &copy
	o.leaseDeadline = now.Add(min(config.ExpiresAt.Sub(now), config.ExpiresAt.Sub(config.IssuedAt)))
	o.configurationError = ""
	o.desiredVersion = config.ConfigurationVersion
	o.lastClaimAt = now.UTC()
	return nil
}

func validateOutpostRoutes(routes []sohaapi.IdentityOutpostRoute) error {
	for _, route := range routes {
		if strings.TrimSpace(route.Host) == "" || strings.TrimSpace(route.ProviderID) == "" ||
			(strings.TrimSpace(route.PathPrefix) != "" && !strings.HasPrefix(route.PathPrefix, "/")) {
			return errors.New("outpost configuration contains an invalid route")
		}
		for _, skipPath := range route.SkipPaths {
			if !strings.HasPrefix(strings.TrimSpace(skipPath), "/") {
				return errors.New("outpost configuration contains an invalid skip path")
			}
		}
	}
	return nil
}

func outpostConfigPayload(config sohaapi.IdentityOutpostRuntimeConfig) []byte {
	payload := struct {
		OutpostID            string                         `json:"outpostId"`
		ProtocolVersion      string                         `json:"protocolVersion"`
		ConfigurationVersion int64                          `json:"configurationVersion"`
		IssuedAt             time.Time                      `json:"issuedAt"`
		ExpiresAt            time.Time                      `json:"expiresAt"`
		KeyID                string                         `json:"keyId"`
		CheckURL             string                         `json:"checkUrl"`
		Routes               []sohaapi.IdentityOutpostRoute `json:"routes"`
	}{config.OutpostID, config.ProtocolVersion, config.ConfigurationVersion, config.IssuedAt, config.ExpiresAt, config.KeyID, config.CheckURL, config.Routes}
	encoded, _ := json.Marshal(payload)
	return encoded
}

func (o *outpostRuntime) route(host, requestPath string, now time.Time) (sohaapi.IdentityOutpostRuntimeConfig, sohaapi.IdentityOutpostRoute, bool, error) {
	o.mu.RLock()
	defer o.mu.RUnlock()
	if o.config == nil || o.configurationError != "" || (!now.Before(o.config.ExpiresAt) || !now.Before(o.leaseDeadline)) {
		return sohaapi.IdentityOutpostRuntimeConfig{}, sohaapi.IdentityOutpostRoute{}, false, errOutpostUnavailable
	}
	host = strings.ToLower(strings.TrimSpace(strings.Split(host, ":")[0]))
	cleanPath := path.Clean("/" + strings.TrimPrefix(requestPath, "/"))
	var selected *sohaapi.IdentityOutpostRoute
	for _, route := range o.config.Routes {
		prefix := path.Clean(firstNonEmpty(strings.TrimSpace(route.PathPrefix), "/"))
		if strings.EqualFold(host, strings.TrimSpace(route.Host)) && hasPathPrefix(cleanPath, prefix) {
			if selected == nil || len(prefix) > len(path.Clean(firstNonEmpty(selected.PathPrefix, "/"))) {
				copy := route
				selected = &copy
			}
		}
	}
	if selected != nil {
		for _, skipPath := range selected.SkipPaths {
			if hasPathPrefix(cleanPath, path.Clean(skipPath)) {
				return *o.config, *selected, true, nil
			}
		}
		return *o.config, *selected, false, nil
	}
	return sohaapi.IdentityOutpostRuntimeConfig{}, sohaapi.IdentityOutpostRoute{}, false, errOutpostUnavailable
}

func (o *outpostRuntime) status(now time.Time) OutpostStatus {
	o.mu.RLock()
	defer o.mu.RUnlock()
	status := OutpostStatus{Enabled: true, ClaimFailures: o.claimFailures, HeartbeatFailures: o.heartbeatFailures, CheckFailures: o.checkFailures, Denied: o.denied}
	status.InstanceID, status.ProcessEpoch = o.instanceID, o.processEpoch
	status.ActiveChecks = o.activeChecks
	if o.control != nil {
		status.AppliedControlGeneration = o.control.Generation
	}
	status.DesiredVersion = o.desiredVersion
	status.OutpostID, status.ErrorCode = o.contactOutpostID, o.configurationError
	if o.config != nil {
		status.Ready = o.configurationError == "" && now.Before(o.config.ExpiresAt) && now.Before(o.leaseDeadline)
		if !now.Before(o.config.ExpiresAt) || !now.Before(o.leaseDeadline) {
			status.ErrorCode = "configuration_expired"
		}
		status.OutpostID = o.config.OutpostID
		status.ConfigurationVersion = o.config.ConfigurationVersion
		status.ExpiresAt = o.config.ExpiresAt.UTC().Format(time.RFC3339)
	}
	status.AcceptingRequests = o.acceptingLocked(now)
	status.Ready = status.Ready && status.AcceptingRequests
	if status.ErrorCode == "" && o.controlEnabled && !status.AcceptingRequests {
		status.ErrorCode = o.controlStatusLocked(now)
	}
	if !o.lastClaimAt.IsZero() {
		status.LastClaimAt = o.lastClaimAt.Format(time.RFC3339)
	}
	if !o.lastHeartbeatAt.IsZero() {
		status.LastHeartbeatAt = o.lastHeartbeatAt.Format(time.RFC3339)
	}
	return status
}

func (o *outpostRuntime) markClaimFailure()     { o.mu.Lock(); o.claimFailures++; o.mu.Unlock() }
func (o *outpostRuntime) markHeartbeatFailure() { o.mu.Lock(); o.heartbeatFailures++; o.mu.Unlock() }
func (o *outpostRuntime) markHeartbeat() {
	o.mu.Lock()
	o.lastHeartbeatAt = time.Now().UTC()
	o.mu.Unlock()
}
func (o *outpostRuntime) markCheckFailure() { o.mu.Lock(); o.checkFailures++; o.mu.Unlock() }
func (o *outpostRuntime) markDenied()       { o.mu.Lock(); o.denied++; o.mu.Unlock() }
func (o *outpostRuntime) markDesiredVersion(version int64) {
	o.mu.Lock()
	o.desiredVersion = version
	o.mu.Unlock()
}

func cleanOutpostHeaders(headers map[string]string) map[string]string {
	clean := make(map[string]string, len(headers))
	for name, value := range headers {
		name = http.CanonicalHeaderKey(strings.TrimSpace(name))
		value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", ""), "\n", ""))
		if contractauth.IsOutpostIdentityHeader(name) && value != "" && len(value) <= 4096 {
			clean[name] = value
		}
	}
	return clean
}

func hasPathPrefix(requestPath, prefix string) bool {
	return prefix == "/" || requestPath == prefix || strings.HasPrefix(requestPath, strings.TrimSuffix(prefix, "/")+"/")
}

func decodeBase64(value string) ([]byte, error) {
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if decoded, err := encoding.DecodeString(strings.TrimSpace(value)); err == nil {
			return decoded, nil
		}
	}
	return nil, errors.New("invalid base64")
}

func positiveDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
func truncate(value string, size int) string {
	if len(value) <= size {
		return value
	}
	return value[:size]
}
