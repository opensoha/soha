package networkproxyruntime

import (
	"bytes"
	"context"
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
	runner := &Runner{config: config, client: client, ingest: ingest, engine: newEngine(config, secret)}
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
	defer r.engine.stop()
	if err := r.enroll(ctx); err != nil {
		return fmt.Errorf("enroll proxy runtime: %w", err)
	}
	lastWall := time.Now().UnixNano()
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
		r.expire(time.Now())
		cycleCtx, cancel := r.cycleContext(ctx)
		err := r.cycle(cycleCtx)
		cancel()
		r.expire(time.Now())
		if err != nil && ctx.Err() == nil {
			slog.Warn("proxy runtime cycle failed", "error", err)
		}
		timer.Reset(r.nextCycleDelay(time.Now()))
	}
}

func (r *Runner) cycle(ctx context.Context) error {
	if err := r.syncConfiguration(ctx); err != nil {
		return err
	}
	// A newly granted configuration may expire before this first cycle ends.
	ctx, cancel := r.cycleContext(ctx)
	defer cancel()
	r.ensureRunning(ctx)
	observation, connections := r.observe(ctx)
	if err := r.client.Observe(ctx, observation); err != nil {
		return fmt.Errorf("report proxy observation: %w", err)
	}
	if connections != nil {
		if err := r.client.PutConnections(ctx, r.config.Engine, connections); err != nil {
			return fmt.Errorf("report proxy connections: %w", err)
		}
	}
	closeErr := r.executeClose(ctx)
	if slices.Contains(observation.Capabilities, "traffic") {
		if err := r.ingest.Send(ctx, observation); err != nil {
			return errors.Join(closeErr, fmt.Errorf("report proxy traffic: %w", err))
		}
	}
	return closeErr
}

func (r *Runner) cycleContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if r.enabled && !r.validUntil.IsZero() {
		return context.WithDeadline(ctx, r.validUntil)
	}
	return context.WithCancel(ctx)
}

func (r *Runner) nextCycleDelay(now time.Time) time.Duration {
	if r.enabled && !r.validUntil.IsZero() {
		return max(time.Millisecond, min(r.config.PollInterval, r.validUntil.Sub(now)))
	}
	return r.config.PollInterval
}

func (r *Runner) suspend(reason string) {
	r.engine.stop()
	r.enabled, r.suspended, r.reasonCode = false, true, reason
}

func (r *Runner) expire(now time.Time) {
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
	if r.appliedRevision == desired.Revision && !r.suspended {
		if desired.Enabled {
			if err := r.engine.refreshEgress(desired.ValidUntil); err != nil {
				r.suspend("egress_lease_failed")
				return err
			}
		}
		r.validUntil = desired.ValidUntil
	}
	if r.lastAttempt != nil && !r.attemptReported {
		if err := r.client.Applied(ctx, *r.lastAttempt); err != nil {
			return fmt.Errorf("acknowledge proxy configuration: %w", err)
		}
		r.attemptReported = true
	}
	return nil
}

func (r *Runner) applyConfiguration(ctx context.Context, desired domain.Configuration) {
	refreshBootstrap := desired.Enabled && time.Since(r.engine.bootstrapAt) >= 5*time.Minute
	if desired.Revision != r.appliedRevision || r.suspended || refreshBootstrap {
		if r.lastAttempt == nil || r.lastAttempt.Revision != desired.Revision || r.suspended || refreshBootstrap {
			applyCtx, cancel := context.WithDeadline(ctx, desired.ValidUntil)
			applied := r.engine.apply(applyCtx, desired)
			cancel()
			r.lastAttempt, r.attemptReported = &applied, false
			if applied.Status == "applied" {
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
	if r.config.Engine == domain.EngineV2Ray || !r.enabled {
		return nil
	}
	command, err := r.client.NextClose(ctx)
	if err != nil {
		return fmt.Errorf("poll proxy close command: %w", err)
	}
	if command == nil || !command.ExpiresAt.After(time.Now()) {
		return nil
	}
	command.Status, err = r.engine.close(ctx, command.ConnectionID)
	if err != nil {
		command.Status, command.ReasonCode = "failed", "controller_unavailable"
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
	observation.UptimeSeconds = int64(time.Since(r.engine.startedAt).Seconds())
	if r.engine.controller != nil {
		version, err := r.engine.controller.version(ctx)
		if err != nil {
			observation.Health, observation.ReasonCode = "degraded", "controller_unavailable"
			return observation, nil
		}
		observation.EngineVersion = version
		upload, download, active, connections, err := r.engine.controller.snapshot(ctx)
		if err != nil {
			observation.Health, observation.ReasonCode = "degraded", "traffic_unavailable"
			return observation, nil
		}
		observation.Health = "healthy"
		observation.Capabilities = []string{"traffic", "connections", "close_connection", "protected_destinations_v1", "guarded_dns_tcp_v1"}
		if r.config.Engine == domain.EngineMihomo {
			observation.Capabilities = append(observation.Capabilities, "protection_rules_readback")
		}
		observation.UploadTotal, observation.DownloadTotal = upload, download
		observation.ActiveConnections = &active
		return observation, connections
	}
	version, upload, download, err := r.engine.v2rayStats(ctx)
	if err != nil {
		observation.Health, observation.ReasonCode = "degraded", "stats_unavailable"
		return observation, nil
	}
	observation.EngineVersion = version
	observation.Health = "healthy"
	observation.Capabilities = []string{"traffic", "protected_destinations_v1", "guarded_dns_tcp_v1"}
	observation.UploadTotal, observation.DownloadTotal = upload, download
	return observation, nil
}
