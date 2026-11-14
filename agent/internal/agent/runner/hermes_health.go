package runner

type hermesHealthCheck struct {
	Status    string `json:"status"`
	FreeBytes int64  `json:"free_bytes"`
}

type hermesHealth struct {
	Status    string `json:"status"`
	Readiness struct {
		Checks map[string]hermesHealthCheck `json:"checks"`
	} `json:"readiness"`
}

func (h hermesHealth) readyForChat() bool {
	if h.Status == "ok" || h.Status == "healthy" {
		return true
	}
	if h.Status != "degraded" {
		return false
	}
	checks := h.Readiness.Checks
	// Hermes marks >90% usage degraded even with ample capacity. Permit only
	// that warning, with at least 4 GiB free and every functional check healthy.
	if checks["disk"].Status != "degraded" || checks["disk"].FreeBytes < 4<<30 {
		return false
	}
	for _, name := range []string{"state_db", "session_store", "config", "model", "gateway", "background_queues"} {
		if checks[name].Status != "ok" {
			return false
		}
	}
	for name, check := range checks {
		if name != "disk" && check.Status != "ok" {
			return false
		}
	}
	return true
}
