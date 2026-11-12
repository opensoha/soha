package networkproxyruntime

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func TestSOCKSUpstreamTCPRenewsBeyondOriginalInnerLease(t *testing.T) {
	f := newSOCKSUpstreamFixture(t, nil, true)
	g, b := installFixtureSOCKSBridge(t, f)
	const window = 300 * time.Millisecond
	initial := time.Now().Add(window)
	g.udpMu.Lock()
	g.udpUntil = initial
	g.udpMu.Unlock()
	if err := g.upstreamBridges[0].gate.Refresh(initial); err != nil {
		t.Fatal(err)
	}
	c, err := dialOwnedTCP(context.Background(), b, "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	started := time.Now()
	for time.Since(started) < 3*window {
		until := time.Now().Add(window)
		if err := g.upstreamBridges[0].gate.Refresh(until); err != nil {
			t.Fatal(err)
		}
		g.udpMu.Lock()
		g.udpUntil = until
		g.udpMu.Unlock()
		_ = c.SetDeadline(time.Now().Add(200 * time.Millisecond))
		if _, err := c.Write([]byte("x")); err != nil {
			t.Fatalf("t=%v renewed stream write: %v", time.Since(started), err)
		}
		one := make([]byte, 1)
		if _, err := io.ReadFull(c, one); err != nil || string(one) != "x" {
			t.Fatalf("t=%v original inner lease was not renewed: %q %v", time.Since(started), one, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if f.targets.Load() != 1 {
		t.Fatal("renewal replaced the original upstream TCP session")
	}
}

func TestSOCKSUpstreamTCPRefreshCommitsCurrentDeadline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "failed gate"}[fail], func(t *testing.T) {
			f := newSOCKSUpstreamFixture(t, nil, true)
			g, b := installFixtureSOCKSBridge(t, f)
			old := time.Now().Add(350 * time.Millisecond)
			g.udpMu.Lock()
			g.udpUntil = old
			g.udpMu.Unlock()
			if err := g.upstreamBridges[0].gate.Refresh(old); err != nil {
				t.Fatal(err)
			}
			gate, err := proxytransport.NewSOCKS5Gate(map[string]string{"guard": "guard-fixture-secret-123456789012345"}, func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }, func(context.Context, string, string) (net.Conn, error) { return nil, net.ErrClosed }, func(context.Context, string, string) error { return net.ErrClosed }, old)
			if err != nil {
				t.Fatal(err)
			}
			g.socks = gate
			conn, err := dialOwnedTCP(context.Background(), b, "1.1.1.1:443")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if fail {
				_ = gate.Close()
			}
			until := old.Add(time.Second)
			err = g.refresh(until)
			if fail {
				if err == nil || !g.udpLease().Equal(old) {
					t.Fatal("failed refresh published candidate authorization", err)
				}
				assertPacketClosed(t, conn, time.Now().Add(time.Second))
				return
			}
			if err != nil || !g.udpLease().Equal(until) {
				t.Fatal("fresh authorization not committed", err)
			}
			time.Sleep(time.Until(old.Add(25 * time.Millisecond)))
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			if _, err := conn.Write([]byte("x")); err != nil {
				t.Fatal("original TCP was lost at old deadline", err)
			}
			if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil || f.targets.Load() != 1 {
				t.Fatal("renewal did not preserve original TCP", err)
			}
		})
	}
}

type recordedTCPRefresh struct {
	net.Conn
	last   time.Time
	closed bool
}

func (c *recordedTCPRefresh) Refresh(until time.Time) error {
	if c.closed || !until.After(time.Now()) {
		return net.ErrClosed
	}
	c.last = until
	return nil
}
func (c *recordedTCPRefresh) Close() error { c.closed = true; return c.Conn.Close() }

