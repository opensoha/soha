package networkproxyruntime

import (
	"context"
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"time"
)

type ingressHTTPPoint struct {
	seen bool
	ms   int64
}
type ingressHTTPSnapshot struct {
	getConn, gotConn, dnsStart, dnsDone, tlsStart, tlsDone, wrote, firstByte ingressHTTPPoint
	reused                                                                   bool
}
type ingressHTTPTrace struct {
	mu    sync.Mutex
	start time.Time
	data  ingressHTTPSnapshot
}
type ingressHTTPError struct {
	cause error
	trace ingressHTTPSnapshot
}

func (e *ingressHTTPError) Error() string    { return e.cause.Error() }
func (e *ingressHTTPError) Unwrap() error    { return e.cause }
func newIngressHTTPTrace() *ingressHTTPTrace { return &ingressHTTPTrace{start: time.Now()} }
func (t *ingressHTTPTrace) mark(p *ingressHTTPPoint) {
	t.mu.Lock()
	defer t.mu.Unlock()
	*p = ingressHTTPPoint{true, time.Since(t.start).Milliseconds()}
}
func (t *ingressHTTPTrace) context(ctx context.Context) context.Context {
	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GetConn: func(string) { t.mark(&t.data.getConn) },
		GotConn: func(i httptrace.GotConnInfo) {
			t.mu.Lock()
			t.data.gotConn = ingressHTTPPoint{true, time.Since(t.start).Milliseconds()}
			t.data.reused = i.Reused
			t.mu.Unlock()
		},
		DNSStart: func(httptrace.DNSStartInfo) { t.mark(&t.data.dnsStart) }, DNSDone: func(httptrace.DNSDoneInfo) { t.mark(&t.data.dnsDone) },
		TLSHandshakeStart: func() { t.mark(&t.data.tlsStart) }, TLSHandshakeDone: func(tls.ConnectionState, error) { t.mark(&t.data.tlsDone) },
		WroteRequest: func(httptrace.WroteRequestInfo) { t.mark(&t.data.wrote) }, GotFirstResponseByte: func() { t.mark(&t.data.firstByte) },
	})
}
func (t *ingressHTTPTrace) wrap(err error) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return &ingressHTTPError{cause: err, trace: t.data}
}
func (s ingressHTTPSnapshot) fields() []any {
	fields := []any{"httpConnReused", s.reused}
	for _, p := range []struct {
		name  string
		point ingressHTTPPoint
	}{{"httpGetConnMs", s.getConn}, {"httpGotConnMs", s.gotConn}, {"httpDNSStartMs", s.dnsStart}, {"httpDNSDoneMs", s.dnsDone}, {"httpTLSStartMs", s.tlsStart}, {"httpTLSDoneMs", s.tlsDone}, {"httpWroteRequestMs", s.wrote}, {"httpFirstResponseByteMs", s.firstByte}} {
		if p.point.seen {
			fields = append(fields, p.name, p.point.ms)
		}
	}
	return fields
}
