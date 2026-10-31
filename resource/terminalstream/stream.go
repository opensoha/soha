// Package terminalstream implements the bounded native PTY wire protocol. It
// owns no authorization, credential, lease, audit or provider policy.
package terminalstream

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

var (
	ErrInvalidFrame  = errors.New("invalid terminal frame")
	ErrInputLimit    = errors.New("terminal input limit")
	ErrOutputLimit   = errors.New("terminal output limit")
	ErrTransportLost = errors.New("terminal transport lost")
)

// Endpoint is one connection and never reconnects. Read/Write carry only PTY
// bytes; resize, EOF and final outcome are explicit protocol messages. Exactly
// one goroutine may Read and one may Write, as with the provider stream.
type Endpoint struct {
	conn                 *websocket.Conn
	ctx                  context.Context
	cancel               context.CancelCauseFunc
	server               bool
	reader               *io.PipeReader
	writer               *io.PipeWriter
	sizes                chan contractresource.NativeTerminalSize
	writeMu              sync.Mutex
	stateMu              sync.Mutex
	ready, eof, terminal bool
	result               *contractresource.PodTerminalResult
	inputBytes           atomic.Int64
	outputBytes          atomic.Int64
	inputStopped         atomic.Bool
	done                 chan struct{}
	readySignal          chan struct{}
	wg                   sync.WaitGroup
}

func NewServer(ctx context.Context, conn *websocket.Conn, size contractresource.NativeTerminalSize) *Endpoint {
	return newEndpoint(ctx, conn, true, &size)
}

func NewClient(ctx context.Context, conn *websocket.Conn) *Endpoint {
	return newEndpoint(ctx, conn, false, nil)
}

func newEndpoint(ctx context.Context, conn *websocket.Conn, server bool, initial *contractresource.NativeTerminalSize) *Endpoint {
	ctx, cancel := context.WithCancelCause(ctx)
	reader, writer := io.Pipe()
	endpoint := &Endpoint{conn: conn, ctx: ctx, cancel: cancel, server: server, reader: reader, writer: writer, sizes: make(chan contractresource.NativeTerminalSize, 1), done: make(chan struct{}), readySignal: make(chan struct{})}
	if initial != nil {
		endpoint.sizes <- *initial
	}
	conn.SetReadLimit(contractresource.NativeTerminalMaxFrameBytes)
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error { return conn.SetReadDeadline(time.Now().Add(45 * time.Second)) })
	endpoint.wg.Add(2)
	go endpoint.readLoop()
	go endpoint.heartbeat()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
		// Closing the writer unblocks both pipe directions. Leave the reader
		// open so a consumer that resumes after a verified exit still sees
		// EOF; closing both ends turns that normal EOF into ErrClosedPipe.
		_ = writer.CloseWithError(context.Cause(ctx))
		endpoint.wg.Wait()
		close(endpoint.done)
	}()
	return endpoint
}

func (endpoint *Endpoint) Context() context.Context      { return endpoint.ctx }
func (endpoint *Endpoint) Done() <-chan struct{}         { return endpoint.done }
func (endpoint *Endpoint) Err() error                    { return context.Cause(endpoint.ctx) }
func (endpoint *Endpoint) Close() error                  { endpoint.cancel(context.Canceled); return nil }
func (endpoint *Endpoint) Read(data []byte) (int, error) { return endpoint.reader.Read(data) }

// Input exposes a closeable reader without closing the WebSocket. The provider
// can end stdin on deadline while the server still sends its final outcome.
func (endpoint *Endpoint) Input() io.ReadCloser { return endpointInput{endpoint: endpoint} }

type endpointInput struct{ endpoint *Endpoint }

func (input endpointInput) Read(data []byte) (int, error) { return input.endpoint.reader.Read(data) }
func (input endpointInput) Close() error {
	input.endpoint.inputStopped.Store(true)
	return input.endpoint.reader.Close()
}

