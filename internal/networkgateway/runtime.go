package networkgateway

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	randv2 "math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/redaction"
)

type Control interface {
	Configuration(context.Context) (networkprotocol.RuntimeMessage, error)
	Report(context.Context, networkprotocol.ConfigurationApplied) error
}

type ConfigurationApplier interface {
	Apply(context.Context, networkprotocol.ConfigurationDesired, time.Time) ApplyOutcome
	Disable(context.Context) error
}

type Telemetry interface {
	Heartbeat(context.Context, RuntimeStatus) error
}

type RuntimeStatus struct {
	desired              *networkprotocol.ConfigurationDesired
	Status               string
	ConfigurationVersion int
	PolicyVersion        int
	UptimeSeconds        int64
	Diagnostics          []string
}

type Runtime struct {
	runtimeID         string
	control           Control
	applier           ConfigurationApplier
	telemetry         Telemetry
	pollInterval      time.Duration
	heartbeatInterval time.Duration
	maxClockSkew      time.Duration
	logger            *slog.Logger
	startedAt         time.Time
	now               func() time.Time
	intervalJitter    func(time.Duration) time.Duration
	wait              func(context.Context, time.Duration) bool

	mu                   sync.RWMutex
	configurationVersion int
	attemptedVersion     int
	policyVersion        int
	validUntil           time.Time
	status               string
	diagnostic           string
	terminalReason       string
	disabled             bool
	pending              *networkprotocol.ConfigurationApplied
	probe                *networkprotocol.VPNProbeConfiguration
	desired              *networkprotocol.ConfigurationDesired
}

func NewRuntime(runtimeID string, control Control, applier ConfigurationApplier, telemetry Telemetry, pollInterval, heartbeatInterval, maxClockSkew time.Duration, logger *slog.Logger) (*Runtime, error) {
	if !identifierPattern.MatchString(runtimeID) || control == nil || applier == nil || logger == nil || pollInterval <= 0 || pollInterval > 5*time.Minute || heartbeatInterval <= 0 || heartbeatInterval > 5*time.Minute || maxClockSkew <= 0 || maxClockSkew > 5*time.Minute {
		return nil, fmt.Errorf("network gateway runtime dependencies and intervals are invalid")
	}
	now := time.Now
	return &Runtime{runtimeID: runtimeID, control: control, applier: applier, telemetry: telemetry, pollInterval: pollInterval, heartbeatInterval: heartbeatInterval, maxClockSkew: maxClockSkew, logger: logger, startedAt: now().UTC(), now: now, intervalJitter: runtimeIntervalJitter, wait: waitForRuntime, status: "unhealthy", diagnostic: "configuration_unavailable", disabled: true}, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	poll := time.NewTimer(0)
	defer poll.Stop()
	var heartbeatDone <-chan struct{}
	if r.telemetry != nil {
		done := make(chan struct{})
		heartbeatDone = done
		go func() {
			defer close(done)
			r.runHeartbeats(ctx)
		}()
	}
	defer func() {
		if heartbeatDone != nil {
			<-heartbeatDone
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-poll.C:
			if err := r.Cycle(ctx); err != nil && ctx.Err() == nil {
				r.logger.Error("network gateway cycle failed", "event", "network_gateway.cycle.failed", "error", redaction.LogText(err.Error(), 2048))
			}
			poll.Reset(r.nextCycleDelay(r.now().UTC()))
		}
	}
}

func (r *Runtime) runHeartbeats(ctx context.Context) {
	delay, retry := time.Duration(0), time.Second
	for {
		if delay > 0 && !r.wait(ctx, r.intervalJitter(delay)) {
			return
		}
		if ctx.Err() != nil {
			return
		}
		if err := r.telemetry.Heartbeat(ctx, r.Status()); err != nil {
			if ctx.Err() != nil {
				return
			}
			r.logger.Warn("network gateway heartbeat failed", "event", "network_gateway.heartbeat.failed", "error", redaction.LogText(err.Error(), 2048))
			delay, retry = retry, min(retry*2, 30*time.Second)
			continue
		}
		_, delay = r.intervals()
		retry = time.Second
	}
}

