package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

var nativeDockerHTTPClient = &http.Client{Transport: nativeDockerTransport(), Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func nativeDockerTransport() *http.Transport {
	transport := dockerRuntimeHTTPTransport.Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 5 * time.Second
	return transport
}

func nativeDockerCapabilities(ctx context.Context, target boundReadTarget) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, dockerRuntimeURL(target.host.Endpoint, "/docker/runtime/v1/terminal/capabilities"), nil)
	if err != nil {
		return apperrors.ErrClusterUnready
	}
	request.Header.Set("Authorization", "Bearer "+target.token)
	response, err := nativeDockerHTTPClient.Do(request)
	if err != nil {
		return apperrors.ErrClusterUnready
	}
	defer func() { _ = response.Body.Close() }()
	switch response.StatusCode {
	case 403, 404, 405, 501:
		return apperrors.ErrUnsupportedOperation
	}
	if response.StatusCode != 200 {
		return apperrors.ErrClusterUnready
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return apperrors.ErrClusterUnready
	}
	var envelope dockerRuntimeItemResponse[protocol.TerminalCapabilities]
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF {
		return apperrors.ErrClusterUnready
	}
	caps := envelope.Data
	if caps.Protocol != protocol.TerminalProtocol || !caps.NativePTY || caps.MaxInputBytes != resource.NativePodTerminalMaxInputBytes || caps.MaxOutputBytes != resource.NativePodTerminalMaxOutputBytes || caps.MaxTimeoutSeconds != resource.NativePodTerminalMaxTimeout {
		return apperrors.ErrUnsupportedOperation
	}
	return nil
}

func streamNativeDockerTerminal(ctx context.Context, target boundReadTarget, input protocol.TerminalRequest, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	unknown := protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	raw, err := json.Marshal(input)
	if err != nil || len(raw) > 4096 {
		return unknown, apperrors.ErrInvalidArgument
	}
	header := http.Header{"Authorization": []string{"Bearer " + target.token}, protocol.TerminalInputHeader: []string{base64.RawURLEncoding.EncodeToString(raw)}}
	dialer := *websocket.DefaultDialer
	dialer.Proxy = nil
	dialer.HandshakeTimeout = 5 * time.Second
	dialer.NetDialContext = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	conn, response, err := dialer.DialContext(ctx, dockerRuntimeWebSocketURL(target.host.Endpoint, "/docker/runtime/v1/terminal/stream"), header)
	if err != nil && response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		return unknown, apperrors.ErrClusterUnready
	}
	defer func() { _ = conn.Close() }()
	local, stop := context.WithCancel(ctx)
	defer stop()
	peer := terminalstream.NewClient(local, conn)
	defer func() { _ = peer.Close() }()
	if err := peer.WaitReady(local); err != nil {
		return unknown, apperrors.ErrClusterUnready
	}
	return copyDockerTerminalPeer(local, stop, peer, stdin, stdout, sizes)
}

func copyDockerTerminalPeer(ctx context.Context, stop context.CancelFunc, peer *terminalstream.Endpoint, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	unknown := protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	var workers sync.WaitGroup
	type outputCopy struct {
		bytes int64
		err   error
	}
	outputDone := make(chan outputCopy, 1)
	workers.Go(func() {
		_, err := io.Copy(peer, stdin)
		if err == nil {
			_ = peer.SendEOF()
		} else {
			stop()
			_ = peer.Close()
		}
	})
	workers.Go(func() { n, err := io.Copy(stdout, peer); outputDone <- outputCopy{n, err} })
	workers.Go(func() {
		for {
			size := sizes.Next(ctx)
			if size == nil {
				return
			}
			if peer.Resize(size.Width, size.Height) != nil {
				return
			}
		}
	})
	var output outputCopy
	select {
	case output = <-outputDone:
	case <-ctx.Done():
		output.err = ctx.Err()
	}
	result, known := peer.Result()
	inputBytes, _ := peer.Counts()
	stop()
	_ = stdin.Close()
	_ = peer.Close()
	workers.Wait()
	if output.err != nil || !known || result.Validate() != nil || result.OutputBytes != output.bytes || result.InputBytes > inputBytes {
		return unknown, apperrors.ErrClusterUnready
	}
	return result, nil
}
