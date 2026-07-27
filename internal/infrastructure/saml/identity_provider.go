package saml

import (
	"crypto"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
)

type IdentityProviderConfig struct {
	MetadataURL                   string
	SSOURL                        string
	SigningKey                    crypto.Signer
	Certificate                   *x509.Certificate
	AdditionalSigningCertificates []*x509.Certificate
	Now                           func() time.Time
}

type IdentityProvider struct {
	provider                      *crewjam.IdentityProvider
	now                           func() time.Time
	additionalSigningCertificates []*x509.Certificate
}

func NewIdentityProvider(config IdentityProviderConfig) (*IdentityProvider, error) {
	metadataURL, err := absoluteURL(config.MetadataURL)
	if err != nil {
		return nil, fmt.Errorf("metadata URL: %w", err)
	}
	ssoURL, err := absoluteURL(config.SSOURL)
	if err != nil {
		return nil, fmt.Errorf("SSO URL: %w", err)
	}
	if config.SigningKey == nil || config.Certificate == nil {
		return nil, errors.New("SAML IdP signing key and certificate are required")
	}
	if err := validateKeyPair(config.SigningKey, config.Certificate); err != nil {
		return nil, err
	}
	now := config.Now
	if now == nil {
		now = time.Now
	}
	if err := validCertificates([]*x509.Certificate{config.Certificate}, now()); err != nil {
		return nil, err
	}
	return &IdentityProvider{
		provider: &crewjam.IdentityProvider{
			Signer: config.SigningKey, Certificate: config.Certificate,
			MetadataURL: *metadataURL, SSOURL: *ssoURL,
			SignatureMethod: signatureMethod(config.SigningKey),
		},
		now:                           now,
		additionalSigningCertificates: append([]*x509.Certificate(nil), config.AdditionalSigningCertificates...),
	}, nil
}

func (idp *IdentityProvider) MetadataXML() ([]byte, error) {
	metadata := idp.provider.Metadata()
	if len(metadata.IDPSSODescriptors) > 0 {
		descriptor := &metadata.IDPSSODescriptors[0].SSODescriptor.RoleDescriptor
		for _, certificate := range idp.additionalSigningCertificates {
			if certificate == nil {
				continue
			}
			descriptor.KeyDescriptors = append(descriptor.KeyDescriptors, crewjam.KeyDescriptor{Use: "signing", KeyInfo: crewjam.KeyInfo{X509Data: crewjam.X509Data{X509Certificates: []crewjam.X509Certificate{{Data: base64.StdEncoding.EncodeToString(certificate.Raw)}}}}})
		}
	}
	return xml.MarshalIndent(metadata, "", "  ")
}

type ResponseInput struct {
	RequestID    string
	SPEntityID   string
	ACSURL       string
	NameID       string
	NameIDFormat string
	SessionIndex string
	AuthnInstant time.Time
	ValidFor     time.Duration
	Attributes   map[string][]string
}

func (idp *IdentityProvider) SignResponse(input ResponseInput) ([]byte, error) {
	if strings.TrimSpace(input.RequestID) == "" || strings.TrimSpace(input.SPEntityID) == "" || strings.TrimSpace(input.NameID) == "" {
		return nil, errors.New("request ID, SP entity ID and NameID are required")
	}
	acsURL, err := absoluteURL(input.ACSURL)
	if err != nil {
		return nil, fmt.Errorf("ACS URL: %w", err)
	}
	now := idp.now()
	validFor := input.ValidFor
	if validFor <= 0 || validFor > 10*time.Minute {
		validFor = 5 * time.Minute
	}
	authnInstant := input.AuthnInstant
	if authnInstant.IsZero() {
		authnInstant = now
	}
	nameIDFormat := input.NameIDFormat
	if nameIDFormat == "" {
		nameIDFormat = string(crewjam.UnspecifiedNameIDFormat)
	}
	spMetadata := &crewjam.EntityDescriptor{EntityID: input.SPEntityID}
	spDescriptor := &crewjam.SPSSODescriptor{}
	endpoint := &crewjam.IndexedEndpoint{Binding: BindingPOST, Location: acsURL.String(), Index: 1}
	request := &crewjam.IdpAuthnRequest{
		IDP: idp.provider, Request: crewjam.AuthnRequest{ID: input.RequestID}, Now: now,
		ServiceProviderMetadata: spMetadata, SPSSODescriptor: spDescriptor, ACSEndpoint: endpoint,
	}
	assertionID, err := randomID()
	if err != nil {
		return nil, err
	}
	request.Assertion = &crewjam.Assertion{
		ID: "id-" + assertionID, IssueInstant: now, Version: "2.0",
		Issuer: crewjam.Issuer{Format: "urn:oasis:names:tc:SAML:2.0:nameid-format:entity", Value: idp.provider.MetadataURL.String()},
		Subject: &crewjam.Subject{
			NameID:               &crewjam.NameID{Format: nameIDFormat, NameQualifier: idp.provider.MetadataURL.String(), SPNameQualifier: input.SPEntityID, Value: input.NameID},
			SubjectConfirmations: []crewjam.SubjectConfirmation{{Method: "urn:oasis:names:tc:SAML:2.0:cm:bearer", SubjectConfirmationData: &crewjam.SubjectConfirmationData{InResponseTo: input.RequestID, NotOnOrAfter: now.Add(validFor), Recipient: acsURL.String()}}},
		},
		Conditions:          &crewjam.Conditions{NotBefore: now.Add(-time.Minute), NotOnOrAfter: now.Add(validFor), AudienceRestrictions: []crewjam.AudienceRestriction{{Audience: crewjam.Audience{Value: input.SPEntityID}}}},
		AuthnStatements:     []crewjam.AuthnStatement{{AuthnInstant: authnInstant, SessionIndex: input.SessionIndex, AuthnContext: crewjam.AuthnContext{AuthnContextClassRef: &crewjam.AuthnContextClassRef{Value: "urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport"}}}},
		AttributeStatements: []crewjam.AttributeStatement{{Attributes: responseAttributes(input.Attributes)}},
	}
	if err := request.MakeResponse(); err != nil {
		return nil, fmt.Errorf("sign SAML response: %w", err)
	}
	document := xml.Header
	serialized, err := elementBytes(request.ResponseEl)
	if err != nil {
		return nil, err
	}
	return []byte(document + string(serialized)), nil
}

func responseAttributes(values map[string][]string) []crewjam.Attribute {
	names := make([]string, 0, len(values))
	for name := range values {
		if strings.TrimSpace(name) != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	attributes := make([]crewjam.Attribute, 0, len(names))
	for _, name := range names {
		attribute := crewjam.Attribute{Name: name, NameFormat: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic"}
		for _, value := range values[name] {
			attribute.Values = append(attribute.Values, crewjam.AttributeValue{Type: "xs:string", Value: value})
		}
		attributes = append(attributes, attribute)
	}
	return attributes
}

func elementBytes(element *etree.Element) ([]byte, error) {
	document := etree.NewDocument()
	document.SetRoot(element)
	return document.WriteToBytes()
}

func randomID() (string, error) {
	buffer := make([]byte, 20)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate SAML ID: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}