func (endpoint *Endpoint) WaitReady(ctx context.Context) error {
	select {
	case <-endpoint.readySignal:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-endpoint.ctx.Done():
		return context.Cause(endpoint.ctx)
	}
}

func (endpoint *Endpoint) Next(ctx context.Context) *contractresource.NativeTerminalSize {
	select {
	case size := <-endpoint.sizes:
		return &size
	case <-ctx.Done():
		return nil
	case <-endpoint.ctx.Done():
		return nil
	}
}

func (endpoint *Endpoint) Counts() (input, output int64) {
	return endpoint.inputBytes.Load(), endpoint.outputBytes.Load()
}

func (endpoint *Endpoint) Result() (contractresource.PodTerminalResult, bool) {
	endpoint.stateMu.Lock()
	defer endpoint.stateMu.Unlock()
	if endpoint.result == nil {
		return contractresource.PodTerminalResult{}, false
	}
	result := *endpoint.result
	if result.ExitCode != nil {
		code := *result.ExitCode
		result.ExitCode = &code
	}
	return result, true
}

func (endpoint *Endpoint) Write(data []byte) (int, error) {
	endpoint.writeMu.Lock()
	defer endpoint.writeMu.Unlock()
	if err := endpoint.canWriteData(); err != nil {
		return 0, err
	}
	counter, maximum, kind, limitErr := &endpoint.inputBytes, contractresource.NativePodTerminalMaxInputBytes, "input", ErrInputLimit
	if endpoint.server {
		counter, maximum, kind, limitErr = &endpoint.outputBytes, contractresource.NativePodTerminalMaxOutputBytes, "output", ErrOutputLimit
	}
	written := 0
	for len(data) > 0 {
		keep := min(len(data), contractresource.NativeTerminalOutputChunkBytes, int(maximum-counter.Load()))
		if keep <= 0 {
			endpoint.sendLimitLocked(kind)
			endpoint.cancel(limitErr)
			return written, limitErr
		}
		frame := contractresource.NativeTerminalFrame{Type: kind, Data: base64.StdEncoding.EncodeToString(data[:keep])}
		if err := endpoint.writeFrameLocked(frame); err != nil {
			return written, err
		}
		counter.Add(int64(keep))
		written += keep
		data = data[keep:]
	}
	return written, nil
}

func (endpoint *Endpoint) canWriteData() error {
	if endpoint.ctx.Err() != nil {
		return context.Cause(endpoint.ctx)
	}
	endpoint.stateMu.Lock()
	defer endpoint.stateMu.Unlock()
	if endpoint.terminal || !endpoint.server && endpoint.eof || !endpoint.ready {
		return ErrInvalidFrame
	}
	return nil
}

func (endpoint *Endpoint) SendReady() error {
	return endpoint.sendControl(contractresource.NativeTerminalFrame{Type: "ready"})
}
func (endpoint *Endpoint) SendEOF() error {
	return endpoint.sendControl(contractresource.NativeTerminalFrame{Type: "eof"})
}
func (endpoint *Endpoint) Resize(width, height uint16) error {
	return endpoint.sendControl(contractresource.NativeTerminalFrame{Type: "resize", Width: width, Height: height})
}
func (endpoint *Endpoint) SendExit(result contractresource.PodTerminalResult) error {
	return endpoint.sendControl(contractresource.NativeTerminalFrame{Type: "exit", Result: &result})
}
func (endpoint *Endpoint) SendError(code string) error {
	return endpoint.sendControl(contractresource.NativeTerminalFrame{Type: "error", Code: code})
}

func (endpoint *Endpoint) sendControl(frame contractresource.NativeTerminalFrame) error {
	if err := frame.Validate(!endpoint.server); err != nil {
		return ErrInvalidFrame
	}
	endpoint.writeMu.Lock()
	defer endpoint.writeMu.Unlock()
	endpoint.stateMu.Lock()
	err := endpoint.acceptFrameState(frame, false)
	endpoint.stateMu.Unlock()
	if err != nil {
		return err
	}
	return endpoint.writeFrameLocked(frame)
}

