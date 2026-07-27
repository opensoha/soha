package saml

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/beevik/etree"
	crewjam "github.com/crewjam/saml"
	appidentityprovider "github.com/opensoha/soha/internal/application/identityprovider"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	dsig "github.com/russellhaering/goxmldsig"
)

const redirectRSASHA256 = "http://www.w3.org/2001/04/xmldsig-more#rsa-sha256"

// ProviderRuntime adapts the hardened XML and signing implementation to the
// identity-provider application boundary.
type ProviderRuntime struct{}

func NewProviderRuntime() *ProviderRuntime { return &ProviderRuntime{} }

func (r *ProviderRuntime) Metadata(material appidentityprovider.SAMLSigningMaterial) ([]byte, error) {
	idp, err := providerFromMaterial(material)
	if err != nil {
		return nil, err
	}
	return idp.MetadataXML()
}

func (r *ProviderRuntime) ValidateRequest(material appidentityprovider.SAMLSigningMaterial, input appidentityprovider.SAMLRequestInput) (appidentityprovider.SAMLValidatedRequest, error) {
	idp, err := providerFromMaterial(material)
	if err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, err
	}
	metadata, err := serviceProviderMetadata(input.ServiceProvider)
	if err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, err
	}
	idp.provider.ServiceProviderProvider = staticServiceProvider{metadata: metadata}
	httpRequest, err := authnHTTPRequest(material.SSOURL, input)
	if err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, err
	}
	request, err := crewjam.NewIdpAuthnRequest(idp.provider, httpRequest)
	if err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, fmt.Errorf("decode AuthnRequest: %w", err)
	}
	shape, err := inspectXML(request.RequestBuffer, false)
	if err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, err
	}
	if shape.root.Space != protocolNamespace || shape.root.Local != "AuthnRequest" || shape.rootID == "" {
		return appidentityprovider.SAMLValidatedRequest{}, fmt.Errorf("%w: expected a SAML AuthnRequest with an ID", ErrInvalidXML)
	}
	if err := validateAuthnRequestSignature(httpRequest, request.RequestBuffer, shape.signatures, input.ServiceProvider); err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, err
	}
	if err := request.Validate(); err != nil {
		return appidentityprovider.SAMLValidatedRequest{}, fmt.Errorf("validate AuthnRequest: %w", err)
	}
	if request.Request.IssueInstant.After(request.Now.Add(2 * time.Minute)) {
		return appidentityprovider.SAMLValidatedRequest{}, errors.New("AuthnRequest issue instant is in the future")
	}
	if request.Request.Issuer.Value != input.ServiceProvider.EntityID || request.ACSEndpoint == nil {
		return appidentityprovider.SAMLValidatedRequest{}, errors.New("AuthnRequest issuer or ACS is not registered")
	}
	return appidentityprovider.SAMLValidatedRequest{
		ID: request.Request.ID, Issuer: request.Request.Issuer.Value,
		ACSURL: request.ACSEndpoint.Location, RelayState: request.RelayState,
	}, nil
}

func (r *ProviderRuntime) SignResponse(material appidentityprovider.SAMLSigningMaterial, input appidentityprovider.SAMLResponseInput) ([]byte, error) {
	idp, err := providerFromMaterial(material)
	if err != nil {
		return nil, err
	}
	return idp.SignResponse(ResponseInput{
		RequestID: input.RequestID, SPEntityID: input.SPEntityID, ACSURL: input.ACSURL,
		NameID: input.NameID, NameIDFormat: input.NameIDFormat, SessionIndex: input.SessionIndex,
		AuthnInstant: input.AuthnInstant, Attributes: input.Attributes,
	})
}

func providerFromMaterial(material appidentityprovider.SAMLSigningMaterial) (*IdentityProvider, error) {
	key, err := parsePrivateKey(material.PrivateKeyPEM)
	if err != nil {
		return nil, err
	}
	certificate, err := parsePEMCertificate(material.CertificatePEM)
	if err != nil {
		return nil, err
	}
	additional := make([]*x509.Certificate, 0, len(material.AdditionalCertificatesPEM))
	for _, encoded := range material.AdditionalCertificatesPEM {
		candidate, parseErr := parsePEMCertificate(encoded)
		if parseErr != nil {
			return nil, parseErr
		}
		additional = append(additional, candidate)
	}
	return NewIdentityProvider(IdentityProviderConfig{
		MetadataURL: material.MetadataURL, SSOURL: material.SSOURL,
		SigningKey: key, Certificate: certificate, AdditionalSigningCertificates: additional,
	})
}

func parsePrivateKey(value string) (crypto.Signer, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil {
		return nil, errors.New("SAML private key is not valid PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse SAML private key: %w", err)
	}
	signer, ok := key.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("unsupported SAML private key type %T", key)
	}
	return signer, nil
}

func parsePEMCertificate(value string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(value))
	if block == nil || block.Type != "CERTIFICATE" {
		return nil, errors.New("SAML certificate is not valid PEM")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse SAML certificate: %w", err)
	}
	return certificate, nil
}