func TestSOCKSTCPAdapterClampsEveryRenewalIncludingZero(t *testing.T) {
	left, right := net.Pipe()
	defer func() { _ = right.Close() }()
	stream := &recordedTCPRefresh{Conn: left}
	defer func() { _ = stream.Close() }()
	limit := time.Now().Add(time.Second)
	adapter := &socksTCPLeaseConn{renewableSOCKSTCP: stream, limit: func() time.Time { return limit }}
	for _, requested := range []time.Time{{}, limit.Add(time.Second), limit.Add(-100 * time.Millisecond)} {
		if err := adapter.SetDeadline(requested); err != nil {
			t.Fatal(err)
		}
		want := requested
		if requested.IsZero() || limit.Before(requested) {
			want = limit
		}
		if !stream.last.Equal(want) {
			t.Fatalf("renewal=%v want=%v", stream.last, want)
		}
	}
	limit = limit.Add(-200 * time.Millisecond)
	if err := adapter.Refresh(time.Time{}); err != nil || !stream.last.Equal(limit) {
		t.Fatal("Refresh bypassed current authorization limit", err)
	}
	if err := adapter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := adapter.SetDeadline(time.Now().Add(time.Second)); !errors.Is(err, net.ErrClosed) {
		t.Fatal("closed connection revived", err)
	}
}

func TestSOCKSUpstreamTCPWithoutRenewalAndRevocationFailClosed(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(map[bool]string{false: "original expiry", true: "current policy denies"}[revoked], func(t *testing.T) {
			f := newSOCKSUpstreamFixture(t, nil, true)
			g, b := installFixtureSOCKSBridge(t, f)
			until := time.Now().Add(200 * time.Millisecond)
			g.udpMu.Lock()
			g.udpUntil = until
			g.udpMu.Unlock()
			if err := g.upstreamBridges[0].gate.Refresh(until); err != nil {
				t.Fatal(err)
			}
			c, err := dialOwnedTCP(context.Background(), b, "1.1.1.1:443")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = c.Close() }()
			if revoked {
				g.udpPolicy.Store(copyUDPPolicy(domain.Configuration{Protection: &domain.Protection{CIDRs: []string{"1.1.1.1/32"}}}))
				_, _ = c.Write([]byte("denied"))
			}
			assertPacketClosed(t, c, until.Add(300*time.Millisecond))
			if !revoked {
				if err := g.upstreamBridges[0].gate.Refresh(time.Now().Add(time.Second)); !errors.Is(err, net.ErrClosed) {
					t.Fatal("expired original gate revived", err)
				}
			}
			if f.targets.Load() != 1 {
				t.Fatal("unexpected source target")
			}
			g.close()
			if g.upstreamBridges[0].owned.activeCount() != 0 {
				t.Fatal("closed TCP connection remains owned")
			}
		})
	}
}

// The server presents only its leaf. Its earlier-expiring CA exists solely in
// RootCAs, so only the verified chain exposes the immutable certificate cap.
func tcpLeaseTLSFixture(t *testing.T, source string) (func(context.Context) (net.Conn, error), *tls.Config, time.Time) {
	t.Helper()
	now := time.Now()
	until := now.Add(2 * time.Second).Truncate(time.Second)
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "lease fixture CA"}, NotBefore: now.Add(-time.Minute), NotAfter: until, IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	parsedCA, err := x509.ParseCertificate(caDER)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Minute), IPAddresses: []net.IP{net.IPv4(127, 0, 0, 1)}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, KeyUsage: x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, leaf, parsedCA, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	l, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				upstream, err := net.Dial("tcp", source)
				if err != nil {
					return
				}
				defer func() { _ = upstream.Close() }()
				go func() { _, _ = io.Copy(upstream, conn); _ = upstream.Close() }()
				_, _ = io.Copy(conn, upstream)
			}()
		}
	}()
	pool := x509.NewCertPool()
	pool.AddCert(parsedCA)
	config := &tls.Config{RootCAs: pool, ServerName: "127.0.0.1", MinVersion: tls.VersionTLS12}
	connect := func(ctx context.Context) (net.Conn, error) {
		return (&tls.Dialer{Config: config}).DialContext(ctx, "tcp", l.Addr().String())
	}
	return connect, config, until
}

