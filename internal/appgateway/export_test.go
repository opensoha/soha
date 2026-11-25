package appgateway

import "net/http"

func SetRuntimeTransportForTest(s *Server, transport http.RoundTripper) {
	s.runtimeTransport = transport
}
