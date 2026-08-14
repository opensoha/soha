package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"testing"
	"time"

	appidentityprovider "github.com/opensoha/soha/internal/application/identityprovider"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

func TestLoginRuntimeUsesPinnedMetadataWithoutNetwork(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, certificate := testCertificate(t, now)
	idp := mustIdentityProvider(t, key, certificate, now)
	metadataXML, err := idp.MetadataXML()
	if err != nil {
		t.Fatal(err)
	}
	runtime := &LoginRuntime{client: nil, now: func() time.Time { return now }}
	request, err := runtime.Begin(context.Background(), domainsettings.LoginProviderSettings{
		EntityID: "https://sp.example.test/metadata", RedirectURL: "https://sp.example.test/acs",
		MetadataURL: "https://unreachable.invalid/metadata", MetadataXML: string(metadataXML),
	}, "relay")
	if err != nil || request.ID == "" || request.RedirectURL == "" {
		t.Fatalf("Begin with pinned metadata = %#v, error=%v", request, err)
	}
}

func TestLoginRuntimeMetadataUsesEntityIDAndACSSeparately(t *testing.T) {
	runtime := &LoginRuntime{}
	metadata, err := runtime.Metadata(context.Background(), domainsettings.LoginProviderSettings{
		EntityID:    "https://sp.example.test/metadata",
		RedirectURL: "https://sp.example.test/acs",
	})
	if err != nil {
		t.Fatalf("Metadata returned error: %v", err)
	}
	text := string(metadata)
	if !strings.Contains(text, `entityID="https://sp.example.test/metadata"`) || !strings.Contains(text, `Location="https://sp.example.test/acs"`) {
		t.Fatalf("metadata does not separate entity ID and ACS: %s", text)
	}
}

func TestProviderRuntimeValidatesRegisteredServiceProvider(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, certificate := testCertificate(t, now)
	privateDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	material := appidentityprovider.SAMLSigningMaterial{
		PrivateKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})),
		CertificatePEM: string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Raw})),
		MetadataURL:    "https://idp.example.test/saml2/idp/provider-1/metadata",
		SSOURL:         "https://idp.example.test/saml2/idp/provider-1/sso",
	}
	serviceProvider := domainprovider.SAMLServiceProvider{
		EntityID:                     "https://sp.example.test/metadata",
		AssertionConsumerServiceURLs: []string{"https://sp.example.test/acs"},
	}
	runtime := NewProviderRuntime()
	requestXML := fmt.Sprintf(`<samlp:AuthnRequest xmlns:samlp="%s" xmlns:saml="%s" ID="request-1" Version="2.0" IssueInstant="%s" Destination="%s" AssertionConsumerServiceURL="https://sp.example.test/acs"><saml:Issuer>https://sp.example.test/metadata</saml:Issuer></samlp:AuthnRequest>`,
		protocolNamespace, assertionNamespace, now.Format(time.RFC3339), material.SSOURL)
	input := appidentityprovider.SAMLRequestInput{
		Method: "POST", Encoded: base64.StdEncoding.EncodeToString([]byte(requestXML)),
		RelayState: "opaque", ServiceProvider: serviceProvider,
	}
	validated, err := runtime.ValidateRequest(material, input)
	if err != nil {
		t.Fatalf("validate registered request: %v", err)
	}
	if validated.ID != "request-1" || validated.Issuer != serviceProvider.EntityID || validated.ACSURL != serviceProvider.AssertionConsumerServiceURLs[0] || validated.RelayState != "opaque" {
		t.Fatalf("unexpected request: %#v", validated)
	}

	t.Run("unregistered ACS", func(t *testing.T) {
		changed := strings.Replace(requestXML, "https://sp.example.test/acs", "https://attacker.example.test/acs", 1)
		input.Encoded = base64.StdEncoding.EncodeToString([]byte(changed))
		if _, err := runtime.ValidateRequest(material, input); err == nil {
			t.Fatal("expected unregistered ACS rejection")
		}
	})

	t.Run("required signature", func(t *testing.T) {
		input.Encoded = base64.StdEncoding.EncodeToString([]byte(requestXML))
		input.ServiceProvider.WantAuthnRequestsSigned = true
		input.ServiceProvider.SigningCertificatePEM = material.CertificatePEM
		if _, err := runtime.ValidateRequest(material, input); err == nil {
			t.Fatal("expected unsigned request rejection")
		}
	})
}

