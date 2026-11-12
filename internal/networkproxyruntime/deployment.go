package networkproxyruntime

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

type deploymentState struct {
	grant      *api.NetworkProxyDeploymentRuntimeControl
	floor      int64
	drained    bool
	drainLease time.Time
}

func (d deploymentState) mayApply(now time.Time) bool {
	return d.grant != nil && !d.grant.StopRequired && d.grant.AcceptingRequests && d.grant.ExpiresAt.After(now)
}

func (r *Runner) syncDeployment(ctx context.Context) error {
	if !r.config.ManagedDeployment {
		return nil
	}
	var grant api.NetworkProxyDeploymentRuntimeControl
	path := "/api/network-control/v1/proxy-runtimes/" + url.PathEscape(r.config.RuntimeID) + "/deployment/control?instanceId=" + url.QueryEscape(r.config.InstanceID) + "&processEpoch=" + url.QueryEscape(r.hostBootEpoch)
	if err := r.client.typed(ctx, http.MethodGet, path, nil, &grant); err != nil {
		r.suspend("deployment_control_unavailable")
		return err
	}
	if err := r.acceptDeployment(grant, time.Now()); err != nil {
		r.suspend("deployment_control_invalid")
		return err
	}
	if !grant.AcceptingRequests {
		r.health.invalidate()
	}
	if r.ingress != nil {
		r.ingress.setAccepting(grant.AcceptingRequests)
		if !grant.AcceptingRequests {
			r.ingress.mu.Lock()
			r.ingress.until = r.ingress.capCertificate(r.deployment.flowUntil())
			r.ingress.mu.Unlock()
			r.ingress.capPacketLeases()
		}
	}
	if r.engine.egress != nil {
		r.engine.egress.accepting.Store(grant.AcceptingRequests && r.enabled && !r.suspended && r.attemptReported)
	}
	return nil
}

func (r *Runner) acceptDeployment(grant api.NetworkProxyDeploymentRuntimeControl, now time.Time) error {
	if err := r.validateDeploymentGrant(grant, now); err != nil {
		return err
	}
	if old := r.deployment.grant; old != nil {
		if grant.StopRequired && old.StopRequired && grant.ControlRevision == old.ControlRevision && !grant.ExpiresAt.Equal(old.ExpiresAt) {
			return errors.New("stop confirmation deadline changed")
		}
		if grant.OwnerEpoch < old.OwnerEpoch || grant.CredentialEpoch < old.CredentialEpoch || (grant.ControlRevision == old.ControlRevision && (grant.AcceptingRequests != old.AcceptingRequests || grant.StopRequired != old.StopRequired)) {
			return errors.New("deployment control regressed")
		}
	}
	if !grant.AcceptingRequests {
		if !r.deployment.drainLease.IsZero() {
			r.deployment.drainLease = minTime(r.deployment.drainLease, grant.ExpiresAt)
		} else {
			r.deployment.drainLease = grant.ExpiresAt
			if !r.validUntil.IsZero() {
				r.deployment.drainLease = minTime(r.deployment.drainLease, r.validUntil)
			}
		}
	}
	r.deployment.grant, r.deployment.floor = &grant, grant.ControlRevision
	if grant.AcceptingRequests {
		r.deployment.drained = false
		r.deployment.drainLease = time.Time{}
	}
	return nil
}

func (r *Runner) ackDeployment(ctx context.Context, sample domain.Observation) error {
	if !r.config.ManagedDeployment || r.deployment.grant == nil {
		return nil
	}
	g := r.deployment.grant
	known, active := r.deploymentActiveCount(sample)
	accepting := g.AcceptingRequests && g.ExpiresAt.After(time.Now()) && r.enabled && !r.suspended && sample.Health == "healthy"
	ack := api.NetworkProxyDeploymentRuntimeAck{RuntimeID: g.RuntimeID, InstanceID: g.InstanceID, ProcessEpoch: r.hostBootEpoch, CredentialID: g.CredentialID, CredentialEpoch: g.CredentialEpoch, OwnerEpoch: g.OwnerEpoch, ControlRevision: g.ControlRevision, ExpiresAt: g.ExpiresAt, ObservedAt: time.Now().UTC(), AcceptingRequests: accepting, ActiveCountKnown: known, ActiveConnections: active}
	if err := r.client.typed(ctx, http.MethodPost, "/api/network-control/v1/proxy-runtimes/"+url.PathEscape(r.config.RuntimeID)+"/deployment/control/receipt", ack, nil); err != nil {
		return err
	}
	if !g.AcceptingRequests && known && active == 0 && !r.deployment.drained {
		r.suspend("deployment_drained")
		r.deployment.drained = true
	}
	return nil
}

func (d deploymentState) flowUntil() time.Time {
	if d.grant == nil {
		return time.Time{}
	}
	until := d.grant.ExpiresAt
	if !d.drainLease.IsZero() {
		until = minTime(until, d.drainLease)
	}
	return until
}

func (r *Runner) validateDeploymentGrant(grant api.NetworkProxyDeploymentRuntimeControl, now time.Time) error {
	if grant.StopRequired && (grant.AcceptingRequests || grant.ExpiresAt.Sub(grant.IssuedAt) > 30*time.Second) {
		return errors.New("invalid stop confirmation grant")
	}
	if grant.RuntimeID != r.config.RuntimeID || grant.InstanceID != r.config.InstanceID || grant.ProcessEpoch != r.hostBootEpoch || grant.CredentialID == "" || grant.CredentialEpoch < 1 || grant.OwnerEpoch < 1 || grant.ControlRevision < 1 || grant.ControlRevision < r.deployment.floor || grant.IssuedAt.After(now) || !grant.ExpiresAt.After(now) || grant.ExpiresAt.After(now.Add(5*time.Minute)) {
		return errors.New("invalid deployment grant")
	}
	return nil
}

func (r *Runner) deploymentActiveCount(sample domain.Observation) (bool, int64) {
	known := sample.ActiveConnections != nil && sample.Health == "healthy"
	var active int64
	if known {
		active = int64(*sample.ActiveConnections)
	}
	if r.ingress != nil {
		known, active = true, r.ingress.active.Load()
	}
	if r.engine != nil && r.engine.egress != nil && r.engine.egress.udpOwned != nil {
		n := r.engine.egress.udpOwned.activeCount()
		if r.ingress != nil {
			active += int64(n)
		} else if n > 0 {
			known = false
		}
	}
	if r.deployment.drained {
		known, active = true, 0
	}
	return known, active
}
