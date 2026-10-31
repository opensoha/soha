package api

import "time"

func (s *Server) collectVolumeReaders() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		if err := s.volumeReader.Collect(s.cleanupContext, time.Now().UTC()); err != nil && s.cleanupContext.Err() == nil {
			s.logger.Warn("safe Docker volume reader cleanup could not be confirmed")
		}
		select {
		case <-s.cleanupContext.Done():
			return
		case <-ticker.C:
		}
	}
}
