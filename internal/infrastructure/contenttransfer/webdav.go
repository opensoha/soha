package contenttransfer

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	app "github.com/opensoha/soha/internal/application/contenttransfer"
	d "github.com/opensoha/soha/internal/domain/contenttransfer"
	"github.com/opensoha/soha/internal/domain/identity"
	secret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
)

type webdavBackend struct {
	destination, identity, snapshot, base, username, password string
	client                                                    *http.Client
}

func (f *Factory) webdav(ctx context.Context, p identity.Principal, dest d.Destination) (app.Backend, error) {
	c := *dest.Published
	username, password := "", ""
	credentialVersion := "anonymous"
	if c.SecretRef != "" {
		target := secret.Target{Type: "connection", Ref: dest.ID}
		refs, err := f.Secrets.PinReferences(ctx, p, map[string]string{"TRANSFER": c.SecretRef}, target)
		if err != nil {
			return nil, err
		}
		values, err := f.Secrets.ResolvePinnedReferences(ctx, p, refs, target)
		if err != nil {
			return nil, err
		}
		credentialVersion = fmt.Sprint(refs)
		value := values["TRANSFER"]
		delete(values, "TRANSFER")
		var credential struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		decoder := json.NewDecoder(strings.NewReader(value))
		decoder.DisallowUnknownFields()
		if len(value) > 8192 || decoder.Decode(&credential) != nil || decoder.Decode(new(any)) != io.EOF || credential.Username == "" || strings.ContainsAny(credential.Username, ":\r\n\x00") || len(credential.Username) > 256 || credential.Password == "" || len(credential.Password) > 4096 {
			return nil, apperrors.ErrInvalidArgument
		}
		username, password = credential.Username, credential.Password
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, apperrors.ErrInvalidArgument
	}
	// DNS is resolved at dialing and every address is checked before using a
	// numeric socket target. TLS still verifies the original approved hostname.
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DisableCompression: true, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || !strings.EqualFold(host, u.Hostname()) {
			return nil, apperrors.ErrAccessDenied
		}
		return dialApproved(ctx, network, host, port, c.PrivateCIDRs)
	}
	client := &http.Client{Transport: transport, Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &webdavBackend{destination: dest.ID, identity: backendIdentity("webdav", u.String(), username), snapshot: backendIdentity("webdav", u.String(), username, credentialVersion), base: u.String(), username: username, password: password, client: client}, nil
}
func dialApproved(ctx context.Context, network, host, port string, private []string) (net.Conn, error) {
	addresses, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 || len(addresses) > 64 {
		return nil, apperrors.ErrServiceUnavailable
	}
	for _, address := range addresses {
		ip, ok := netip.AddrFromSlice(address.IP)
		if !ok {
			return nil, apperrors.ErrAccessDenied
		}
		ip = ip.Unmap()
		if !approvedIP(ip, private) {
			return nil, apperrors.ErrAccessDenied
		}
	}
	var last error
	for _, address := range addresses {
		dialer := net.Dialer{Timeout: 5 * time.Second}
		connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.IP.String(), port))
		if err == nil {
			return connection, nil
		}
		last = err
	}
	return nil, last
}
func approvedIP(ip netip.Addr, private []string) bool {
	if !netguard.BlockedOutboundIP(net.IP(ip.AsSlice())) {
		return true
	}
	if !ip.IsPrivate() {
		return false
	}
	for _, raw := range private {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Contains(ip) {
			return true
		}
	}
	return false
}
func (b *webdavBackend) Identity() string         { return b.identity }
func (b *webdavBackend) SnapshotIdentity() string { return b.snapshot }
func (b *webdavBackend) request(ctx context.Context, method, key string, content []byte) (*http.Response, error) {
	if !validObjectKey(key, b.destination) {
		return nil, apperrors.ErrInvalidArgument
	}
	parts := strings.Split(key, "/")
	target := b.base + parts[3] + ".bin"
	request, err := http.NewRequestWithContext(ctx, method, target, bytes.NewReader(content))
	if err != nil {
		return nil, apperrors.ErrInvalidArgument
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-store")
	if b.username != "" {
		request.SetBasicAuth(b.username, b.password)
	}
	if method == http.MethodPut {
		request.Header.Set("If-None-Match", "*")
		request.Header.Set("Content-Type", "application/octet-stream")
	}
	response, err := b.client.Do(request)
	if err != nil {
		return nil, apperrors.ErrServiceUnavailable
	}
	if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		_ = response.Body.Close()
		return nil, apperrors.ErrServiceUnavailable
	}
	return response, nil
}
func (b *webdavBackend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	r, err := b.request(ctx, http.MethodGet, key, nil)
	if err != nil {
		return nil, err
	}
	if r.StatusCode != http.StatusOK {
		_ = r.Body.Close()
		return nil, apperrors.ErrServiceUnavailable
	}
	return r.Body, nil
}
func (b *webdavBackend) Put(ctx context.Context, key string, content []byte) error {
	r, err := b.request(ctx, http.MethodPut, key, content)
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode != http.StatusCreated {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}
func (b *webdavBackend) Delete(ctx context.Context, key string) error {
	r, err := b.request(ctx, http.MethodDelete, key, nil)
	if err != nil {
		return err
	}
	defer func() { _ = r.Body.Close() }()
	if r.StatusCode != http.StatusNoContent && r.StatusCode != http.StatusOK && r.StatusCode != http.StatusNotFound {
		return apperrors.ErrServiceUnavailable
	}
	return nil
}
