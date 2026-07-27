package saml

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"crypto/sha256"
	"encoding/hex"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type LoginRuntime struct {
	client *http.Client
	now    func() time.Time
}

func NewLoginRuntime() *LoginRuntime {
	return &LoginRuntime{client: secureMetadataClient(), now: time.Now}
}

func (r *LoginRuntime) PinMetadata(ctx context.Context, provider domainsettings.LoginProviderSettings) (domainsettings.LoginProviderSettings, error) {
	input := sohaapi.SAMLMetadataInput{Source: sohaapi.URL, URL: provider.MetadataURL}
	if strings.TrimSpace(provider.MetadataXML) != "" {
		input = sohaapi.SAMLMetadataInput{Source: sohaapi.XML, XML: provider.MetadataXML}
	}
	validation, raw, err := r.ValidateMetadata(ctx, input)
	if err != nil {
		return provider, err
	}
	if validation.EntityID == "" {
		return provider, errors.New("SAML metadata entity ID is required")
	}
	provider.MetadataXML = raw
	return provider, nil
}

func (r *LoginRuntime) ValidateMetadata(ctx context.Context, input sohaapi.SAMLMetadataInput) (sohaapi.SAMLMetadataValidation, string, error) {
	var raw []byte
	var metadata *Metadata
	var err error
	switch input.Source {
	case sohaapi.URL:
		raw, metadata, err = r.fetchMetadataDocument(ctx, input.URL)
	case sohaapi.XML:
		raw = []byte(input.XML)
		metadata, err = ParseMetadata(raw, r.now())
	default:
		return sohaapi.SAMLMetadataValidation{}, "", errors.New("SAML metadata source must be url or xml")
	}
	if err != nil {
		return sohaapi.SAMLMetadataValidation{}, "", err
	}
	result := sohaapi.SAMLMetadataValidation{Valid: true, EntityID: metadata.EntityID}
	for _, value := range []string{metadata.RedirectSSOURL, metadata.POSTSSOURL} {
		if value != "" && !slices.Contains(result.SingleSignOnUrls, value) {
			result.SingleSignOnUrls = append(result.SingleSignOnUrls, value)
		}
	}
	for _, certificate := range metadata.SigningCertificates {
		fingerprint := sha256.Sum256(certificate.Raw)
		result.Certificates = append(result.Certificates, sohaapi.CertificateSummary{
			ID: hex.EncodeToString(fingerprint[:8]), FingerprintSHA256: hex.EncodeToString(fingerprint[:]),
			Subject: certificate.Subject.String(), Issuer: certificate.Issuer.String(),
			NotBefore: certificate.NotBefore, NotAfter: certificate.NotAfter, Status: sohaapi.CertificateSummaryStatusActive,
		})
	}
	return result, string(raw), nil
}

func (r *LoginRuntime) Begin(ctx context.Context, provider domainsettings.LoginProviderSettings, relayState string) (appidentity.SAMLAuthnRequest, error) {
	serviceProvider, err := r.serviceProvider(ctx, provider)
	if err != nil {
		return appidentity.SAMLAuthnRequest{}, err
	}
	request, err := serviceProvider.BuildAuthnRequest(BindingRedirect, relayState)
	if err != nil {
		return appidentity.SAMLAuthnRequest{}, err
	}
	return appidentity.SAMLAuthnRequest{ID: request.ID, RedirectURL: request.RedirectURL}, nil
}

func (r *LoginRuntime) Validate(ctx context.Context, provider domainsettings.LoginProviderSettings, encodedResponse, requestID string) (appidentity.SAMLAssertion, error) {
	serviceProvider, err := r.serviceProvider(ctx, provider)
	if err != nil {
		return appidentity.SAMLAssertion{}, err
	}
	assertion, err := serviceProvider.ValidateResponse(encodedResponse, requestID)
	if err != nil {
		return appidentity.SAMLAssertion{}, err
	}
	return appidentity.SAMLAssertion{ID: assertion.ID, Subject: assertion.Subject, Attributes: assertion.Attributes}, nil
}

func (r *LoginRuntime) Metadata(ctx context.Context, provider domainsettings.LoginProviderSettings) ([]byte, error) {
	_ = ctx
	return BuildServiceProviderMetadata(
		provider.EntityID,
		provider.RedirectURL,
		provider.RedirectURL,
	)
}

func (r *LoginRuntime) serviceProvider(ctx context.Context, provider domainsettings.LoginProviderSettings) (*ServiceProvider, error) {
	entityID := strings.TrimSpace(provider.EntityID)
	if entityID == "" {
		return nil, errors.New("SAML SP entity ID is required")
	}
	if strings.TrimSpace(provider.MetadataXML) == "" {
		return nil, errors.New("pinned SAML metadata is required; save or import the login source first")
	}
	metadata, err := ParseMetadata([]byte(provider.MetadataXML), r.now())
	if err != nil {
		return nil, fmt.Errorf("parse pinned SAML metadata: %w", err)
	}
	return NewServiceProvider(ServiceProviderConfig{
		EntityID: entityID, MetadataURL: provider.RedirectURL,
		ACSURL: provider.RedirectURL, IDPMetadata: metadata, Now: r.now,
	})
}

func (r *LoginRuntime) fetchMetadata(ctx context.Context, rawURL string) (*Metadata, error) {
	_, metadata, err := r.fetchMetadataDocument(ctx, rawURL)
	return metadata, err
}

func (r *LoginRuntime) fetchMetadataDocument(ctx context.Context, rawURL string) ([]byte, *Metadata, error) {
	metadataURL, err := safeMetadataURL(rawURL)
	if err != nil {
		return nil, nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL.String(), nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build metadata request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch SAML metadata: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("fetch SAML metadata: unexpected status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maxXMLBytes+1))
	if err != nil {
		return nil, nil, fmt.Errorf("read SAML metadata: %w", err)
	}
	metadata, err := ParseMetadata(data, r.now())
	if err != nil {
		return nil, nil, err
	}
	return data, metadata, nil
}

func secureMetadataClient() *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			addresses, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}
			for _, address := range addresses {
				if publicIP(address) {
					return dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
				}
			}
			return nil, errors.New("SAML metadata host does not resolve to a public address")
		},
		TLSHandshakeTimeout: 5 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(request *http.Request, _ []*http.Request) error {
			_, err := safeMetadataURL(request.URL.String())
			return err
		},
	}
}

func safeMetadataURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return nil, errors.New("SAML metadata URL must be an absolute HTTPS URL without credentials")
	}
	if ip := net.ParseIP(parsed.Hostname()); ip != nil && !publicIP(ip) {
		return nil, errors.New("SAML metadata URL cannot target a private or local address")
	}
	return parsed, nil
}

func validateMetadataURL(value string) error {
	_, err := safeMetadataURL(value)
	return err
}

func publicIP(address net.IP) bool {
	return address.IsGlobalUnicast() && !address.IsPrivate() && !address.IsLoopback() && !address.IsLinkLocalUnicast() && !address.IsLinkLocalMulticast()
}
