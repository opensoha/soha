package networkgateway

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprobe"
	"golang.org/x/time/rate"
)

// ProbeServer uses a dedicated server certificate; runtime client keys are never
// repurposed as a public listener identity.
func (c Config) ProbeServer(r *Runtime) (*http.Server, error) {
	if err := c.validateProbeConfig(); err != nil {
		return nil, err
	}
	if c.ProbeAddress == "" {
		return nil, nil
	}
	config, err := networkidentity.LoadServerTLS(c.ProbeCertFile, c.ProbeKeyFile, c.ProbeCAFile)
	if err != nil {
		return nil, err
	}
	config.ClientAuth = tls.RequireAndVerifyClientCert
	return &http.Server{Addr: c.ProbeAddress, Handler: r.ProbeHandler(), TLSConfig: config,
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 3 * time.Second,
		IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192}, nil
}

func (c Config) validateProbeConfig() error {
	values := []string{c.ProbeAddress, c.ProbeCertFile, c.ProbeKeyFile, c.ProbeCAFile}
	count := 0
	for _, value := range values {
		if value != "" {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	if count != len(values) {
		return fmt.Errorf("VPN probe listener and separate TLS files must be configured together")
	}
	_, port, err := net.SplitHostPort(c.ProbeAddress)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || n < 1 || n > 65535 {
		return fmt.Errorf("VPN probe address must be host:port")
	}
	if c.ProbeKeyFile == c.ControlKeyFile || c.ProbeKeyFile == c.IngestKeyFile {
		return fmt.Errorf("VPN probe server requires its own TLS key")
	}
	return validateGatewayFiles(map[string]string{"probe certificate": c.ProbeCertFile, "probe key": c.ProbeKeyFile, "probe client CA": c.ProbeCAFile})
}

func (r *Runtime) ProbeHandler() http.Handler {
	// A global bound avoids an attacker-controlled per-identity cache.
	limiter := rate.NewLimiter(100, 100)
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if request.Method != http.MethodGet || request.URL.Path != "/vpn/probe" || request.URL.RawQuery != "" || request.ContentLength > 0 {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !limiter.Allow() {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		if request.TLS == nil || len(request.TLS.VerifiedChains) == 0 || len(request.TLS.VerifiedChains[0]) == 0 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		identity, err := networkidentity.ParseCertificate(request.TLS.VerifiedChains[0][0], networkidentity.ScopeNetworkControl)
		if err != nil || identity.Kind != "endpoint" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		r.mu.RLock()
		probe, ready := r.probe, !r.disabled && r.validUntil.After(r.now().UTC())
		r.mu.RUnlock()
		if probe == nil || !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		token, ok := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := networkprobe.Verify(probe.VerificationKey, token, r.runtimeID, identity.ID, r.now().UTC()); err != nil {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