func (endpoint *Endpoint) writeFrameLocked(frame contractresource.NativeTerminalFrame) error {
	if endpoint.ctx.Err() != nil {
		return context.Cause(endpoint.ctx)
	}
	if err := endpoint.conn.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		endpoint.cancel(ErrTransportLost)
		return ErrTransportLost
	}
	if err := endpoint.conn.WriteJSON(frame); err != nil {
		endpoint.cancel(ErrTransportLost)
		return ErrTransportLost
	}
	return nil
}

func (endpoint *Endpoint) readLoop() {
	defer endpoint.wg.Done()
	defer func() {
		if endpoint.ctx.Err() == nil {
			endpoint.cancel(ErrTransportLost)
		}
	}()
	for {
		kind, data, err := endpoint.conn.ReadMessage()
		if err != nil {
			return
		}
		if kind != websocket.TextMessage {
			endpoint.cancel(ErrInvalidFrame)
			return
		}
		frame, err := DecodeFrame(data, endpoint.server)
		if err != nil {
			if endpoint.server {
				_ = endpoint.SendError("invalid_frame")
			}
			endpoint.cancel(ErrInvalidFrame)
			return
		}
		if err = endpoint.receive(frame); err != nil {
			if endpoint.server {
				if errors.Is(err, ErrInputLimit) {
					_ = endpoint.SendError("input_limit")
				} else {
					_ = endpoint.SendError("invalid_frame")
				}
			}
			endpoint.cancel(err)
			return
		}
		if frame.Type == "exit" {
			endpoint.cancel(io.EOF)
			return
		}
	}
}

// DecodeFrame rejects oversized, unknown and trailing fields before a frame
// reaches the state machine. Data is decoded strictly by the public contract.
func DecodeFrame(data []byte, fromClient bool) (contractresource.NativeTerminalFrame, error) {
	var frame contractresource.NativeTerminalFrame
	if len(data) > contractresource.NativeTerminalMaxFrameBytes {
		return frame, ErrInvalidFrame
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frame); err != nil {
		return frame, ErrInvalidFrame
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return frame, ErrInvalidFrame
	}
	if err := frame.Validate(fromClient); err != nil {
		return frame, ErrInvalidFrame
	}
	if err := validateWireFields(data, frame.Type); err != nil {
		return frame, ErrInvalidFrame
	}
	return frame, nil
}

func validateWireFields(data []byte, kind string) error {
	fields, err := wireObject(data)
	if err != nil {
		return err
	}
	allowed := map[string]string{"input": "type,data", "output": "type,data", "resize": "type,width,height", "eof": "type", "ready": "type", "exit": "type,result", "error": "type,code"}[kind]
	if err = exactWireFields(fields, allowed, allowed); err != nil {
		return err
	}
	if kind != "exit" {
		return nil
	}
	result, err := wireObject(fields["result"])
	if err != nil {
		return err
	}
	return exactWireFields(result, "status,exitCode,inputBytes,outputBytes,uidVerifiedBeforeStart,targetChanged,reasonCode", "status,inputBytes,outputBytes,uidVerifiedBeforeStart,targetChanged")
}

func wireObject(data []byte) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, ErrInvalidFrame
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || len(fields) > 8 {
			return nil, ErrInvalidFrame
		}
		if _, duplicate := fields[key]; duplicate {
			return nil, ErrInvalidFrame
		}
		var raw json.RawMessage
		if err = decoder.Decode(&raw); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, ErrInvalidFrame
		}
		fields[key] = raw
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidFrame
	}
	return fields, nil
}

