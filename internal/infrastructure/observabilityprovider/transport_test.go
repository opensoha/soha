package observabilityprovider

import (
	"github.com/stretchr/testify/require"
	"net/http"
	"testing"
	"time"
)

type unsupportedTransport struct{}

func (unsupportedTransport) RoundTrip(*http.Request) (*http.Response, error) { panic("not used") }
func TestCloneHTTPTransportHandlesReplacedDefault(t *testing.T) {
	var typedNil *http.Transport
	for _, base := range []http.RoundTripper{nil, typedNil, unsupportedTransport{}} {
		cloned := cloneHTTPTransport(base)
		require.NotNil(t, cloned)
		require.Positive(t, cloned.TLSHandshakeTimeout)
		require.Positive(t, cloned.IdleConnTimeout)
	}
	original := &http.Transport{MaxIdleConns: 17, TLSHandshakeTimeout: 3 * time.Second}
	cloned := cloneHTTPTransport(original)
	require.NotSame(t, original, cloned)
	require.Equal(t, original.MaxIdleConns, cloned.MaxIdleConns)
	require.Equal(t, original.TLSHandshakeTimeout, cloned.TLSHandshakeTimeout)
	cloned.MaxIdleConns = 1
	require.Equal(t, 17, original.MaxIdleConns)
}
