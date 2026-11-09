package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type enrollmentState struct {
	RuntimeID              string `json:"runtimeId"`
	ControlURL             string `json:"controlUrl"`
	EnrollmentID           string `json:"enrollmentId"`
	ChallengeID            string `json:"challengeId"`
	CertificateFingerprint string `json:"certificateFingerprint"`
}

type Runner struct {
	health            runtimeHealth
	logs              []hostLog
	logSequence       int64
	hostBootEpoch     string
	deployment        deploymentState
	ingress           *publicationIngress
	retiredIngress    []*publicationIngress
	commandReceipts   map[string]commandReceiptEntry
	config            Config
	client            *Client
	ingest            *IngestClient
	engine            *engine
	appliedRevision   int64
	enabled           bool
	lastAttempt       *domain.Applied
	attemptReported   bool
	validUntil        time.Time
	seenRevision      int64
	seenHash          string
	seenEnabled       bool
	seenPolicyVersion int
	suspended         bool
	reasonCode        string
}

func NewRunner(config Config) (*Runner, error) {
	if err := os.MkdirAll(config.StateDir, 0700); err != nil {
		return nil, fmt.Errorf("prepare proxy runtime state: %w", err)
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		return nil, err
	}
	client, err := config.ControlClient()
	if err != nil {
		return nil, err
	}
	client.SetSchemas(schemas)
	ingest, err := config.IngestClient()
	if err != nil {
		return nil, err
	}
	var secret string
	if config.Engine != domain.EngineV2Ray {
		secret, err = config.ControllerSecret()
		if err != nil {
			return nil, err
		}
	}
	engine := newEngine(config, secret)
	if err := engine.configureExternal(); err != nil {
		return nil, err
	}
	runner := &Runner{hostBootEpoch: rand.Text(), config: config, client: client, ingest: ingest, engine: engine}
	if err := runner.restoreConfigurationFloor(); err != nil {
		return nil, err
	}
	return runner, nil
}

func (r *Runner) enroll(ctx context.Context) error {
	if r.config.EnrollmentID == "" {
		return nil
	}
	publicKey, fingerprint, err := r.client.enrollmentIdentity()
	if err != nil {
		return err
	}
	state, err := json.Marshal(enrollmentState{
		RuntimeID: r.client.runtimeID, ControlURL: r.client.origin,
		EnrollmentID: r.config.EnrollmentID, ChallengeID: r.config.ChallengeID,
		CertificateFingerprint: fingerprint,
	})
	if err != nil {
		return err
	}
	marker := filepath.Join(r.config.StateDir, "enrolled")
	previous, err := securefile.Read(marker, 4096, true)
	if err == nil && bytes.Equal(previous, state) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	// Old markers lack a certificate binding. Authenticate before migrating
	// them so a healthy restart does not replay a consumed enrollment token.
	if bytes.Equal(previous, []byte(r.client.runtimeID)) {
		if _, err := r.client.Configuration(ctx); err == nil {
			return writeAtomic(marker, state)
		} else {
			var status controlStatusError
			if !errors.As(err, &status) || status != controlStatusError(http.StatusUnauthorized) {
				return err
			}
		}
	}
	token, err := r.config.EnrollmentToken()
	if err != nil {
		return err
	}
	if err := r.client.Enroll(ctx, r.config.EnrollmentID, r.config.ChallengeID, publicKey, token, "proxy-runtime/v1alpha1"); err != nil {
		return err
	}
	return writeAtomic(marker, state)
}

