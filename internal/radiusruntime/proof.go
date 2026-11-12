package radiusruntime

func (s *Supervisor) configureHook(hook *HTTP) error {
	return hook.withManaged(s.gate.RLocker(), func() (string, error) {
		if s.leaseState.RuntimeProof == "" {
			return "", ErrUnknown
		}
		return s.leaseState.RuntimeProof, nil
	}, func(request Request) error {
		if request.NASID != s.leaseState.NASID || !s.leaseState.AcceptNew && request.State == "" {
			return ErrDenied
		}
		return nil
	}, func() { s.mu.Lock(); s.observedNASUse = true; s.mu.Unlock() })
}
