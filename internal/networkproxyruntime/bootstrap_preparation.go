package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type bootstrapPreparation struct {
	doc         map[string]any
	explicitUDP map[string]bool
	fingerprint [32]byte
	kernel      installedKernel
}

type bootstrapRuntimeIdentity struct {
	Runtime, Instance, Engine          string
	Binary, Adapter, Controller        string
	BinaryHash, ResolvedBinary         string
	ConfigFile, StateDir, KernelSource string
	Managed                            bool
}

// Prepare external material and source pins before allocating any local guard,
// random credential or listener. A changed candidate is consumed exactly once.
func (e *engine) prepareBootstrap(ctx context.Context, desired domain.Configuration) (*bootstrapPreparation, string, error) {
	if err := validateProtection(desired); err != nil {
		return nil, "protection_invalid", err
	}
	raw, err := e.render(desired.Content)
	if err != nil {
		return nil, "render_failed", err
	}
	raw, err = protectConfiguration(e.dialect(), raw, desired.Protection)
	if err != nil {
		return nil, "protection_compile_failed", err
	}
	doc, err := decodeEngineDocument(e.dialect(), raw)
	if err != nil {
		return nil, "bootstrap_guard_failed", err
	}
	p := &bootstrapPreparation{doc: doc, explicitUDP: map[string]bool{}}
	if err := bootstrapConfigurationWithUDP(ctx, e.dialect(), doc, desired.Protection, p.explicitUDP); err != nil {
		return nil, "bootstrap_guard_failed", err
	}
	if e.config.EngineBinary != "" {
		p.kernel, err = inspectInstalledKernel(e.config)
		if err != nil {
			return nil, "bootstrap_guard_failed", err
		}
	}
	p.fingerprint, err = e.bootstrapMaterialFingerprint(desired, p)
	return p, "bootstrap_guard_failed", err
}

func (e *engine) bootstrapMaterialFingerprint(desired domain.Configuration, p *bootstrapPreparation) ([32]byte, error) {
	desired.ValidUntil = time.Time{}
	if desired.Publication != nil {
		pub := *desired.Publication
		pub.ValidUntil = time.Time{}
		desired.Publication = &pub
	}
	var owner, authorization string
	var spaces map[string]string
	if desired.Binding != nil {
		owner = desired.Binding.OwnerUserID
		authorization = desired.Binding.AuthorizationHash
		spaces = desired.Binding.ResourceSpaceIDs
	}
	material := struct {
		Desired                            domain.Configuration
		Document                           map[string]any
		ExplicitUDP                        map[string]bool
		BindingOwner, BindingAuthorization string
		BindingSpaces                      map[string]string
		Identity                           bootstrapRuntimeIdentity
	}{Desired: desired, Document: p.doc, ExplicitUDP: p.explicitUDP, BindingOwner: owner, BindingAuthorization: authorization, BindingSpaces: spaces,
		Identity: bootstrapRuntimeIdentity{Runtime: e.config.RuntimeID, Instance: e.config.InstanceID, Engine: e.config.Engine, Binary: e.config.EngineBinary, BinaryHash: p.kernel.hash, ResolvedBinary: p.kernel.path, Adapter: e.config.AdapterPolicyFile, Controller: e.config.ControllerURL, ConfigFile: e.config.ConfigFile, StateDir: e.config.StateDir, KernelSource: e.config.KernelSourceRef, Managed: e.config.ManagedDeployment}}
	raw, err := json.Marshal(material)
	if err != nil {
		return [32]byte{}, err
	}
	defer clear(raw)
	return sha256.Sum256(raw), nil
}

func (r *Runner) preparePeriodicBootstrap(ctx context.Context, desired domain.Configuration) (*bootstrapPreparation, bool) {
	if !r.enabled || r.suspended {
		return nil, false
	}
	if ctx.Err() != nil || !desired.ValidUntil.After(time.Now()) || !r.validUntil.After(time.Now()) {
		r.suspend("configuration_expired")
		return nil, true
	}
	p, reason, err := r.engine.prepareBootstrap(ctx, desired)
	if err != nil {
		r.lastAttempt = &domain.Applied{Revision: desired.Revision, Status: "rejected", ReasonCode: reason, ReadbackHash: r.engine.fileHash}
		r.attemptReported = false
		r.suspend(reason)
		return nil, true
	}
	if r.engine.bootstrapFingerprint == ([32]byte{}) || p.fingerprint != r.engine.bootstrapFingerprint {
		return p, false
	}
	if err := r.engine.health(ctx); err != nil {
		r.suspend("bootstrap_readback_failed")
		return nil, true
	}
	now := time.Now()
	if ctx.Err() != nil || !desired.ValidUntil.After(now) || !r.validUntil.After(now) {
		r.suspend("configuration_expired")
		return nil, true
	}
	r.engine.bootstrapAt = now.UTC()
	r.recordEvent("bootstrap_refreshed_unchanged")
	return nil, true
}