func TestSOCKSUpstreamTCPRenewalCannotPassVerifiedCertificateCap(t *testing.T) {
	f := newSOCKSUpstreamFixture(t, nil, true)
	connect, _, certificateUntil := tcpLeaseTLSFixture(t, f.address)
	states := make(chan tls.ConnectionState, 1)
	verifiedConnect := connect
	connect = func(ctx context.Context) (net.Conn, error) {
		conn, err := verifiedConnect(ctx)
		if err == nil {
			states <- fixtureValue[*tls.Conn](t, conn).ConnectionState()
		}
		return conn, err
	}
	lease := time.Now().Add(3 * time.Second)
	g, err := newEgressGuard(nil, lease)
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	g.udpUntil = lease
	node := map[string]any{"username": "upstream-user", "password": "short-secret", "tls": true}
	if err := g.installSOCKSBridge(node, lease, connect, netip.MustParseAddr("127.0.0.1"), func(context.Context, string, string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	b := &egressBackend{port: fixtureValue[int](t, node["port"]), user: fixtureValue[string](t, node["username"]), secret: fixtureValue[string](t, node["password"])}
	c, err := dialOwnedTCP(context.Background(), b, "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	state := <-states
	if len(state.PeerCertificates) != 1 || len(state.VerifiedChains) == 0 || len(state.VerifiedChains[0]) != 2 || !state.VerifiedChains[0][1].NotAfter.Equal(certificateUntil) {
		t.Fatal("fixture did not verify the RootCAs-only earlier-expiring CA")
	}
	for time.Until(certificateUntil) > 100*time.Millisecond {
		until := time.Now().Add(3 * time.Second)
		g.udpMu.Lock()
		g.udpUntil = until
		g.udpMu.Unlock()
		if err := g.upstreamBridges[0].gate.Refresh(until); err != nil {
			t.Fatal(err)
		}
		_ = c.SetDeadline(certificateUntil.Add(300 * time.Millisecond))
		_, err = c.Write([]byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.ReadFull(c, make([]byte, 1)); err != nil {
			t.Fatal(err)
		}
		time.Sleep(40 * time.Millisecond)
	}
	assertPacketClosed(t, c, certificateUntil.Add(300*time.Millisecond))
	if time.Now().Before(certificateUntil) {
		t.Fatal("stream closed before certificate cap")
	}
	g.close()
	if err := g.upstreamBridges[0].gate.Refresh(time.Now().Add(time.Second)); !errors.Is(err, net.ErrClosed) {
		t.Fatal("expired/closed TLS stream revived", err)
	}
}

func TestSOCKSTCPRequiresVerifiedTLSWhenRequested(t *testing.T) {
	left, right := net.Pipe()
	defer func() { _ = left.Close() }()
	defer func() { _ = right.Close() }()
	if _, err := socksTCPCertificateLimit(left, true); err == nil {
		t.Fatal("required TLS accepted plaintext")
	}
	secure := tls.Client(left, &tls.Config{InsecureSkipVerify: true}) // #nosec G402 -- negative test must reject an unverified TLS connection; no handshake or data is sent here.
	if _, err := socksTCPCertificateLimit(secure, true); err == nil {
		t.Fatal("unverified TLS accepted")
	}
	if until, err := socksTCPCertificateLimit(right, false); err != nil || !until.IsZero() {
		t.Fatal("plaintext source gained a TLS cap", until, err)
	}
	f := newSOCKSUpstreamFixture(t, nil, true)
	connect, config, _ := tcpLeaseTLSFixture(t, f.address)
	config.InsecureSkipVerify = true // A completed handshake still lacks trusted verification.
	unverified, err := connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unverified.Close() }()
	if _, err := socksTCPCertificateLimit(unverified, true); err == nil {
		t.Fatal("completed but unverified TLS was accepted")
	}
	if f.targets.Load() != 0 {
		t.Fatal("untrusted TLS sent a SOCKS target")
	}
}
