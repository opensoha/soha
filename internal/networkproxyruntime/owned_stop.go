package networkproxyruntime

import (
	"context"
	"errors"
	"time"

	"github.com/opensoha/soha-contracts/proxyadapter"
)

// Stop only the command handles and listeners created by this host. A timeout
// retains those handles so a later confirmation can observe/reap the same child.
func (e *engine) stopOwned(ctx context.Context) error {
	if e.egress != nil {
		e.egress.accepting.Store(false)
		e.egress.close()
		e.ownedCloseErr = errors.Join(e.ownedCloseErr, e.egress.closeErr)
		e.egress = nil
	}
	return errors.Join(e.ownedCloseErr, e.stopEngineOwned(ctx), e.stopExternalOwned(ctx))
}

func (e *engine) stopEngineOwned(ctx context.Context) error {
	if e.process == nil {
		return nil
	}
	if e.process.Process == nil || e.processFence == nil {
		return errors.New("owned engine identity unavailable")
	}
	if e.wait != nil {
		if err := e.waitOwnedEngine(ctx); err != nil {
			return err
		}
		e.wait = nil
	}
	if e.process.ProcessState == nil {
		return errors.New("owned engine wait proof unavailable")
	}
	if err := awaitOwnedGroup(ctx, e.process); err != nil {
		return err
	}
	e.process, e.processFence = nil, nil
	return nil
}

func (e *engine) waitOwnedEngine(ctx context.Context) error {
	_ = e.processFence.signal(false)
	grace := time.NewTimer(time.Second)
	defer grace.Stop()
	select {
	case <-e.wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-grace.C:
	}
	_ = e.processFence.signal(true)
	select {
	case <-e.wait:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *engine) stopExternalOwned(ctx context.Context) error {
	a, ok := e.adapter.(*externalEngineAdapter)
	if !ok || a.session == nil {
		return nil
	}
	s := a.session
	grace, cancel := context.WithTimeout(ctx, time.Second)
	_, _ = s.call(grace, a.epoch, proxyadapter.Stop, proxyadapter.Payload{})
	cancel()
	s.mu.Lock()
	s.abort()
	s.mu.Unlock()
	select {
	case <-s.done:
		if s.command.ProcessState == nil {
			return errors.New("owned adapter wait proof unavailable")
		}
		if err := awaitOwnedGroup(ctx, s.command); err != nil {
			return err
		}
		a.session = nil
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runner) waitOwnedIngressClosed(ctx context.Context) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		closed, err := r.ownedIngressClosed()
		if err != nil {
			return err
		}
		if closed {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *Runner) ownedIngressClosed() (bool, error) {
	for _, p := range r.retiredIngress {
		p.mu.Lock()
		closed, err := p.closed, p.closeErr
		p.mu.Unlock()
		if err != nil {
			return false, errors.New("owned ingress close result unknown")
		}
		if !closed || p.active.Load() != 0 {
			return false, nil
		}
	}
	return r.ingress == nil, nil
}

func (r *Runner) pruneRetiredIngress() {
	retained := r.retiredIngress[:0]
	for _, p := range r.retiredIngress {
		p.mu.Lock()
		closed, failed := p.closed, p.closeErr != nil
		p.mu.Unlock()
		if !closed || failed || p.active.Load() != 0 {
			retained = append(retained, p)
		}
	}
	r.retiredIngress = retained
}
