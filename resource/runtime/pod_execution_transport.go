package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"
)

func newPodExecutionExecutor(config *rest.Config, name string, target *url.URL) (remotecommand.Executor, error) {
	transport, upgrader, err := spdy.RoundTripperFor(config)
	if err != nil {
		return nil, err
	}
	status := &atomic.Int32{}
	executor, err := remotecommand.NewSPDYExecutorRejectRedirects(podExecutionStatusTransport{base: transport, status: status}, upgrader, http.MethodPost, target)
	if err != nil {
		return nil, err
	}
	return podExecutionStatusExecutor{Executor: executor, status: status, name: name}, nil
}

type podExecutionStatusExecutor struct {
	remotecommand.Executor
	status *atomic.Int32
	name   string
}

func (executor podExecutionStatusExecutor) Stream(options remotecommand.StreamOptions) error {
	return executor.StreamWithContext(context.Background(), options)
}

func (executor podExecutionStatusExecutor) StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error {
	err := executor.Executor.StreamWithContext(ctx, options)
	// Negotiate also stringifies transport errors. Keep the HTTP category in
	// this attempt's private state, rather than parsing a provider message.
	if code := executor.status.Load(); code != 0 {
		return apierrors.NewGenericServerResponse(int(code), http.MethodPost, schema.GroupResource{Resource: "pods/exec"}, executor.name, "Pod exec upgrade rejected", 0, false)
	}
	return err
}

type podExecutionStatusTransport struct {
	base   http.RoundTripper
	status *atomic.Int32
}

func (transport podExecutionStatusTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	if validPodExecutionUpgrade(response) {
		return response, nil
	}
	// Never read the raw error body: it can be unbounded or contain command
	// secrets. The original transport still applies authentication and TLS.
	if response.Body != nil {
		_ = response.Body.Close()
	}
	code := response.StatusCode
	if code < http.StatusBadRequest {
		code = http.StatusBadGateway
	}
	transport.status.Store(int32(code))
	return nil, fmt.Errorf("pod exec HTTP upgrade rejected")
}

func validPodExecutionUpgrade(response *http.Response) bool {
	return response.StatusCode == http.StatusSwitchingProtocols && strings.Contains(strings.ToLower(response.Header.Get("Connection")), "upgrade") && strings.Contains(strings.ToLower(response.Header.Get("Upgrade")), "spdy/3.1")
}
