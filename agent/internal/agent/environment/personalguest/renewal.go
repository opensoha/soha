package personalguest

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-agent/internal/agent/config"
	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func (c *Client) renewalDue() bool {
	transport, ok := c.http.Transport.(*http.Transport)
	if !ok || len(transport.TLSClientConfig.Certificates) != 1 {
		return false
	}
	leaf, err := x509.ParseCertificate(transport.TLSClientConfig.Certificates[0].Certificate[0])
	return err == nil && !time.Now().Before(leaf.NotAfter.Add(-leaf.NotAfter.Sub(leaf.NotBefore)/2))
}

func (c *Client) renew(ctx context.Context) error {
	if c.state.Binding == nil {
		return errors.New("renewal requires a durable registered binding")
	}
	if c.state.RenewalResult != nil && !time.Now().Before(c.state.RenewalResult.CommitBefore) {
		return c.confirmExpiredRenewal(ctx)
	}
	if c.state.Renewal == nil {
		if err := c.prepareRenewal(); err != nil {
			return err
		}
	}
	if c.state.RenewalResult == nil {
		ready, err := c.receiveRenewal(ctx)
		if err != nil || !ready {
			return err
		}
	}
	result := c.state.RenewalResult
	client, err := guestHTTPClientWithCertificate(c.cfg, result.CertificatePEM)
	if err != nil {
		return err
	}
	c.http.CloseIdleConnections()
	c.http = client
	status, raw, err := c.call(ctx, http.MethodPost, "/renewals/"+result.RequestID+"/commit", nil, "")
	if err != nil {
		return err
	}
	if status == http.StatusGone {
		return c.resetExpiredRenewal(ctx)
	}
	var binding guest.Binding
	if status != http.StatusOK || json.Unmarshal(raw, &binding) != nil || binding != result.Binding {
		return errors.New("personal guest renewal commit is unconfirmed")
	}
	c.state.Binding, c.state.ActiveCertificatePEM = &binding, result.CertificatePEM
	c.state.Renewal, c.state.RenewalResult = nil, nil
	return c.store.save(c.state)
}

func (c *Client) prepareRenewal() error {
	raw, err := config.ReadPrivateFile(c.cfg.KeyFile, 8192)
	if err != nil {
		return err
	}
	block, rest := pem.Decode(raw)
	if block == nil || len(rest) != 0 {
		return errors.New("invalid personal guest private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return errors.New("invalid personal guest signing key")
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return errors.New("personal guest key cannot sign a renewal")
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, signer)
	if err != nil {
		return err
	}
	c.state.Renewal = &guest.Renewal{SchemaVersion: guest.RenewalVersion, RequestID: uuid.NewString(), BindingVersion: c.state.Binding.Version, CSRPEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr}))}
	return c.store.save(c.state)
}

func (c *Client) resetExpiredRenewal(ctx context.Context) error {
	old, err := guestHTTPClientWithCertificate(c.cfg, c.state.ActiveCertificatePEM)
	if err != nil {
		return err
	}
	c.http.CloseIdleConnections()
	c.http = old
	status, raw, err := c.call(ctx, http.MethodGet, "", nil, "")
	var binding guest.Binding
	if err != nil {
		return err
	}
	if status != http.StatusOK || json.Unmarshal(raw, &binding) != nil || binding != *c.state.Binding {
		return errors.New("expired renewal cannot recover current authority")
	}
	c.state.Renewal, c.state.RenewalResult = nil, nil
	return c.store.save(c.state)
}

func (c *Client) confirmExpiredRenewal(ctx context.Context) error {
	old, err := guestHTTPClientWithCertificate(c.cfg, c.state.ActiveCertificatePEM)
	if err != nil {
		return err
	}
	c.http.CloseIdleConnections()
	c.http = old
	status, _, err := c.call(ctx, http.MethodPost, "/renewals", c.state.Renewal, "")
	if err != nil {
		return err
	}
	if status != http.StatusGone {
		return errors.New("pending guest renewal expiry is unconfirmed")
	}
	return c.resetExpiredRenewal(ctx)
}

func (c *Client) receiveRenewal(ctx context.Context) (bool, error) {
	status, raw, err := c.call(ctx, http.MethodPost, "/renewals", c.state.Renewal, "")
	if err != nil {
		return false, err
	}
	if status == http.StatusGone {
		return false, c.resetExpiredRenewal(ctx)
	}
	var result guest.RenewalResult
	if status != http.StatusOK || json.Unmarshal(raw, &result) != nil {
		return false, errors.New("personal guest renewal was denied")
	}
	old := c.state.Binding
	ca, err := config.ReadPrivateFile(c.cfg.CAFile, 8192)
	if err != nil {
		return false, err
	}
	if result.RequestID != c.state.Renewal.RequestID || result.Binding.ID != old.ID || result.Binding.Version != old.Version+1 || !old.SameWorkspace(result.Binding) || !result.CommitBefore.After(result.Binding.IssuedAt) || result.CommitBefore.Sub(result.Binding.IssuedAt) > 5*time.Minute || strings.TrimSpace(result.ClientCAPEM) != strings.TrimSpace(string(ca)) {
		return false, errors.New("personal guest renewal changed workspace or trust authority")
	}
	client, err := guestHTTPClientWithCertificate(c.cfg, result.CertificatePEM)
	if err != nil {
		return false, err
	}
	client.CloseIdleConnections()
	c.state.RenewalResult = &result
	if err := c.store.save(c.state); err != nil {
		return false, err
	}
	return true, nil
}
