package saml

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	crewjam "github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	dsig "github.com/russellhaering/goxmldsig"
)

const (
	BindingRedirect = crewjam.HTTPRedirectBinding
	BindingPOST     = crewjam.HTTPPostBinding
)

type Metadata struct {
	EntityID            string
	RedirectSSOURL      string
	POSTSSOURL          string
	SigningCertificates []*x509.Certificate
	entity              *crewjam.EntityDescriptor
}

func ParseMetadata(data []byte, now time.Time) (*Metadata, error) {
	if now.IsZero() {
		now = time.Now()
	}
	shape, err := inspectXML(data, false)
	if err != nil {
		return nil, err
	}
	if shape.root.Local != "EntityDescriptor" && shape.root.Local != "EntitiesDescriptor" {
		return nil, fmt.Errorf("%w: expected SAML metadata", ErrInvalidXML)
	}
	entity, err := samlsp.ParseMetadata(data)
	if err != nil {
		return nil, fmt.Errorf("parse SAML metadata: %w", err)
	}
	if strings.TrimSpace(entity.EntityID) == "" || len(entity.IDPSSODescriptors) == 0 {
		return nil, fmt.Errorf("%w: metadata has no IdP descriptor", ErrInvalidXML)
	}

	result := &Metadata{EntityID: entity.EntityID, entity: entity}
	for descriptorIndex := range entity.IDPSSODescriptors {
		descriptor := &entity.IDPSSODescriptors[descriptorIndex]
		if err := collectEndpoints(result, descriptor.SingleSignOnServices); err != nil {
			return nil, err
		}
		if err := collectSigningCertificates(result, descriptor, now); err != nil {
			return nil, err
		}
	}
	if result.RedirectSSOURL == "" && result.POSTSSOURL == "" {
		return nil, fmt.Errorf("%w: metadata has no supported SSO endpoint", ErrInvalidXML)
	}
	if len(result.SigningCertificates) == 0 {
		return nil, fmt.Errorf("%w: metadata has no currently valid signing certificate", ErrInvalidXML)
	}
	return result, nil
}

func collectEndpoints(metadata *Metadata, endpoints []crewjam.Endpoint) error {
	for _, endpoint := range endpoints {
		if _, err := absoluteURL(endpoint.Location); err != nil {
			return fmt.Errorf("%w: invalid SSO endpoint", ErrInvalidXML)
		}
		switch endpoint.Binding {
		case BindingRedirect:
			if metadata.RedirectSSOURL == "" {
				metadata.RedirectSSOURL = endpoint.Location
			}
		case BindingPOST:
			if metadata.POSTSSOURL == "" {
				metadata.POSTSSOURL = endpoint.Location
			}
		}
	}
	return nil
}

func collectSigningCertificates(metadata *Metadata, descriptor *crewjam.IDPSSODescriptor, now time.Time) error {
	validKeys := descriptor.KeyDescriptors[:0]
	for _, key := range descriptor.KeyDescriptors {
		if key.Use != "" && key.Use != "signing" {
			validKeys = append(validKeys, key)
			continue
		}
		validValues, certificates, err := currentCertificates(key.KeyInfo.X509Data.X509Certificates, now)
		if err != nil {
			return err
		}
		metadata.SigningCertificates = append(metadata.SigningCertificates, certificates...)
		if len(validValues) > 0 {
			key.KeyInfo.X509Data.X509Certificates = validValues
			validKeys = append(validKeys, key)
		}
	}
	descriptor.KeyDescriptors = validKeys
	return nil
}

func currentCertificates(values []crewjam.X509Certificate, now time.Time) ([]crewjam.X509Certificate, []*x509.Certificate, error) {
	validValues := values[:0]
	certificates := make([]*x509.Certificate, 0, len(values))
	for _, value := range values {
		certificate, err := parseCertificate(value.Data)
		if err != nil {
			return nil, nil, err
		}
		if !now.Before(certificate.NotBefore) && now.Before(certificate.NotAfter) {
			validValues = append(validValues, value)
			certificates = append(certificates, certificate)
		}
	}
	return validValues, certificates, nil
}

type ServiceProviderConfig struct {
	EntityID           string
	MetadataURL        string
	ACSURL             string
	SigningKey         crypto.Signer
	SigningCertificate *x509.Certificate
	IDPMetadata        *Metadata
	ClockSkew          time.Duration
	Now                func() time.Time
}

