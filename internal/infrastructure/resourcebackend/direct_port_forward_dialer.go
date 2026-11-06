package resourcebackend

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"time"

	"golang.org/x/net/http/httpguts"
	"k8s.io/apimachinery/pkg/util/httpstream"
	streamspdy "k8s.io/apimachinery/pkg/util/httpstream/spdy"
	"k8s.io/client-go/rest"
)

const directPortForwardHandshakeTimeout = 10 * time.Second

type directPortForwardDialer struct {
	ctx     context.Context
	client  *http.Client
	url     *url.URL
	timeout time.Duration
}

func newDirectPortForwardDialer(ctx context.Context, config *rest.Config, endpoint *url.URL) (*directPortForwardDialer, error) {
	config = rest.CopyConfig(config)
	config.NextProtos = []string{"http/1.1"}
	transport, err := rest.TransportFor(config)
	if err != nil {
		return nil, fmt.Errorf("build port-forward transport: %w", err)
	}
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return &directPortForwardDialer{ctx: ctx, client: client, url: endpoint, timeout: directPortForwardHandshakeTimeout}, nil
}

func (d *directPortForwardDialer) Dial(protocols ...string) (httpstream.Connection, string, error) {
	ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
	defer cancel()
	var conn net.Conn
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		GotConn: func(info httptrace.GotConnInfo) { conn = info.Conn },
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url.String(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set(httpstream.HeaderConnection, httpstream.HeaderUpgrade)
	req.Header.Set(httpstream.HeaderUpgrade, streamspdy.HeaderSpdy31)
	for _, protocol := range protocols {
		req.Header.Add(httpstream.HeaderProtocolVersion, protocol)
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("port-forward upgrade: %w", err)
	}
	body, upgraded := resp.Body.(io.ReadWriteCloser)
	if resp.StatusCode != http.StatusSwitchingProtocols || !upgraded || conn == nil ||
		!httpguts.HeaderValuesContainsToken(resp.Header.Values(httpstream.HeaderConnection), httpstream.HeaderUpgrade) ||
		!httpguts.HeaderValuesContainsToken(resp.Header.Values(httpstream.HeaderUpgrade), streamspdy.HeaderSpdy31) {
		_ = resp.Body.Close()
		return nil, "", fmt.Errorf("port-forward SPDY upgrade rejected (HTTP %d)", resp.StatusCode)
	}
	// net/http hands off the upgraded connection, including any already buffered data.
	stream, err := streamspdy.NewClientConnectionWithPings(upgradedPortForwardConn{Conn: conn, body: body}, 5*time.Second)
	if err != nil {
		_ = body.Close()
		return nil, "", err
	}
	return stream, resp.Header.Get(httpstream.HeaderProtocolVersion), nil
}

type upgradedPortForwardConn struct {
	net.Conn
	body io.ReadWriteCloser
}

func (c upgradedPortForwardConn) Read(p []byte) (int, error)  { return c.body.Read(p) }
func (c upgradedPortForwardConn) Write(p []byte) (int, error) { return c.body.Write(p) }
func (c upgradedPortForwardConn) Close() error                { return c.body.Close() }