func (r *Runtime) Cycle(ctx context.Context) error {
	now := r.now().UTC()
	if err := r.expire(ctx, now); err != nil {
		return err
	}
	if err := r.reportPending(ctx); err != nil {
		return err
	}
	message, err := r.control.Configuration(ctx)
	if err != nil {
		r.setDegraded("control_unavailable")
		return fmt.Errorf("poll desired configuration: %w", err)
	}
	desired, err := r.decodeDesired(message, now)
	if err != nil {
		return err
	}
	r.applyRuntimeIntervals(desired.RuntimeIntervals)
	stop, err := r.handleKnownVersion(desired)
	if stop || err != nil {
		return err
	}
	outcome := r.applier.Apply(ctx, desired, now)
	report := networkprotocol.ConfigurationApplied{ConfigurationVersion: desired.ConfigurationVersion, PolicyVersion: desired.PolicyVersion, Status: outcome.Status, ReadbackHash: outcome.ReadbackHash, ReasonCode: outcome.ReasonCode}
	r.recordOutcome(desired, outcome, report)
	if err := r.control.Report(ctx, report); err != nil {
		return fmt.Errorf("report configuration result: %w", err)
	}
	r.clearPending()
	if outcome.Status != "applied" {
		return fmt.Errorf("configuration %d was %s: %s", desired.ConfigurationVersion, outcome.Status, outcome.ReasonCode)
	}
	return nil
}

func (r *Runtime) reportPending(ctx context.Context) error {
	pending := r.pendingReport()
	if pending == nil {
		return nil
	}
	if err := r.control.Report(ctx, *pending); err != nil {
		r.setDegraded("configuration_report_failed")
		return fmt.Errorf("report applied configuration: %w", err)
	}
	r.clearPending()
	return nil
}

func (r *Runtime) decodeDesired(message networkprotocol.RuntimeMessage, now time.Time) (networkprotocol.ConfigurationDesired, error) {
	if message.SchemaVersion != networkprotocol.RuntimeSchemaVersion || message.MessageType != networkprotocol.MessageConfiguration || message.ProducerID != "network-control" || message.RuntimeID != r.runtimeID || message.RuntimeKind != "gateway" {
		r.setDegraded("invalid_configuration_message")
		return networkprotocol.ConfigurationDesired{}, fmt.Errorf("network control returned a configuration for a different runtime")
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, now, r.maxClockSkew); err != nil {
		r.setDegraded("expired_configuration_message")
		return networkprotocol.ConfigurationDesired{}, err
	}
	desired, err := networkprotocol.DecodePayload[networkprotocol.ConfigurationDesired](message.Payload)
	if err != nil || !message.ExpiresAt.Equal(desired.ValidUntil) {
		r.setDegraded("invalid_configuration_payload")
		return networkprotocol.ConfigurationDesired{}, fmt.Errorf("network control returned an invalid configuration payload")
	}
	return desired, nil
}

func (r *Runtime) handleKnownVersion(desired networkprotocol.ConfigurationDesired) (bool, error) {
	r.mu.RLock()
	currentVersion, attemptedVersion, terminalReason := r.configurationVersion, r.attemptedVersion, r.terminalReason
	r.mu.RUnlock()
	if desired.ConfigurationVersion < max(currentVersion, attemptedVersion) {
		r.setDegraded("stale_configuration")
		return true, fmt.Errorf("network control returned a stale configuration")
	}
	if desired.ConfigurationVersion == attemptedVersion && attemptedVersion > currentVersion {
		r.setDegraded(terminalReason)
		return true, nil
	}
	if desired.ConfigurationVersion == currentVersion && currentVersion > 0 {
		r.mu.Lock()
		r.probe = desired.VPNProbe
		r.desired = &desired
		r.validUntil, r.status, r.diagnostic, r.disabled = desired.ValidUntil, "healthy", "", false
		r.mu.Unlock()
		return true, nil
	}
	return false, nil
}

func (r *Runtime) recordOutcome(desired networkprotocol.ConfigurationDesired, outcome ApplyOutcome, report networkprotocol.ConfigurationApplied) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if outcome.Status == "applied" {
		r.probe = desired.VPNProbe
		r.desired = &desired
		r.configurationVersion, r.attemptedVersion, r.policyVersion, r.validUntil = desired.ConfigurationVersion, desired.ConfigurationVersion, desired.PolicyVersion, desired.ValidUntil
		r.status, r.diagnostic, r.disabled = "healthy", "", false
		r.terminalReason = ""
	} else {
		r.attemptedVersion, r.status, r.diagnostic, r.terminalReason = desired.ConfigurationVersion, "degraded", outcome.ReasonCode, outcome.ReasonCode
	}
	r.pending = &report
}

