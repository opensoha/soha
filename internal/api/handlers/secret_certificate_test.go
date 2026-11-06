package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type certificateServiceStub struct {
	SecretService
	got [3]string
}

func (s *certificateServiceStub) GetSecretCertificate(_ context.Context, _ domainidentity.Principal, clusterID, namespace, name string) (domainresource.SecretCertificateView, error) {
	s.got = [3]string{clusterID, namespace, name}
	selfSigned := true
	return domainresource.SecretCertificateView{Name: name, Namespace: namespace, Status: "available", NotBefore: "2026-11-02T08:00:00Z", NotAfter: "2026-12-02T08:00:00Z", SelfSigned: &selfSigned}, nil
}
func TestSecretCertificateHandlerScopeAndClosedProjection(t *testing.T) {
	service := &certificateServiceStub{}
	h := &secretResourceHandler{service: service}
	r := gin.New()
	r.GET("/clusters/:clusterID/secrets/:name/certificate", h.GetSecretCertificate)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/clusters/cluster-a/secrets/web-tls/certificate?namespace=team-a", nil))
	if w.Code != 200 || service.got != [3]string{"cluster-a", "team-a", "web-tls"} {
		t.Fatalf("response=%d scope=%v", w.Code, service.got)
	}
	if !strings.Contains(w.Body.String(), "2026-12-02T08:00:00Z") {
		t.Fatal("certificate dates missing")
	}
	for _, s := range []string{"tls.key", "tls.crt", "BEGIN", "\"data\":{\"data\""} {
		if strings.Contains(w.Body.String(), s) {
			t.Fatalf("unexpected sensitive field %q", s)
		}
	}
}
