package providerportal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	infrasaml "github.com/opensoha/soha/internal/infrastructure/saml"
	"golang.org/x/net/html"
)

//nolint:funlen // One SP lifecycle verifies request replay, certificate rotation and overlap using the same signed response.
func TestSAMLProtocolHTTPWithPostgres(t *testing.T) {
	f := newSSOProtocolFixture(t)
	application, pending := newSSOTestServiceProvider()
	defer application.Close()
	input := onboardingTestInput("saml")
	input.Provider.Config = map[string]any{"entityId": application.URL + "/metadata", "assertionConsumerServiceUrls": []string{application.URL + "/acs"}, "attributeMappings": []any{map[string]any{"source": "email", "target": "email"}}}
	result := f.onboard(t, input)
	metadataURL := f.server.URL + "/saml2/idp/" + result.Provider.ID + "/metadata"
	newSP := func(metadata *infrasaml.Metadata, entity, acs string, now func() time.Time) *infrasaml.ServiceProvider {
		t.Helper()
		sp, err := infrasaml.NewServiceProvider(infrasaml.ServiceProviderConfig{EntityID: entity, MetadataURL: application.URL + "/metadata", ACSURL: acs, IDPMetadata: metadata, Now: now})
		ssoNoError(t, err)
		return sp
	}
	metadata := loadSSOIdPMetadata(t, f, metadataURL)
	sp := newSP(metadata, application.URL+"/metadata", application.URL+"/acs", nil)
	var oldResponse, oldRequestID string
	for _, binding := range []string{infrasaml.BindingRedirect, infrasaml.BindingPOST} {
		auth, err := sp.BuildAuthnRequest(binding, "opaque-relay")
		ssoNoError(t, err)
		body, _ := ssoHTTP(t, f.client, samlHTTPAuthRequest(t, f, auth, true), http.StatusOK)
		action, values := ssoHTMLForm(t, body)
		ssoCheck(t, action == application.URL+"/acs" && values.Get("SAMLResponse") != "", "SAML did not target registered ACS")
		pending <- ssoPendingSAMLRequest{sp: sp, id: auth.ID}
		acsRequest, _ := http.NewRequest("POST", action, strings.NewReader(values.Encode()))
		acsRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		assertionBody, _ := ssoHTTP(t, f.client, acsRequest, http.StatusOK)
		var assertion infrasaml.Assertion
		ssoNoError(t, json.Unmarshal(assertionBody, &assertion))
		ssoCheck(t, assertion.Subject == f.principal.Email, "unexpected subject %q", assertion.Subject)
		ssoCheck(t, assertion.SessionIndex == f.sessionID("saml"), "unexpected session %q", assertion.SessionIndex)
		ssoCheck(t, slices.Equal(assertion.Attributes["email"], []string{f.principal.Email}), "unexpected email attributes %v", assertion.Attributes["email"])
		ssoHTTP(t, f.client, samlHTTPAuthRequest(t, f, auth, true), http.StatusUnauthorized)
		oldResponse, oldRequestID = values.Get("SAMLResponse"), auth.ID
	}
	for name, invalidSP := range map[string]*infrasaml.ServiceProvider{
		"ACS":       newSP(metadata, application.URL+"/metadata", application.URL+"/unregistered", nil),
		"Entity ID": newSP(metadata, application.URL+"/unknown", application.URL+"/acs", nil),
	} {
		t.Run(name, func(t *testing.T) {
			auth, err := invalidSP.BuildAuthnRequest(infrasaml.BindingRedirect, "opaque-relay")
			ssoNoError(t, err)
			ssoHTTP(t, f.client, samlHTTPAuthRequest(t, f, auth, true), http.StatusBadRequest)
		})
	}
	unsigned, err := base64.StdEncoding.DecodeString(oldResponse)
	ssoNoError(t, err)
	tampered := bytes.ReplaceAll(unsigned, []byte(f.principal.Email), []byte("forged@example.test"))
	ssoCheck(t, !(bytes.Equal(tampered, unsigned)), "tamper fixture missed signed attribute")
	if _, err := sp.ValidateResponse(base64.StdEncoding.EncodeToString(tampered), oldRequestID); err == nil {
		t.Fatal("SP accepted tampered signature")
	}
	expiredSP := newSP(metadata, application.URL+"/metadata", application.URL+"/acs", func() time.Time { return time.Now().Add(time.Hour) })
	if _, err := expiredSP.ValidateResponse(oldResponse, oldRequestID); err == nil {
		t.Fatal("SP accepted expired assertion")
	}
	if _, err := sp.ValidateResponse(oldResponse, "unrelated-request"); err == nil {
		t.Fatal("SP accepted wrong response correlation")
	}
	rotation, err := f.providers.RotateSAMLProviderCertificate(context.Background(), f.principal, result.Provider.ID, sohaapi.SAMLCertificateRotateRequest{OverlapSeconds: 3600})
	ssoCheck(t, err == nil && rotation.Active.ID != rotation.Retiring.ID, "certificate rotation: %v", err)
	rotatedMetadata := loadSSOIdPMetadata(t, f, metadataURL)
	ssoCheck(t, len(rotatedMetadata.SigningCertificates) == 2, "metadata omitted overlap certificate")
	rotatedSP := newSP(rotatedMetadata, application.URL+"/metadata", application.URL+"/acs", nil)
	if _, err := rotatedSP.ValidateResponse(oldResponse, oldRequestID); err != nil {
		t.Fatalf("retiring certificate no longer trusted during overlap: %v", err)
	}
	auth, err := rotatedSP.BuildAuthnRequest(infrasaml.BindingRedirect, "opaque-relay")
	ssoNoError(t, err)
	body, _ := ssoHTTP(t, f.client, samlHTTPAuthRequest(t, f, auth, true), http.StatusOK)
	action, values := ssoHTMLForm(t, body)
	ssoCheck(t, action == application.URL+"/acs", "rotated response changed ACS")
	if _, err := sp.ValidateResponse(values.Get("SAMLResponse"), auth.ID); err == nil {
		t.Fatal("stale metadata accepted untrusted new certificate")
	}
	pending <- ssoPendingSAMLRequest{sp: rotatedSP, id: auth.ID}
	acsRequest, _ := http.NewRequest("POST", action, strings.NewReader(values.Encode()))
	acsRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ssoHTTP(t, f.client, acsRequest, http.StatusOK)
	login, err := rotatedSP.BuildAuthnRequest(infrasaml.BindingRedirect, "opaque-relay")
	ssoNoError(t, err)
	_, headers := ssoHTTP(t, f.client, samlHTTPAuthRequest(t, f, login, false), http.StatusFound)
	ssoCheck(t, strings.HasPrefix(headers.Get("Location"), "/login?"), "anonymous request did not require platform login")
}