func (r *Runner) Run(ctx context.Context) error {
	if err := r.health.start(r.config.HealthListen); err != nil {
		return err
	}
	defer func() { r.health.close(); r.closeIngress(); r.engine.stop() }()
	if err := r.enroll(ctx); err != nil {
		return fmt.Errorf("enroll proxy runtime: %w", err)
	}
	lastWall := time.Now().UnixNano()
	var nextPoll time.Time
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		wall := time.Now().UnixNano()
		gap := time.Duration(wall - lastWall)
		lastWall = wall
		if gap < 0 || gap > max(3*r.config.PollInterval, 15*time.Second) {
			r.suspend("system_resume_requires_refresh")
		}
		now := time.Now()
		r.expire(now)
		// Lease boundaries close owned resources without accelerating HTTP retries.
		if !now.Before(nextPoll) {
			err := r.cycle(ctx)
			r.expire(time.Now())
			if err != nil && ctx.Err() == nil {
				slog.Warn("proxy runtime cycle failed", "error", err)
			}
			nextPoll = time.Now().Add(r.config.PollInterval)
		}
		now = time.Now()
		timer.Reset(max(time.Millisecond, min(r.nextCycleDelay(now), nextPoll.Sub(now))))
	}
}

func (r *Runner) cycle(ctx context.Context) (cycleErr error) {
	defer func() {
		if cycleErr != nil {
			r.health.invalidate()
		}
	}()
	r.expire(time.Now())
	controlCtx := ctx
	if r.enabled && !r.suspended {
		// A blocked refresh cannot keep serving beyond the current authorization.
		var cancel context.CancelFunc
		controlCtx, cancel = r.cycleContext(ctx)
		defer cancel()
	}
	if err := r.syncDeployment(controlCtx); err != nil {
		return err
	}
	if r.stopConfirmationRequired() {
		return r.confirmOwnedStop(ctx)
	}
	ctx, cycleCancel := r.cycleContext(ctx)
	defer cycleCancel()
	if err := r.syncConfiguration(ctx); err != nil {
		return err
	}
	// A newly granted configuration may expire before this first cycle ends.
	ctx, cancel := r.cycleContext(ctx)
	defer cancel()
	r.ensureRunning(ctx)
	r.refreshExistingIngressDeadline(time.Now())
	observation, connections := r.observe(ctx)
	r.publishReadiness(observation.Health == "healthy", time.Now())
	if err := r.client.Observe(ctx, observation); err != nil {
		return fmt.Errorf("report proxy observation: %w", err)
	}
	if connections != nil {
		if err := r.client.PutSnapshot(ctx, r.config.Engine, observation, connections); err != nil {
			return fmt.Errorf("report proxy connections: %w", err)
		}
	}
	closeErr := errors.Join(r.executeClose(ctx), r.executeCommand(ctx))
	if err := r.updatePublication(ctx); err != nil {
		return err
	}
	if err := r.ackDeployment(ctx, observation); err != nil {
		return err
	}
	if slices.Contains(observation.Capabilities, "traffic") {
		if err := r.ingest.Send(ctx, observation); err != nil {
			return errors.Join(closeErr, fmt.Errorf("report proxy traffic: %w", err))
		}
	}
	return closeErr
}

func (r *Runner) cycleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.config.ManagedDeployment && r.deployment.grant != nil {
		until := r.deployment.flowUntil()
		if r.enabled && !r.validUntil.IsZero() {
			until = minTime(until, r.validUntil)
		}
		return context.WithDeadline(ctx, until)
	}
	if r.enabled && !r.validUntil.IsZero() {
		return context.WithDeadline(ctx, r.validUntil)
	}
	return context.WithCancel(ctx)
}

func (r *Runner) nextCycleDelay(now time.Time) time.Duration {
	delay := r.config.PollInterval
	if r.config.ManagedDeployment && r.deployment.grant != nil {
		if until := r.deployment.flowUntil(); until.After(now) {
			delay = min(delay, until.Sub(now))
		}
	}
	if r.enabled && r.validUntil.After(now) {
		delay = min(delay, r.validUntil.Sub(now))
	}
	return max(time.Millisecond, delay)
}

func (r *Runner) suspend(reason string) {
	r.health.invalidate()
	if r.reasonCode != reason {
		r.recordEvent(reason)
	}
	r.closeIngress()
	r.engine.stop()
	r.enabled, r.suspended, r.reasonCode = false, true, reason
}

