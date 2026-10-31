package dockerruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/moby/moby/client"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
	"github.com/opensoha/soha-contracts/resource"
	"github.com/opensoha/soha-contracts/resource/terminalstream"
)

var errTerminalInputLimit = errors.New("terminal input limit")
var errTerminalOutputLimit = errors.New("terminal output limit")

func NativeTerminalCapabilities(ctx context.Context) (protocol.TerminalCapabilities, error) {
	engine, err := openNativeTerminalEngine(ctx)
	if err != nil {
		return protocol.TerminalCapabilities{}, err
	}
	defer func() { _ = engine.Close() }()
	return protocol.TerminalCapabilities{Protocol: protocol.TerminalProtocol, NativePTY: true, MaxTimeoutSeconds: resource.NativePodTerminalMaxTimeout, MaxInputBytes: resource.NativePodTerminalMaxInputBytes, MaxOutputBytes: resource.NativePodTerminalMaxOutputBytes}, nil
}

func openNativeTerminalEngine(ctx context.Context) (*terminalEngine, error) {
	engine, err := newTerminalEngine()
	if err != nil {
		return nil, errTerminalEngine
	}
	ready, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	if _, err := engine.client.Ping(ready, client.PingOptions{NegotiateAPIVersion: true}); err != nil {
		_ = engine.Close()
		return nil, errTerminalEngine
	}
	raw, err := engine.readTerminalJSON(ready, "info", "", 1<<20)
	var info struct {
		OSType string `json:"OSType"`
	}
	if err != nil || json.Unmarshal(raw, &info) != nil || info.OSType != "linux" {
		_ = engine.Close()
		return nil, errTerminalEngine
	}
	return engine, nil
}

// StreamNativeTerminal starts at most one actual Engine exec. Transport loss
// never calls ExecStart/ExecCreate again and never asserts process termination.
func StreamNativeTerminal(ctx context.Context, input protocol.TerminalRequest, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue) (protocol.TerminalResult, error) {
	input, err := input.Normalized(time.Now().UTC())
	if err != nil || stdin == nil || stdout == nil || sizes == nil {
		return protocol.TerminalResult{}, errTerminalInspection
	}
	leaseCtx, stop := context.WithDeadline(ctx, input.DeadlineAt)
	defer stop()
	engine, err := openNativeTerminalEngine(leaseCtx)
	if err != nil {
		return protocol.TerminalResult{}, err
	}
	defer func() { _ = engine.Close() }()
	result := protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost"}
	if err := engine.verifyTerminalContainer(leaseCtx, input.Container, true); err != nil {
		return result, nil
	}
	result.UIDVerifiedBeforeStart = true
	createCtx, stopCreate := context.WithTimeout(leaseCtx, 5*time.Second)
	exec, createErr := engine.client.ExecCreate(createCtx, input.Container.ContainerID, client.ExecCreateOptions{
		Cmd: []string{input.Input.Shell}, TTY: true, ConsoleSize: client.ConsoleSize{Height: uint(input.Input.Height), Width: uint(input.Input.Width)},
		AttachStdin: true, AttachStdout: true, AttachStderr: true, Env: []string{"TERM=xterm-256color"},
	})
	stopCreate()
	if createErr != nil || !terminalExecID.MatchString(exec.ID) {
		return result, nil
	}
	attachCtx, stopAttach := context.WithTimeout(leaseCtx, 5*time.Second)
	attached, attachErr := engine.client.ExecAttach(attachCtx, exec.ID, client.ExecAttachOptions{TTY: true, ConsoleSize: client.ConsoleSize{Height: uint(input.Input.Height), Width: uint(input.Input.Width)}})
	stopAttach()
	if attachErr != nil {
		return result, nil
	}
	defer attached.Close()
	return engine.copyNativeTerminal(leaseCtx, input, exec.ID, &attached, stdin, stdout, sizes, result), nil
}

type limitedTerminalWriter struct {
	writer   io.Writer
	limit    int64
	count    atomic.Int64
	limitErr error
}

func (writer *limitedTerminalWriter) Write(data []byte) (int, error) {
	keep := min(int64(len(data)), max(int64(0), writer.limit-writer.count.Load()))
	n, err := writer.writer.Write(data[:keep])
	writer.count.Add(int64(n))
	if err != nil {
		return n, err
	}
	if int64(len(data)) > keep {
		return n, writer.limitErr
	}
	if n != len(data) {
		return n, io.ErrShortWrite
	}
	return n, nil
}