type ServiceProvider struct {
	provider  *crewjam.ServiceProvider
	metadata  *Metadata
	clockSkew time.Duration
	now       func() time.Time
}

func NewServiceProvider(config ServiceProviderConfig) (*ServiceProvider, error) {
	if strings.TrimSpace(config.EntityID) == "" || config.IDPMetadata == nil || config.IDPMetadata.entity == nil {
		return nil, errors.New("SAML SP entity ID and IdP metadata are required")
	}
	metadataURL, err := absoluteURL(config.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("metadata URL: %w", err)
	}
	acsURL, err := absoluteURL(config.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("ACS URL: %w", err)
	}
	if err := validateKeyPair(config.SigningKey, config.SigningCertificate); err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	if config.SigningCertificate != nil {
		if err := validCertificates([]*x509.Certificate{config.SigningCertificate}, now()); err != nil {
			return nil, err
		}
	}
	clockSkew := config.ClockSkew
	if clockSkew <= 0 {
		clockSkew = 2 * time.Minute
	}
	if clockSkew > 5*time.Minute {
		return nil, errors.New("SAML clock skew cannot exceed 5 minutes")
	}
	provider := &crewjam.ServiceProvider{
		EntityID:          config.EntityID,
		MetadataURL:       *metadataURL,
		AcsURL:            *acsURL,
		IDPMetadata:       config.IDPMetadata.entity,
		Key:               config.SigningKey,
		Certificate:       config.SigningCertificate,
		AuthnNameIDFormat: crewjam.UnspecifiedNameIDFormat,
	}
	if config.SigningKey != nil {
		provider.SignatureMethod = signatureMethod(config.SigningKey)
	}
	return &ServiceProvider{provider: provider, metadata: config.IDPMetadata, clockSkew: clockSkew, now: now}, nil
}

type AuthnRequest struct {
	ID          string
	Binding     string
	RedirectURL string
	POSTForm    []byte
}

func (sp *ServiceProvider) BuildAuthnRequest(binding, relayState string) (AuthnRequest, error) {
	location := sp.provider.GetSSOBindingLocation(binding)
	if location == "" {
		return AuthnRequest{}, fmt.Errorf("IdP metadata does not advertise binding %q", binding)
	}
	request, err := sp.provider.MakeAuthenticationRequest(location, binding, BindingPOST)
	if err != nil {
		return AuthnRequest{}, fmt.Errorf("build AuthnRequest: %w", err)
	}
	result := AuthnRequest{ID: request.ID, Binding: binding}
	switch binding {
	case BindingRedirect:
		redirect, redirectErr := request.Redirect(url.QueryEscape(relayState), sp.provider)
		if redirectErr != nil {
			return AuthnRequest{}, fmt.Errorf("build Redirect AuthnRequest: %w", redirectErr)
		}
		result.RedirectURL = redirect.String()
	case BindingPOST:
		result.POSTForm = request.Post(relayState)
	default:
		return AuthnRequest{}, fmt.Errorf("unsupported SAML binding %q", binding)
	}
	return result, nil
}

func (sp *ServiceProvider) MetadataXML() ([]byte, error) {
	return xml.MarshalIndent(sp.provider.Metadata(), "", "  ")
}

func BuildServiceProviderMetadata(entityID, metadataURL, acsURL string) ([]byte, error) {
	if strings.TrimSpace(entityID) == "" {
		return nil, errors.New("SAML SP entity ID is required")
	}
	parsedMetadataURL, err := absoluteURL(metadataURL)
	if err != nil {
		return nil, fmt.Errorf("metadata URL: %w", err)
	}
	parsedACSURL, err := absoluteURL(acsURL)
	if err != nil {
		return nil, fmt.Errorf("ACS URL: %w", err)
	}
	provider := &crewjam.ServiceProvider{
		EntityID: entityID, MetadataURL: *parsedMetadataURL, AcsURL: *parsedACSURL,
		AuthnNameIDFormat: crewjam.UnspecifiedNameIDFormat,
	}
	return xml.MarshalIndent(provider.Metadata(), "", "  ")
}

type Assertion struct {
	ID           string
	ResponseID   string
	InResponseTo string
	Issuer       string
	Subject      string
	SessionIndex string
	Attributes   map[string][]string
}