func ssoHTMLForm(t *testing.T, body []byte) (string, url.Values) {
	t.Helper()
	document, err := html.Parse(bytes.NewReader(body))
	ssoNoError(t, err)
	action, values := "", url.Values{}
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		if node.Type == html.ElementNode && (node.Data == "form" || node.Data == "input") {
			attributes := map[string]string{}
			for _, attr := range node.Attr {
				attributes[attr.Key] = attr.Val
			}
			if node.Data == "form" {
				action = attributes["action"]
			} else if attributes["name"] != "" {
				values.Set(attributes["name"], attributes["value"])
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(document)
	ssoCheck(t, action != "" && len(values) != 0, "missing protocol POST form")
	return action, values
}

type ssoPendingSAMLRequest struct {
	sp *infrasaml.ServiceProvider
	id string
}

func newSSOTestServiceProvider() (*httptest.Server, chan ssoPendingSAMLRequest) {
	pending := make(chan ssoPendingSAMLRequest, 1)
	application := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/acs" || r.Method != "POST" {
			http.NotFound(w, r)
			return
		}
		expected := <-pending
		assertion, err := expected.sp.ValidateResponse(r.FormValue("SAMLResponse"), expected.id)
		if err != nil || r.FormValue("RelayState") != "opaque-relay" {
			http.Error(w, "SAML verification failed", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(assertion)
	}))
	return application, pending
}

func samlHTTPAuthRequest(t *testing.T, f *ssoProtocolFixture, auth infrasaml.AuthnRequest, authenticated bool) *http.Request {
	t.Helper()
	var request *http.Request
	if auth.RedirectURL != "" {
		request, _ = http.NewRequest("GET", auth.RedirectURL, nil)
	} else {
		action, values := ssoHTMLForm(t, auth.POSTForm)
		request, _ = http.NewRequest("POST", action, strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if authenticated {
		request.AddCookie(&http.Cookie{Name: "test-platform-session", Value: f.login(t, "saml")})
	}
	return request
}

func loadSSOIdPMetadata(t *testing.T, f *ssoProtocolFixture, metadataURL string) *infrasaml.Metadata {
	t.Helper()
	request, _ := http.NewRequest("GET", metadataURL, nil)
	body, _ := ssoHTTP(t, f.client, request, http.StatusOK)
	metadata, err := infrasaml.ParseMetadata(body, time.Now())
	ssoCheck(t, err == nil && metadata.EntityID == metadataURL, "metadata: %v", err)
	return metadata
}