func serviceProviderMetadata(item domainprovider.SAMLServiceProvider) (*crewjam.EntityDescriptor, error) {
	if strings.TrimSpace(item.EntityID) == "" || len(item.AssertionConsumerServiceURLs) == 0 {
		return nil, errors.New("SAML service provider entity ID and ACS are required")
	}
	endpoints := make([]crewjam.IndexedEndpoint, 0, len(item.AssertionConsumerServiceURLs))
	for index, value := range item.AssertionConsumerServiceURLs {
		if _, err := absoluteURL(value); err != nil {
			return nil, fmt.Errorf("SAML ACS URL: %w", err)
		}
		isDefault := index == 0
		endpoints = append(endpoints, crewjam.IndexedEndpoint{
			Binding: BindingPOST, Location: value, Index: index + 1, IsDefault: &isDefault,
		})
	}
	wantAssertionsSigned := item.WantAssertionsSigned
	return &crewjam.EntityDescriptor{
		EntityID: item.EntityID,
		SPSSODescriptors: []crewjam.SPSSODescriptor{{
			SSODescriptor: crewjam.SSODescriptor{RoleDescriptor: crewjam.RoleDescriptor{
				ProtocolSupportEnumeration: protocolNamespace,
			}},
			WantAssertionsSigned:      &wantAssertionsSigned,
			AssertionConsumerServices: endpoints,
		}},
	}, nil
}

type staticServiceProvider struct{ metadata *crewjam.EntityDescriptor }

func (p staticServiceProvider) GetServiceProvider(_ *http.Request, entityID string) (*crewjam.EntityDescriptor, error) {
	if p.metadata == nil || entityID != p.metadata.EntityID {
		return nil, os.ErrNotExist
	}
	return p.metadata, nil
}

func authnHTTPRequest(ssoURL string, input appidentityprovider.SAMLRequestInput) (*http.Request, error) {
	method := strings.ToUpper(strings.TrimSpace(input.Method))
	values := url.Values{"SAMLRequest": {input.Encoded}}
	if input.RelayState != "" {
		values.Set("RelayState", input.RelayState)
	}
	if method == http.MethodGet {
		rawQuery := input.RawQuery
		if rawQuery == "" {
			rawQuery = values.Encode()
		}
		return http.NewRequestWithContext(context.Background(), method, strings.TrimRight(ssoURL, "?")+"?"+rawQuery, nil)
	}
	if method != http.MethodPost {
		return nil, errors.New("SAML binding must use GET or POST")
	}
	request, err := http.NewRequestWithContext(context.Background(), method, ssoURL, strings.NewReader(values.Encode()))
	if err == nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	return request, err
}

func validateAuthnRequestSignature(request *http.Request, document []byte, signatures int, provider domainprovider.SAMLServiceProvider) error {
	hasRedirectSignature := request.Method == http.MethodGet && request.URL.Query().Get("Signature") != ""
	hasSignature := signatures > 0 || hasRedirectSignature
	if !provider.WantAuthnRequestsSigned && !hasSignature {
		return nil
	}
	if strings.TrimSpace(provider.SigningCertificatePEM) == "" {
		return errors.New("signed AuthnRequest requires a registered SP certificate")
	}
	certificate, err := parsePEMCertificate(provider.SigningCertificatePEM)
	if err != nil {
		return fmt.Errorf("parse SP signing certificate: %w", err)
	}
	if err := validCertificates([]*x509.Certificate{certificate}, time.Now().UTC()); err != nil {
		return err
	}
	if request.Method == http.MethodGet {
		if !hasRedirectSignature {
			return errors.New("AuthnRequest signature is required")
		}
		return verifyRedirectSignature(request.URL.RawQuery, certificate)
	}
	if signatures == 0 {
		return errors.New("AuthnRequest signature is required")
	}
	return verifyPOSTSignature(document, certificate)
}

func verifyPOSTSignature(document []byte, certificate *x509.Certificate) error {
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(document); err != nil {
		return err
	}
	context := dsig.NewDefaultValidationContext(&dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{certificate}})
	context.IdAttribute = "ID"
	validated, err := context.Validate(doc.Root())
	if err != nil {
		return fmt.Errorf("validate AuthnRequest signature: %w", err)
	}
	if validated.Tag != "AuthnRequest" {
		return errors.New("signed XML root is not AuthnRequest")
	}
	return nil
}

func verifyRedirectSignature(rawQuery string, certificate *x509.Certificate) error {
	requestValue, ok := rawQueryValue(rawQuery, "SAMLRequest")
	if !ok {
		return errors.New("signed redirect is missing SAMLRequest")
	}
	sigAlgValue, ok := rawQueryValue(rawQuery, "SigAlg")
	if !ok {
		return errors.New("signed redirect is missing SigAlg")
	}
	sigAlg, err := url.QueryUnescape(sigAlgValue)
	if err != nil || sigAlg != redirectRSASHA256 {
		return ErrUnsupportedAlg
	}
	signatureValue, ok := rawQueryValue(rawQuery, "Signature")
	if !ok {
		return errors.New("signed redirect is missing Signature")
	}
	signed := "SAMLRequest=" + requestValue
	if relayState, exists := rawQueryValue(rawQuery, "RelayState"); exists {
		signed += "&RelayState=" + relayState
	}
	signed += "&SigAlg=" + sigAlgValue
	decodedSignature, err := url.QueryUnescape(signatureValue)
	if err != nil {
		return err
	}
	signature, err := base64.StdEncoding.DecodeString(decodedSignature)
	if err != nil {
		return err
	}
	publicKey, ok := certificate.PublicKey.(*rsa.PublicKey)
	if !ok {
		return errors.New("RSA-SHA256 redirect signature requires an RSA certificate")
	}
	digest := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, digest[:], signature); err != nil {
		return fmt.Errorf("validate redirect signature: %w", err)
	}
	return nil
}

func rawQueryValue(rawQuery, name string) (string, bool) {
	for _, part := range strings.Split(rawQuery, "&") {
		key, value, found := strings.Cut(part, "=")
		if found && key == name {
			return value, true
		}
	}
	return "", false
}