func TestSignedResponseRoundTrip(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	key, certificate := testCertificate(t, now)
	idp := mustIdentityProvider(t, key, certificate, now)
	metadataXML, err := idp.MetadataXML()
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	metadata, err := ParseMetadata(metadataXML, now)
	if err != nil {
		t.Fatalf("parse metadata: %v", err)
	}
	sp, err := NewServiceProvider(ServiceProviderConfig{
		EntityID: "https://sp.example.test/metadata", MetadataURL: "https://sp.example.test/metadata",
		ACSURL: "https://sp.example.test/acs", IDPMetadata: metadata,
		SigningKey: key, SigningCertificate: certificate, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create SP: %v", err)
	}

	redirect, err := sp.BuildAuthnRequest(BindingRedirect, "return=/identity&tab=apps")
	if err != nil {
		t.Fatalf("build Redirect request: %v", err)
	}
	redirectURL, err := url.Parse(redirect.RedirectURL)
	if err != nil || redirect.ID == "" || redirectURL.Query().Get("RelayState") != "return=/identity&tab=apps" || redirectURL.Query().Get("Signature") == "" {
		t.Fatalf("invalid Redirect request: id=%q url=%q err=%v", redirect.ID, redirect.RedirectURL, err)
	}
	post, err := sp.BuildAuthnRequest(BindingPOST, "opaque")
	if err != nil || post.ID == "" || !strings.Contains(string(post.POSTForm), `name="SAMLRequest"`) {
		t.Fatalf("invalid POST request: id=%q err=%v", post.ID, err)
	}

	rawResponse, err := idp.SignResponse(ResponseInput{
		RequestID: redirect.ID, SPEntityID: "https://sp.example.test/metadata", ACSURL: "https://sp.example.test/acs",
		NameID: "subject-123", SessionIndex: "session-123", Attributes: map[string][]string{"email": {"user@example.test"}},
	})
	if err != nil {
		t.Fatalf("sign response: %v", err)
	}
	assertion, err := sp.ValidateResponse(base64.StdEncoding.EncodeToString(rawResponse), redirect.ID)
	if err != nil {
		t.Fatalf("validate response: %v", err)
	}
	if assertion.ID == "" || assertion.ResponseID == "" || assertion.Subject != "subject-123" || assertion.SessionIndex != "session-123" || len(assertion.Attributes["email"]) != 1 {
		t.Fatalf("unexpected assertion: %#v", assertion)
	}

	t.Run("destination", func(t *testing.T) {
		wrong, signErr := idp.SignResponse(ResponseInput{
			RequestID: redirect.ID, SPEntityID: "https://sp.example.test/metadata", ACSURL: "https://attacker.example.test/acs", NameID: "subject-123",
		})
		if signErr != nil {
			t.Fatal(signErr)
		}
		if _, validateErr := sp.ValidateResponse(base64.StdEncoding.EncodeToString(wrong), redirect.ID); validateErr == nil {
			t.Fatal("expected destination/recipient mismatch")
		}
	})

	t.Run("audience", func(t *testing.T) {
		wrong, signErr := idp.SignResponse(ResponseInput{
			RequestID: redirect.ID, SPEntityID: "https://other-sp.example.test/metadata", ACSURL: "https://sp.example.test/acs", NameID: "subject-123",
		})
		if signErr != nil {
			t.Fatal(signErr)
		}
		if _, validateErr := sp.ValidateResponse(base64.StdEncoding.EncodeToString(wrong), redirect.ID); validateErr == nil {
			t.Fatal("expected audience mismatch")
		}
	})

	t.Run("unknown certificate", func(t *testing.T) {
		otherKey, otherCertificate := testCertificate(t, now)
		otherIDP := mustIdentityProvider(t, otherKey, otherCertificate, now)
		wrong, signErr := otherIDP.SignResponse(ResponseInput{
			RequestID: redirect.ID, SPEntityID: "https://sp.example.test/metadata", ACSURL: "https://sp.example.test/acs", NameID: "subject-123",
		})
		if signErr != nil {
			t.Fatal(signErr)
		}
		if _, validateErr := sp.ValidateResponse(base64.StdEncoding.EncodeToString(wrong), redirect.ID); validateErr == nil {
			t.Fatal("expected unknown certificate rejection")
		}
	})

	t.Run("weak algorithm", func(t *testing.T) {
		weak := strings.Replace(string(rawResponse), "rsa-sha256", "rsa-sha1", 1)
		_, validateErr := sp.ValidateResponse(base64.StdEncoding.EncodeToString([]byte(weak)), redirect.ID)
		if !errors.Is(validateErr, ErrUnsupportedAlg) {
			t.Fatalf("expected weak algorithm rejection, got %v", validateErr)
		}
	})
}

func TestXMLSecurityLimits(t *testing.T) {
	tests := []struct {
		name string
		xml  string
	}{
		{name: "doctype", xml: `<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]><x>&e;</x>`},
		{name: "duplicate ID", xml: `<Response ID="same"><Assertion ID="same"/></Response>`},
		{name: "too deep", xml: strings.Repeat("<x>", maxXMLDepth+1) + strings.Repeat("</x>", maxXMLDepth+1)},
		{name: "too large", xml: `<x>` + strings.Repeat("a", maxXMLBytes) + `</x>`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := inspectXML([]byte(test.xml), true); !errors.Is(err, ErrInvalidXML) {
				t.Fatalf("expected invalid XML, got %v", err)
			}
		})
	}
}