func (sp *ServiceProvider) ValidateResponse(encodedResponse, requestID string) (*Assertion, error) {
	if len(encodedResponse) > base64.StdEncoding.EncodedLen(maxXMLBytes) {
		return nil, fmt.Errorf("%w: encoded response too large", ErrInvalidXML)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(encodedResponse)
	if err != nil {
		return nil, fmt.Errorf("decode SAML response: %w", ErrInvalidXML)
	}
	shape, err := inspectXML(raw, true)
	if err != nil {
		return nil, err
	}
	if shape.root.Space != protocolNamespace || shape.root.Local != "Response" || shape.rootID == "" || shape.assertions != 1 || shape.signatures == 0 || shape.signatures > 2 {
		return nil, fmt.Errorf("%w: response must contain one assertion and a direct signature", ErrInvalidXML)
	}
	if requestID == "" || shape.inResponse != requestID {
		return nil, fmt.Errorf("%w: response correlation mismatch", ErrInvalidXML)
	}
	now := sp.now()
	if err := validCertificates(sp.metadata.SigningCertificates, now); err != nil {
		return nil, err
	}
	verified, err := sp.provider.ParseXMLResponse(raw, []string{requestID}, sp.provider.AcsURL)
	if err != nil {
		return nil, fmt.Errorf("validate SAML response: %w", err)
	}
	if err := validateAssertionTime(verified, now, sp.clockSkew); err != nil {
		return nil, err
	}
	return mapAssertion(verified, shape), nil
}

func validateAssertionTime(assertion *crewjam.Assertion, now time.Time, skew time.Duration) error {
	if assertion.Conditions == nil || assertion.Subject == nil || assertion.Subject.NameID == nil {
		return fmt.Errorf("%w: assertion is missing required conditions or subject", ErrInvalidXML)
	}
	if assertion.Conditions.NotBefore.Add(-skew).After(now) || !now.Before(assertion.Conditions.NotOnOrAfter.Add(skew)) {
		return fmt.Errorf("%w: assertion is outside its validity window", ErrInvalidXML)
	}
	return nil
}

func mapAssertion(assertion *crewjam.Assertion, shape xmlShape) *Assertion {
	result := &Assertion{
		ID: assertion.ID, ResponseID: shape.rootID, InResponseTo: shape.inResponse,
		Issuer: assertion.Issuer.Value, Subject: assertion.Subject.NameID.Value,
		Attributes: make(map[string][]string),
	}
	if len(assertion.AuthnStatements) > 0 {
		result.SessionIndex = assertion.AuthnStatements[0].SessionIndex
	}
	for _, statement := range assertion.AttributeStatements {
		for _, attribute := range statement.Attributes {
			name := attribute.Name
			if name == "" {
				name = attribute.FriendlyName
			}
			for _, value := range attribute.Values {
				result.Attributes[name] = append(result.Attributes[name], value.Value)
			}
		}
	}
	return result
}

func parseCertificate(value string) (*x509.Certificate, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.Join(strings.Fields(value), ""))
	if err != nil {
		return nil, fmt.Errorf("parse SAML certificate: %w", err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		return nil, fmt.Errorf("parse SAML certificate: %w", err)
	}
	return certificate, nil
}

func validCertificates(certificates []*x509.Certificate, now time.Time) error {
	for _, certificate := range certificates {
		if certificate != nil && !now.Before(certificate.NotBefore) && now.Before(certificate.NotAfter) {
			return nil
		}
	}
	return errors.New("SAML signing certificate is expired or not yet valid")
}

func absoluteURL(value string) (*url.URL, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return nil, errors.New("must be an absolute HTTP(S) URL")
	}
	return parsed, nil
}

func validateKeyPair(signer crypto.Signer, certificate *x509.Certificate) error {
	if signer == nil && certificate == nil {
		return nil
	}
	if signer == nil || certificate == nil {
		return errors.New("SAML signing key and certificate must be configured together")
	}
	publicKey, err := x509.MarshalPKIXPublicKey(signer.Public())
	if err != nil {
		return fmt.Errorf("marshal SAML signing public key: %w", err)
	}
	certificateKey, err := x509.MarshalPKIXPublicKey(certificate.PublicKey)
	if err != nil || !bytes.Equal(publicKey, certificateKey) {
		return errors.New("SAML signing key does not match certificate")
	}
	if signatureMethod(signer) == "" {
		return fmt.Errorf("unsupported SAML signing key type %T", signer)
	}
	return nil
}

func signatureMethod(signer crypto.Signer) string {
	switch signer.(type) {
	case *rsa.PrivateKey:
		return dsig.RSASHA256SignatureMethod
	case *ecdsa.PrivateKey:
		return dsig.ECDSASHA256SignatureMethod
	default:
		return ""
	}
}