func exactWireFields(fields map[string]json.RawMessage, allowed, required string) error {
	for key := range fields {
		if !strings.Contains(","+allowed+",", ","+key+",") {
			return ErrInvalidFrame
		}
	}
	for _, key := range strings.Split(required, ",") {
		if _, exists := fields[key]; !exists {
			return ErrInvalidFrame
		}
	}
	return nil
}

func (endpoint *Endpoint) receive(frame contractresource.NativeTerminalFrame) error {
	endpoint.stateMu.Lock()
	err := endpoint.acceptFrameState(frame, true)
	endpoint.stateMu.Unlock()
	if err != nil {
		return err
	}
	switch frame.Type {
	case "input", "output":
		return endpoint.receiveData(frame)
	case "resize":
		select {
		case <-endpoint.sizes:
		default:
		}
		select {
		case endpoint.sizes <- contractresource.NativeTerminalSize{Width: frame.Width, Height: frame.Height}:
		case <-endpoint.ctx.Done():
			return context.Cause(endpoint.ctx)
		}
	case "eof":
		return endpoint.writer.Close()
	case "exit":
		return endpoint.writer.Close()
	case "error":
		return fmt.Errorf("terminal peer: %s", frame.Code)
	}
	return nil
}

// stateMu is held by the caller. The incoming server side receives client
// frames; the incoming client side receives server frames.
func (endpoint *Endpoint) acceptFrameState(frame contractresource.NativeTerminalFrame, incoming bool) error {
	if endpoint.terminal {
		return ErrInvalidFrame
	}
	fromClient := endpoint.server == incoming
	if fromClient {
		if frame.Type == "input" && endpoint.eof {
			return ErrInvalidFrame
		}
		if frame.Type == "eof" {
			if endpoint.eof {
				return ErrInvalidFrame
			}
			endpoint.eof = true
		}
		return nil
	}
	if frame.Type == "ready" {
		if endpoint.ready {
			return ErrInvalidFrame
		}
		endpoint.ready = true
		close(endpoint.readySignal)
		return nil
	}
	if !endpoint.ready {
		return ErrInvalidFrame
	}
	if frame.Type == "exit" || frame.Type == "error" {
		endpoint.terminal = true
	}
	if incoming && frame.Type == "exit" {
		endpoint.result = frame.Result
	}
	return nil
}

func (endpoint *Endpoint) receiveData(frame contractresource.NativeTerminalFrame) error {
	data, _ := base64.StdEncoding.Strict().DecodeString(frame.Data)
	counter, maximum, limitErr := &endpoint.outputBytes, contractresource.NativePodTerminalMaxOutputBytes, ErrOutputLimit
	if endpoint.server {
		counter, maximum, limitErr = &endpoint.inputBytes, contractresource.NativePodTerminalMaxInputBytes, ErrInputLimit
	}
	if counter.Load()+int64(len(data)) > maximum {
		return limitErr
	}
	counter.Add(int64(len(data)))
	if endpoint.server && endpoint.inputStopped.Load() {
		return nil
	}
	_, err := endpoint.writer.Write(data)
	if endpoint.server && endpoint.inputStopped.Load() {
		return nil
	}
	return err
}

// writeMu is held here. Send a static outcome before cancellation closes the
// connection, without exposing any partially buffered provider error.
func (endpoint *Endpoint) sendLimitLocked(kind string) {
	if !endpoint.server {
		return
	}
	endpoint.stateMu.Lock()
	if !endpoint.ready || endpoint.terminal {
		endpoint.stateMu.Unlock()
		return
	}
	endpoint.terminal = true
	endpoint.stateMu.Unlock()
	_ = endpoint.writeFrameLocked(contractresource.NativeTerminalFrame{Type: "error", Code: kind + "_limit"})
}

func (endpoint *Endpoint) heartbeat() {
	defer endpoint.wg.Done()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-endpoint.ctx.Done():
			return
		case <-ticker.C:
			if err := endpoint.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				endpoint.cancel(ErrTransportLost)
				return
			}
		}
	}
}
