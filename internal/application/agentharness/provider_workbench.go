package agentharness

import "time"

// The observer receives safe readiness alongside the installed plugin catalog.
// Readiness is not persisted in the catalog or included in its digest.
func (s *ProviderControlPlane) publishWorkbenchCatalogLocked() {
	if s.observer == nil {
		return
	}
	catalog := cloneProviderCatalog(s.catalog)
	catalog.RuntimeStatuses = make(map[string]RunnerProviderStatus, len(catalog.Providers))
	now := s.now().UTC()
	for _, provider := range catalog.Providers {
		status := RunnerProviderStatus{ProviderID: provider.ID, ProviderVersion: provider.ProviderVersion, CatalogRevision: catalog.Revision, Health: "unknown", Draining: provider.Draining}
		for _, ack := range s.acks {
			if !ack.Accepted || !ack.Targeted || ack.ActiveRevision != catalog.Revision || ack.ObservedAt.After(now.Add(time.Minute)) || now.Sub(ack.ObservedAt) > 2*time.Minute {
				continue
			}
			for _, candidate := range ack.ProviderStatuses {
				if candidate.ProviderID != provider.ID || candidate.ProviderVersion != provider.ProviderVersion || candidate.CatalogRevision != catalog.Revision || candidate.Health != "healthy" || candidate.Draining {
					continue
				}
				if status.ObservedAt.Before(ack.ObservedAt) {
					status = candidate
					status.ObservedAt = ack.ObservedAt
					status.Draining = provider.Draining
				}
			}
		}
		catalog.RuntimeStatuses[provider.ID] = status
	}
	s.observer.ApplyProviderCatalog(catalog)
}
