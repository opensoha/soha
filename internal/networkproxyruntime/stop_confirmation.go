package networkproxyruntime

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func (r *Runner) stopConfirmationRequired() bool {
	return r.config.ManagedDeployment && r.deployment.grant != nil && r.deployment.grant.StopRequired
}

// A fresh confirmation grant is independent of expired data/config leases. It
// permits only closing owned resources and reporting their observed termination.
func (r *Runner) confirmOwnedStop(ctx context.Context) error {
	g := r.deployment.grant
	if !r.stopConfirmationRequired() || !g.ExpiresAt.After(time.Now()) {
		return errors.New("stop confirmation unavailable")
	}
	r.health.invalidate()
	r.enabled, r.suspended, r.reasonCode = false, true, "deployment_stop_required"
	r.closeIngress()
	stopCtx, cancel := context.WithDeadline(ctx, g.ExpiresAt)
	defer cancel()
	stopErr := errors.Join(r.engine.stopOwned(stopCtx), r.waitOwnedIngressClosed(stopCtx))
	stopped := stopErr == nil
	ack := api.NetworkProxyDeploymentRuntimeAck{RuntimeID: g.RuntimeID, InstanceID: g.InstanceID, ProcessEpoch: r.hostBootEpoch, CredentialID: g.CredentialID, CredentialEpoch: g.CredentialEpoch, OwnerEpoch: g.OwnerEpoch, ControlRevision: g.ControlRevision, ExpiresAt: g.ExpiresAt, ObservedAt: time.Now().UTC(), AcceptingRequests: false, ActiveCountKnown: stopped, ActiveConnections: 0, ProcessStopped: stopped}
	ackErr := r.client.typed(stopCtx, http.MethodPost, "/api/network-control/v1/proxy-runtimes/"+url.PathEscape(r.config.RuntimeID)+"/deployment/control/receipt", ack, nil)
	if stopped {
		r.deployment.drained = true
	}
	return errors.Join(stopErr, ackErr)
}
