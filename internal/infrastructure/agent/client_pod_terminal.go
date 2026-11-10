package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (client *Client) CheckPodTerminal(ctx context.Context) error {
	var payload struct {
		Data contractresource.AgentPodExecutionCapabilities `json:"data"`
	}
	if err := client.request(ctx, http.MethodGet, podExecutionPath+"/capabilities", nil, &payload); err != nil {
		return err
	}
	item := payload.Data
	if !item.Ready || item.ProtocolVersion != contractresource.PodExecutionProtocolVersion || item.TerminalVersion != contractresource.PodTerminalProtocolVersion || item.MaxSessionSeconds < contractresource.NativePodTerminalMaxTimeout || item.MaxTerminalInputBytes < contractresource.NativePodTerminalMaxInputBytes || item.MaxTerminalOutputBytes < contractresource.NativePodTerminalMaxOutputBytes {
		return fmt.Errorf("%w: Agent lacks the native terminal protocol", apperrors.ErrUnsupportedOperation)
	}
	return nil
}

func (client *Client) StreamNativePodTerminal(ctx context.Context, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue) (contractresource.PodTerminalResult, error) {
	var empty contractresource.PodTerminalResult
	input, err := input.Normalized()
	if err != nil {
		return empty, apperrors.ErrInvalidArgument
	}
	if err = client.CheckPodTerminal(ctx); err != nil {
		return empty, err
	}
	deadline := time.Now().Add(time.Duration(input.TimeoutSeconds) * time.Second)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	query := url.Values{"namespace": {namespace}, "expectedUid": {input.ExpectedUID}, "container": {input.Container}, "shell": {input.Shell}, "width": {strconv.Itoa(int(input.Width))}, "height": {strconv.Itoa(int(input.Height))}, "timeoutSeconds": {strconv.FormatInt(input.TimeoutSeconds, 10)}, "deadlineAt": {deadline.UTC().Format(time.RFC3339Nano)}}
	endpoint, err := client.websocketEndpoint(podExecutionPath + "/pods/" + url.PathEscape(name) + "/terminal?" + query.Encode())
	if err != nil {
		return empty, apperrors.ErrClusterUnready
	}
	header := http.Header{"Authorization": []string{"Bearer " + client.token}}
	conn, response, err := client.wsDialer.DialContext(ctx, endpoint, header)
	if err != nil {
		return empty, nativeTerminalUpgradeError(response)
	}
	runCtx, cancel := context.WithCancel(ctx)
	stream := terminalstream.NewClient(runCtx, conn)
	var workers sync.WaitGroup
	defer func() { cancel(); _ = stream.Close(); _ = stdin.Close(); workers.Wait(); <-stream.Done() }()
	if stream.WaitReady(runCtx) != nil {
		return empty, apperrors.ErrClusterUnready
	}
	workers.Add(2)
	go copyNativeTerminalInput(runCtx, cancel, stream, stdin, &workers)
	go copyNativeTerminalSizes(runCtx, cancel, stream, sizes, &workers)
	written, _ := io.Copy(stdout, stream)
	result, known := stream.Result()
	inputBytes, _ := stream.Counts()
	if !known || result.Validate() != nil || result.OutputBytes != written || result.InputBytes > inputBytes {
		return empty, apperrors.ErrClusterUnready
	}
	return result, nil
}

func copyNativeTerminalInput(ctx context.Context, cancel context.CancelFunc, stream *terminalstream.Endpoint, stdin io.Reader, workers *sync.WaitGroup) {
	defer workers.Done()
	if _, err := io.Copy(stream, stdin); err != nil {
		if ctx.Err() == nil {
			cancel()
		}
		return
	}
	if err := stream.SendEOF(); err != nil && ctx.Err() == nil {
		cancel()
	}
}

func copyNativeTerminalSizes(ctx context.Context, cancel context.CancelFunc, stream *terminalstream.Endpoint, sizes contractresource.NativeTerminalSizeQueue, workers *sync.WaitGroup) {
	defer workers.Done()
	for {
		size := sizes.Next(ctx)
		if size == nil {
			return
		}
		if stream.Resize(size.Width, size.Height) != nil {
			cancel()
			return
		}
	}
}

func nativeTerminalUpgradeError(response *http.Response) error {
	if response == nil {
		return apperrors.ErrClusterUnready
	}
	if response.Body != nil {
		_ = response.Body.Close()
	}
	switch response.StatusCode {
	case 400:
		return apperrors.ErrInvalidArgument
	case 401, 403:
		return apperrors.ErrAccessDenied
	case 404, 405, 501:
		return apperrors.ErrUnsupportedOperation
	case 409, 410:
		return apperrors.ErrConflict
	default:
		return apperrors.ErrClusterUnready
	}
}