func (engine *terminalEngine) copyNativeTerminal(ctx context.Context, input protocol.TerminalRequest, id string, attached *client.ExecAttachResult, stdin io.ReadCloser, stdout io.Writer, sizes resource.NativeTerminalSizeQueue, result protocol.TerminalResult) protocol.TerminalResult {
	runCtx, cancel := context.WithCancelCause(ctx)
	inputWriter := &limitedTerminalWriter{writer: attached.Conn, limit: resource.NativePodTerminalMaxInputBytes, limitErr: errTerminalInputLimit}
	outputWriter := &limitedTerminalWriter{writer: stdout, limit: resource.NativePodTerminalMaxOutputBytes, limitErr: errTerminalOutputLimit}
	// SDK attaches ctx cancellation only during the HTTP handshake. The owner
	// must close the returned hijacked connection and its own input reader.
	closed := make(chan struct{})
	stopClose := context.AfterFunc(runCtx, func() { attached.Close(); _ = stdin.Close(); close(closed) })
	defer func() {
		if stopClose() {
			attached.Close()
			_ = stdin.Close()
		} else {
			<-closed
		}
	}()
	var workers sync.WaitGroup
	outputDone := make(chan error, 1)
	workers.Go(func() {
		_, err := io.Copy(inputWriter, stdin)
		_ = attached.CloseWrite()
		if err != nil {
			cancel(err)
		}
	})
	workers.Go(func() { _, err := io.Copy(outputWriter, attached.Reader); outputDone <- err })
	workers.Go(func() { engine.resizeNativeTerminal(runCtx, cancel, id, sizes) })
	naturalEnd := false
	select {
	case err := <-outputDone:
		naturalEnd = err == nil && runCtx.Err() == nil
		if err != nil {
			cancel(err)
		}
	case <-runCtx.Done():
	}
	cause := context.Cause(runCtx)
	cancel(context.Canceled)
	workers.Wait()
	result.InputBytes, result.OutputBytes = inputWriter.count.Load(), outputWriter.count.Load()
	if !naturalEnd {
		return uncertainTerminalResult(result, cause)
	}
	return engine.finishOriginalTerminal(ctx, input, id, result)
}

func (engine *terminalEngine) resizeNativeTerminal(ctx context.Context, cancel context.CancelCauseFunc, id string, sizes resource.NativeTerminalSizeQueue) {
	for {
		size := sizes.Next(ctx)
		if size == nil {
			return
		}
		if size.Width == 0 || size.Height == 0 || size.Width > resource.NativeTerminalMaxDimension || size.Height > resource.NativeTerminalMaxDimension {
			cancel(errTerminalInspection)
			return
		}
		if _, err := engine.client.ExecResize(ctx, id, client.ExecResizeOptions{Width: uint(size.Width), Height: uint(size.Height)}); err != nil {
			cancel(errTerminalInspection)
			return
		}
	}
}

func uncertainTerminalResult(result protocol.TerminalResult, cause error) protocol.TerminalResult {
	switch {
	case errors.Is(cause, errTerminalInputLimit), errors.Is(cause, terminalstream.ErrInputLimit):
		result.Status, result.ReasonCode = "input_limit", "input_limit"
	case errors.Is(cause, errTerminalOutputLimit), errors.Is(cause, terminalstream.ErrOutputLimit):
		result.Status, result.ReasonCode = "output_limit", "output_limit"
	case errors.Is(cause, context.DeadlineExceeded):
		result.ReasonCode = "deadline_exceeded"
	case errors.Is(cause, context.Canceled):
		result.ReasonCode = "request_canceled"
	}
	return result
}

func (engine *terminalEngine) finishOriginalTerminal(ctx context.Context, input protocol.TerminalRequest, id string, result protocol.TerminalResult) protocol.TerminalResult {
	inspectCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	for inspectCtx.Err() == nil {
		item, err := engine.inspectOriginalExec(inspectCtx, id, input.Container.ContainerID)
		if err != nil {
			return result
		}
		if !item.Running && item.ExitCode != nil {
			if err := engine.verifyTerminalContainer(inspectCtx, input.Container, false); err != nil {
				result.ReasonCode = "target_verification_unavailable"
				return result
			}
			code := int64(*item.ExitCode)
			result.ExitCode, result.ReasonCode = &code, "process_exited"
			result.Status = "failed"
			if code == 0 {
				result.Status = "succeeded"
			}
			if result.Validate() != nil {
				return protocol.TerminalResult{Status: "unknown", ReasonCode: "transport_lost", UIDVerifiedBeforeStart: true, InputBytes: result.InputBytes, OutputBytes: result.OutputBytes}
			}
			return result
		}
		select {
		case <-inspectCtx.Done():
			return result
		case <-time.After(25 * time.Millisecond):
		}
	}
	return result
}
