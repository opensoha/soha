package proxypublication

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"github.com/gin-gonic/gin"
	controlapi "github.com/opensoha/soha/internal/api/networkcontrol"
	domain "github.com/opensoha/soha/internal/domain/proxypublication"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func receiptClientCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(issuer)
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identity, _ := url.Parse("spiffe://opensoha.local/network-control/endpoint/endpoint-1")
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{identity}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, issuer, &leafKey.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{leafDER, der}, PrivateKey: leafKey}, roots
}
func TestRejectedProjectionReceiptOverVerifiedMTLS(t *testing.T) {
	b, store, _ := ingressFixture()
	store.record.Public.ProjectionHash = "sha256:" + strings.Repeat("a", 64)
	b.endpoint = endpointFake{value: domain.EndpointContext{UserID: "user-1", DeviceID: "device-1", RuntimeID: "endpoint-1", ProfileID: "profile-1", BundleID: "bundle-1", OrgRevision: 1, SourceRevision: 1}}
	handler, err := controlapi.NewPublicationHandler(b, controlapi.Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 100})
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.POST("/api/network-control/v1/runtimes/:runtimeID/mihomo-profiles/:profileID/projection/receipt", handler.ProjectionReceipt)
	certificate, roots := receiptClientCertificate(t)
	server := httptest.NewUnstartedServer(router)
	server.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	client := server.Client()
	transport := fixtureValue[*http.Transport](t, client.Transport).Clone()
	transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	transport.TLSClientConfig.Certificates = []tls.Certificate{certificate}
	client.Transport = transport
	client.Timeout = 5 * time.Second
	t.Cleanup(transport.CloseIdleConnections)
	code, body, cache := sendReceipt(t, client, server.URL, "rejected", store.record.Public.ProjectionHash)
	if code != 200 || cache != "private, no-store" || !strings.Contains(body, `"status":"rejected"`) || strings.Contains(body, "synthetic-sensitive") {
		t.Fatal("rejected receipt failed", code, body)
	}
	if store.receipt.Status != "rejected" || store.receipt.ReasonCode != "projection_apply_rejected" {
		t.Fatal("receipt status not retained", store.receipt)
	}
	code, _, _ = sendReceipt(t, client, server.URL, "failed", store.record.Public.ProjectionHash)
	if code != 409 || store.receipt.Status != "rejected" {
		t.Fatal("illegal failed receipt accepted or overwrote status", code)
	}
	code, _, _ = sendReceipt(t, client, server.URL, "applied", store.record.Public.ProjectionHash)
	if code != 200 || store.receipt.Status != "applied" || store.receipt.ReasonCode != "" {
		t.Fatal("valid applied receipt rejected or unsanitized", code)
	}
}
func sendReceipt(t *testing.T, client *http.Client, base, status, hash string) (int, string, string) {
	t.Helper()
	body := `{"bundleId":"bundle-1","revision":1,"projectionHash":"` + hash + `","processEpoch":"boot-1","status":"` + status + `","reasonCode":"synthetic-sensitive-upstream"}`
	response, err := client.Post(base+"/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/profile-1/projection/receipt", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(raw), response.Header.Get("Cache-Control")
}
