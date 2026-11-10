package radiusadapter

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/url"
	"testing"
	"time"
)

type networkRuntimeTLS struct {
	server, client *tls.Config
	leaf           *x509.Certificate
}

func newNetworkRuntimeTLS(t *testing.T, runtimeID string) networkRuntimeTLS {
	t.Helper()
	key := func() *ecdsa.PrivateKey {
		t.Helper()
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		return k
	}
	caKey := key()
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Task115 owned CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	leaf := func(id int64, client bool) (tls.Certificate, *x509.Certificate) {
		t.Helper()
		k := key()
		template := &x509.Certificate{SerialNumber: big.NewInt(id), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(30 * time.Minute), BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature}
		if client {
			identity, _ := url.Parse("spiffe://opensoha.local/network-control/nas/" + runtimeID)
			template.URIs = []*url.URL{identity}
			template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		} else {
			template.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			template.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		}
		der, err := x509.CreateCertificate(rand.Reader, template, ca, &k.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		parsed, err := x509.ParseCertificate(der)
		if err != nil {
			t.Fatal(err)
		}
		return tls.Certificate{Certificate: [][]byte{der, caDER}, PrivateKey: k, Leaf: parsed}, parsed
	}
	server, _ := leaf(2, false)
	client, parsed := leaf(3, true)
	return networkRuntimeTLS{server: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{server}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool}, client: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: pool, Certificates: []tls.Certificate{client}}, leaf: parsed}
}