func (r *Runtime) nextCycleDelay(now time.Time) time.Duration {
	r.mu.RLock()
	validUntil, disabled, pollInterval := r.validUntil, r.disabled, r.pollInterval
	r.mu.RUnlock()
	delay := r.intervalJitter(pollInterval)
	if disabled || validUntil.IsZero() {
		return delay
	}
	untilExpiry := validUntil.Sub(now)
	if untilExpiry > 0 && untilExpiry < delay {
		return untilExpiry
	}
	return delay
}

func (r *Runtime) applyRuntimeIntervals(intervals *networkprotocol.RuntimeIntervals) {
	if intervals == nil || !validRuntimeInterval(intervals.HeartbeatIntervalSeconds) || !validRuntimeInterval(intervals.ConfigurationPollIntervalSeconds) {
		return
	}
	r.mu.Lock()
	r.heartbeatInterval = time.Duration(intervals.HeartbeatIntervalSeconds) * time.Second
	r.pollInterval = time.Duration(intervals.ConfigurationPollIntervalSeconds) * time.Second
	r.mu.Unlock()
}

func (r *Runtime) intervals() (time.Duration, time.Duration) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.pollInterval, r.heartbeatInterval
}

func validRuntimeInterval(seconds int) bool {
	return seconds >= networkprotocol.MinRuntimeIntervalSeconds && seconds <= networkprotocol.MaxRuntimeIntervalSeconds
}

func runtimeIntervalJitter(delay time.Duration) time.Duration {
	span := delay / 5
	if span == 0 {
		return delay
	}
	return delay - span + time.Duration(randv2.Int64N(int64(2*span)+1)) // #nosec G404 -- scheduling jitter has no security or identity role.
}

func waitForRuntime(ctx context.Context, delay time.Duration) bool {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (r *Runtime) expire(ctx context.Context, now time.Time) error {
	r.mu.RLock()
	expired := !r.validUntil.IsZero() && !r.validUntil.After(now) && !r.disabled
	r.mu.RUnlock()
	if !expired {
		return nil
	}
	if err := r.applier.Disable(ctx); err != nil {
		r.setUnhealthy("expired_configuration_disable_failed")
		return fmt.Errorf("disable expired gateway configuration: %w", err)
	}
	r.setUnhealthy("configuration_expired")
	r.mu.Lock()
	r.disabled = true
	r.mu.Unlock()
	return nil
}

func (r *Runtime) Ready() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.configurationVersion > 0 && !r.disabled && r.validUntil.After(r.now().UTC())
}

func (r *Runtime) Status() RuntimeStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()
	diagnostics := []string{}
	if r.diagnostic != "" {
		diagnostics = append(diagnostics, r.diagnostic)
	}
	var desired *networkprotocol.ConfigurationDesired
	if !r.disabled && r.validUntil.After(r.now().UTC()) {
		desired = r.desired
	}
	return RuntimeStatus{desired: desired, Status: r.status, ConfigurationVersion: r.configurationVersion, PolicyVersion: r.policyVersion, UptimeSeconds: max(0, int64(r.now().UTC().Sub(r.startedAt)/time.Second)), Diagnostics: diagnostics}
}

func (r *Runtime) Handler() http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /healthz", func(response http.ResponseWriter, _ *http.Request) {
		writeRuntimeStatus(response, http.StatusOK, "ok")
	})
	router.HandleFunc("GET /readyz", func(response http.ResponseWriter, _ *http.Request) {
		if !r.Ready() {
			writeRuntimeStatus(response, http.StatusServiceUnavailable, "not_ready")
			return
		}
		writeRuntimeStatus(response, http.StatusOK, "ready")
	})
	return router
}

func writeRuntimeStatus(response http.ResponseWriter, status int, value string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"status": value})
}

func (r *Runtime) pendingReport() *networkprotocol.ConfigurationApplied {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.pending == nil {
		return nil
	}
	copy := *r.pending
	return &copy
}

func (r *Runtime) clearPending() {
	r.mu.Lock()
	r.pending = nil
	r.mu.Unlock()
}

func (r *Runtime) setDegraded(reason string) {
	r.mu.Lock()
	r.status, r.diagnostic = "degraded", reason
	r.mu.Unlock()
}

func (r *Runtime) setUnhealthy(reason string) {
	r.mu.Lock()
	r.status, r.diagnostic = "unhealthy", reason
	r.mu.Unlock()
}