func TestMetadataRejectsExpiredCertificate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	certificateTime := now.Add(-48 * time.Hour)
	key, certificate := testCertificate(t, certificateTime)
	idp := mustIdentityProvider(t, key, certificate, certificateTime)
	metadata, err := idp.MetadataXML()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseMetadata(metadata, now); !errors.Is(err, ErrInvalidXML) {
		t.Fatalf("expected expired certificate rejection, got %v", err)
	}
}

func TestIdentityProviderMetadataIncludesRetiringSigningCertificate(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	activeKey, activeCertificate := testCertificate(t, now)
	_, retiringCertificate := testCertificate(t, now)
	idp, err := NewIdentityProvider(IdentityProviderConfig{
		MetadataURL: "https://idp.example.test/metadata", SSOURL: "https://idp.example.test/sso",
		SigningKey: activeKey, Certificate: activeCertificate,
		AdditionalSigningCertificates: []*x509.Certificate{retiringCertificate},
		Now:                           func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	metadata, err := idp.MetadataXML()
	if err != nil {
		t.Fatal(err)
	}
	for _, certificate := range []*x509.Certificate{activeCertificate, retiringCertificate} {
		encoded := base64.StdEncoding.EncodeToString(certificate.Raw)
		if !strings.Contains(string(metadata), encoded) {
			t.Fatalf("metadata does not include certificate %s", certificate.Subject)
		}
	}
}

func TestMetadataURLRejectsSSRFHosts(t *testing.T) {
	for _, value := range []string{
		"http://idp.example.test/metadata",
		"https://127.0.0.1/metadata",
		"https://169.254.169.254/latest/meta-data",
		"https://[::1]/metadata",
		"https://user:password@idp.example.test/metadata",
	} {
		if _, err := safeMetadataURL(value); err == nil {
			t.Fatalf("expected metadata URL %q to be rejected", value)
		}
	}
}

func TestMetadataURLRejectsInternalNetworks(t *testing.T) {
	for _, rawURL := range []string{
		"http://idp.example.test/metadata",
		"https://127.0.0.1/metadata",
		"https://169.254.169.254/latest/meta-data",
		"https://10.0.0.1/metadata",
		"https://[::1]/metadata",
	} {
		if err := validateMetadataURL(rawURL); err == nil {
			t.Fatalf("validateMetadataURL(%q) error = nil", rawURL)
		}
	}
	if err := validateMetadataURL("https://idp.example.test/metadata"); err != nil {
		t.Fatalf("public metadata URL rejected: %v", err)
	}
}

func mustIdentityProvider(t *testing.T, key *rsa.PrivateKey, certificate *x509.Certificate, now time.Time) *IdentityProvider {
	t.Helper()
	idp, err := NewIdentityProvider(IdentityProviderConfig{
		MetadataURL: "https://idp.example.test/metadata", SSOURL: "https://idp.example.test/sso",
		SigningKey: key, Certificate: certificate, Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("create IdP: %v", err)
	}
	return idp
}

func testCertificate(t *testing.T, center time.Time) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(center.UnixNano()), Subject: pkix.Name{CommonName: "Soha SAML test"},
		NotBefore: center.Add(-time.Hour), NotAfter: center.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(raw)
	if err != nil {
		t.Fatal(err)
	}
	return key, certificate
}