func (r *Runner) expire(now time.Time) {
	if r.config.ManagedDeployment && (r.deployment.grant == nil || !r.deployment.flowUntil().After(now)) {
		r.suspend("deployment_control_expired")
		return
	}
	if r.enabled && !r.validUntil.After(now) {
		r.suspend("configuration_expired")
	}
}

func (r *Runner) syncConfiguration(ctx context.Context) error {
	desired, err := r.client.Configuration(ctx)
	if err != nil {
		if isProtectionFailure(err) {
			r.suspend("protection_unavailable")
		}
		return fmt.Errorf("poll proxy configuration: %w", err)
	}
	if desired == nil {
		r.suspend("configuration_unavailable")
		return nil
	}
	if desired.Engine != r.config.Engine {
		r.suspend("engine_mismatch")
		return fmt.Errorf("desired proxy engine does not match runtime")
	}
	if desired.Revision < r.seenRevision || (desired.Revision == r.seenRevision &&
		(desired.Digest() != r.seenHash || desired.Enabled != r.seenEnabled)) {
		r.suspend("stale_configuration")
		return fmt.Errorf("desired proxy configuration regressed or changed without a revision")
	}
	if err := r.rememberConfiguration(*desired); err != nil {
		r.suspend("configuration_floor_unavailable")
		return err
	}
	r.applyConfiguration(ctx, *desired)
	if err := r.refreshAppliedConfigurationLease(desired); err != nil {
		return err
	}
	if r.lastAttempt != nil && !r.attemptReported {
		if err := r.client.Applied(ctx, *r.lastAttempt); err != nil {
			return fmt.Errorf("acknowledge proxy configuration: %w", err)
		}
		r.attemptReported = true
	}
	if r.config.ManagedDeployment && r.engine.egress != nil {
		r.engine.egress.accepting.Store(r.deployment.mayApply(time.Now()) && r.enabled && !r.suspended && r.attemptReported)
	}
	return nil
}

func (r *Runner) refreshAppliedConfigurationLease(desired *domain.Configuration) error {
	if r.appliedRevision == desired.Revision && !r.suspended {
		if r.config.ManagedDeployment && !r.deployment.drainLease.IsZero() {
			desired.ValidUntil = minTime(desired.ValidUntil, r.deployment.flowUntil())
		}
		if desired.Enabled {
			if err := r.engine.refreshEgress(desired.ValidUntil); err != nil {
				r.suspend("egress_lease_failed")
				return err
			}
		}
		r.validUntil = desired.ValidUntil
		r.engine.desired = *desired
		if r.engine.egress != nil {
			r.engine.egress.udpPolicy.Store(copyUDPPolicy(*desired))
		}
	}
	return nil
}

func (r *Runner) applyConfiguration(ctx context.Context, desired domain.Configuration) {
	if r.config.ManagedDeployment && !r.deployment.mayApply(time.Now()) {
		if r.enabled && (desired.Revision != r.appliedRevision || desired.Digest() != r.engine.desired.Digest() || !desired.Enabled) {
			r.suspend("configuration_changed_while_draining")
		}
		return
	}
	refreshBootstrap := desired.Enabled && time.Since(r.engine.bootstrapAt) >= 5*time.Minute
	var prepared *bootstrapPreparation
	if refreshBootstrap && desired.Revision == r.appliedRevision {
		var handled bool
		prepared, handled = r.preparePeriodicBootstrap(ctx, desired)
		if handled {
			return
		}
	}
	if desired.Revision != r.appliedRevision || r.suspended || refreshBootstrap {
		if r.lastAttempt == nil || r.lastAttempt.Revision != desired.Revision || r.suspended || refreshBootstrap {
			r.health.invalidate()
			applyCtx, cancel := context.WithDeadline(ctx, desired.ValidUntil)
			r.closeIngress()
			applied := r.engine.applyPrepared(applyCtx, desired, prepared)
			cancel()
			r.lastAttempt, r.attemptReported = &applied, false
			if applied.Status == "applied" {
				r.recordEvent("configuration_applied")
				r.appliedRevision, r.enabled = desired.Revision, desired.Enabled
				r.suspended, r.reasonCode = false, ""
			} else {
				// A rejected replacement may narrow access; do not continue its predecessor.
				r.suspend(applied.ReasonCode)
			}
		}
	}
}

