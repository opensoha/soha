package cluster

import domaincluster "github.com/opensoha/soha/internal/domain/cluster"

// The repository is authoritative once configured. Reconcile removed and Agent
// connections too, so an idle subscription on another replica cannot retain a
// deleted direct client indefinitely. A failed inventory closes local watches;
// the next successful cycle restores only current persisted registrations.
func (s *Service) reconcileDirectRegistrations(connections []domaincluster.Connection) {
	direct := make(map[string]bool, len(connections))
	for _, connection := range connections {
		mode := connection.Summary.ConnectionMode
		if mode == "" || mode == domaincluster.ConnectionModeDirectKubeconfig {
			direct[connection.Summary.ID] = true
		}
	}
	for _, id := range s.manager.ClusterIDs() {
		if !direct[id] {
			s.retireDirectRegistration(id)
		}
	}
}

func (s *Service) retireDirectRegistration(id string) {
	if s.cache != nil {
		s.cache.UnregisterCluster(id)
	}
	s.registry.UnregisterCluster(id)
}
