package runtime

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"time"

	contractresource "github.com/opensoha/soha/contracts/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

var errTerminalInputLimit = errors.New("terminal input limit")
var errTerminalOutputLimit = errors.New("terminal output limit")

// StreamPodTerminal uses a single PTY with merged stdout/stderr. It never
// retries. Cancellation closes this transport; it does not prove that a remote
// process has terminated. Callers own stream authorization and durable leases.
func StreamPodTerminal(ctx context.Context, client kubernetes.Interface, config *rest.Config, namespace, name string, input contractresource.PodTerminalInput, stdin io.ReadCloser, stdout io.Writer, sizes contractresource.NativeTerminalSizeQueue, factory PodExecutorFactory) (contractresource.PodTerminalResult, error) {
	var result contractresource.PodTerminalResult
	input, err := input.Normalized()
	if err != nil {
		return result, apierrors.NewBadRequest(err.Error())
	}
	if stdin == nil || stdout == nil || sizes == nil {
		return result, apierrors.NewBadRequest("terminal streams are required")
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	stopInput := context.AfterFunc(runCtx, func() { _ = stdin.Close() })
	defer stopInput()
	target, err := InspectPodExecutionTarget(runCtx, client, namespace, name, input.ExpectedUID, input.Container)
	if err != nil {
		return result, err
	}
	if err = runCtx.Err(); err != nil {
		return result, err
	}
	options := corev1.PodExecOptions{Container: target.Container, Command: []string{input.Shell}, Stdin: true, Stdout: true, Stderr: false, TTY: true}
	if factory == nil {
		if config == nil || client.CoreV1().RESTClient() == nil {
			return result, apierrors.NewBadRequest("Pod terminal transport is unavailable")
		}
		factory = func(namespace, name string, options corev1.PodExecOptions) (remotecommand.Executor, error) {
			request := client.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
			request.VersionedParams(&options, scheme.ParameterCodec)
			return newPodExecutionExecutor(config, name, request.URL())
		}
	}
	executor, err := factory(namespace, name, options)
	if err != nil {
		return result, err
	}
	reader := &terminalInputReader{reader: stdin, cancel: cancel}
	writer := &terminalOutputWriter{writer: stdout, cancel: cancel}
	execErr := executor.StreamWithContext(runCtx, remotecommand.StreamOptions{Stdin: reader, Stdout: writer, Tty: true, TerminalSizeQueue: terminalSizeQueue{ctx: runCtx, source: sizes}})
	if apierrors.IsForbidden(execErr) || apierrors.IsUnauthorized(execErr) || apierrors.IsNotFound(execErr) || apierrors.IsBadRequest(execErr) {
		return result, execErr
	}
	result = terminalStreamResult(execErr, runCtx.Err(), reader, writer)
	if runCtx.Err() == nil {
		verifyCtx, verifyCancel := context.WithTimeout(runCtx, 3*time.Second)
		defer verifyCancel()
		_, verifyErr := InspectPodExecutionTarget(verifyCtx, client, namespace, name, input.ExpectedUID, target.Container)
		if verifyErr != nil {
			result.Status, result.ExitCode, result.ReasonCode = "unknown", nil, "target_verification_unavailable"
			if apierrors.IsConflict(verifyErr) || apierrors.IsNotFound(verifyErr) {
				result.TargetChanged = true
				result.ReasonCode = "target_changed"
			}
		}
	}
	return result, nil
}

func terminalStreamResult(execErr, ctxErr error, reader *terminalInputReader, writer *terminalOutputWriter) contractresource.PodTerminalResult {
	result := contractresource.PodTerminalResult{Status: "transport_lost", ReasonCode: "transport_lost", UIDVerifiedBeforeStart: true, InputBytes: reader.count.Load(), OutputBytes: writer.count.Load()}
	var remoteExit utilexec.ExitError
	switch {
	case reader.limited.Load():
		result.Status, result.ReasonCode = "input_limit", "input_limit"
	case writer.limited.Load():
		result.Status, result.ReasonCode = "output_limit", "output_limit"
	case errors.Is(ctxErr, context.DeadlineExceeded):
		result.Status, result.ReasonCode = "timed_out", "deadline_exceeded"
	case errors.Is(ctxErr, context.Canceled):
		result.Status, result.ReasonCode = "canceled", "request_canceled"
	case execErr == nil:
		code := int64(0)
		result.Status, result.ReasonCode, result.ExitCode = "succeeded", "", &code
	case errors.As(execErr, &remoteExit) && remoteExit.Exited() && remoteExit.ExitStatus() > 0 && int64(remoteExit.ExitStatus()) <= 4294967295:
		code := int64(remoteExit.ExitStatus())
		result.Status, result.ReasonCode, result.ExitCode = "failed", "process_exited", &code
	}
	return result
}

type terminalSizeQueue struct {
	ctx    context.Context
	source contractresource.NativeTerminalSizeQueue
}

func (queue terminalSizeQueue) Next() *remotecommand.TerminalSize {
	if queue.ctx.Err() != nil {
		return nil
	}
	size := queue.source.Next(queue.ctx)
	if size == nil || queue.ctx.Err() != nil || size.Width < 1 || size.Height < 1 || size.Width > contractresource.NativeTerminalMaxDimension || size.Height > contractresource.NativeTerminalMaxDimension {
		return nil
	}
	return &remotecommand.TerminalSize{Width: size.Width, Height: size.Height}
}

type terminalInputReader struct {
	reader  io.Reader
	cancel  context.CancelFunc
	count   atomic.Int64
	limited atomic.Bool
}

func (reader *terminalInputReader) Read(data []byte) (int, error) {
	if len(data) == 0 {
		return 0, nil
	}
	remaining := contractresource.NativePodTerminalMaxInputBytes - reader.count.Load()
	if int64(len(data)) > remaining {
		data = data[:remaining]
	}
	if len(data) == 0 {
		if remaining > 0 {
			return 0, nil
		}
		var probe [1]byte
		n, err := reader.reader.Read(probe[:])
		if n == 0 {
			return 0, err
		}
		reader.limited.Store(true)
		reader.cancel()
		return 0, errTerminalInputLimit
	}
	n, err := reader.reader.Read(data)
	reader.count.Add(int64(n))
	return n, err
}

type terminalOutputWriter struct {
	writer  io.Writer
	cancel  context.CancelFunc
	count   atomic.Int64
	limited atomic.Bool
}

func (writer *terminalOutputWriter) Write(data []byte) (int, error) {
	remaining := contractresource.NativePodTerminalMaxOutputBytes - writer.count.Load()
	keep := min(int64(len(data)), remaining)
	n, err := writer.writer.Write(data[:keep])
	writer.count.Add(int64(n))
	if err != nil {
		return n, err
	}
	if int64(n) != keep {
		return n, io.ErrShortWrite
	}
	if keep < int64(len(data)) {
		writer.limited.Store(true)
		writer.cancel()
		return n, errTerminalOutputLimit
	}
	return n, nil
}