func isProtectionFailure(err error) bool {
	var status controlStatusError
	return errors.Is(err, errUnsafeConfiguration) || errors.As(err, &status) && (status == 401 || status == 403 || status == 404 || status == 409)
}

func (r *Runner) ensureRunning(ctx context.Context) {
	if r.enabled && r.engine.health(ctx) != nil {
		r.suspend("engine_state_drift")
	}
}

func (r *Runner) executeClose(ctx context.Context) error {
	if !r.engine.adapter.supportsClose() || !r.enabled {
		return nil
	}
	command, err := r.client.NextClose(ctx)
	if err != nil {
		return fmt.Errorf("poll proxy close command: %w", err)
	}
	if command == nil || !command.ExpiresAt.After(time.Now()) {
		return nil
	}
	if command.ProcessEpoch != r.engine.processEpoch || command.ExpiresAt.After(time.Now().Add(30*time.Second)) {
		command.Status, command.ReasonCode = "failed", "epoch_mismatch"
		return r.client.CompleteClose(ctx, *command)
	}
	closeCtx, cancel := context.WithDeadline(ctx, command.ExpiresAt)
	command.Status, err = r.engine.close(closeCtx, command.ConnectionID)
	cancel()
	if err != nil {
		command.Status, command.ReasonCode = "failed", "controller_unavailable"
	}
	if !command.ExpiresAt.After(time.Now()) {
		return errors.New("proxy close result expired")
	}
	if err := r.client.CompleteClose(ctx, *command); err != nil {
		return fmt.Errorf("complete proxy close command: %w", err)
	}
	return nil
}

func (r *Runner) observe(ctx context.Context) (observation domain.Observation, connections []domain.Connection) {
	observation = domain.Observation{
		Engine: r.config.Engine, EngineVersion: "unknown", Health: "unhealthy",
		Capabilities: []string{}, UptimeSeconds: 0,
	}
	defer func() {
		if r.reasonCode != "" {
			observation.Health = "degraded"
			observation.ReasonCode = r.reasonCode
		}
	}()
	if !r.enabled || r.engine.process == nil {
		return observation, nil
	}
	r.engine.sampleSequence++
	observation.ProcessEpoch, observation.SampleSequence = r.engine.processEpoch, r.engine.sampleSequence
	observation.EngineDescriptor = r.engine.desired.EngineDescriptor
	observation.UptimeSeconds = int64(time.Since(r.engine.startedAt).Seconds())
	sample, connections := r.engine.adapter.observe(ctx)
	observation.EngineVersion = sample.EngineVersion
	observation.Health, observation.ReasonCode = sample.Health, sample.ReasonCode
	observation.Capabilities = sample.Capabilities
	if sample.Health == "healthy" && r.engine.egress != nil && r.engine.egress.udpVerified.Load() {
		observation.Capabilities = append(observation.Capabilities, "guarded_udp_v1")
	}
	observation.UploadTotal, observation.DownloadTotal = sample.UploadTotal, sample.DownloadTotal
	observation.ActiveConnections = sample.ActiveConnections
	if sample.Health == "healthy" {
		observation.Capabilities = append(observation.Capabilities, "logs", "diagnostics")
		if r.engine.dialect() == domain.EngineMihomo && r.engine.desired.Publication != nil {
			observation.Capabilities = append(observation.Capabilities, "groups", "select", "rules")
			if r.config.ProbeProfilesFile != "" {
				observation.Capabilities = append(observation.Capabilities, "probe", "dns")
			}
		}
	}
	return observation, connections
}
