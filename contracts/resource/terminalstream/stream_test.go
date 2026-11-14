package terminalstream

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha/contracts/resource"
)

func terminalPair(t *testing.T) (*Endpoint, *Endpoint) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	upgraded := make(chan *Endpoint, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		endpoint := NewServer(ctx, conn, contractresource.NativeTerminalSize{Width: 80, Height: 24})
		upgraded <- endpoint
		<-endpoint.Done()
	}))
	conn, response, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	if err != nil {
		cancel()
		server.Close()
		t.Fatal(err)
	}
	client := NewClient(ctx, conn)
	var backend *Endpoint
	select {
	case backend = <-upgraded:
	case <-ctx.Done():
		t.Fatal("upgrade missing")
	}
	t.Cleanup(func() {
		_ = client.Close()
		_ = backend.Close()
		cancel()
		<-client.Done()
		<-backend.Done()
		server.Close()
	})
	return client, backend
}

func TestTerminalWireRoundtripEOFResizeAndKnownExit(t *testing.T) {
	client, backend := terminalPair(t)
	if err := backend.SendReady(); err != nil {
		t.Fatal(err)
	}
	if err := client.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	input := []byte{0, 1, 255, '\n'}
	inputDone := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(backend); inputDone <- data }()
	if _, err := client.Write(input); err != nil {
		t.Fatal(err)
	}
	if err := client.SendEOF(); err != nil {
		t.Fatal(err)
	}
	if got := <-inputDone; !bytes.Equal(input, got) {
		t.Fatalf("input changed: %v", got)
	}
	if _, err := client.Write([]byte("late")); !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("input after EOF: %v", err)
	}
	if err := client.Resize(120, 40); err != nil {
		t.Fatal(err)
	}
	var size *contractresource.NativeTerminalSize
	for size == nil || size.Width != 120 {
		size = backend.Next(t.Context())
		if size == nil {
			t.Fatal("resize missing")
		}
	}
	output := []byte{255, 0, 27, '[', 'm'}
	outputDone := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(client); outputDone <- data }()
	if _, err := backend.Write(output); err != nil {
		t.Fatalf("output after input EOF: %v", err)
	}
	code := int64(42)
	result := contractresource.PodTerminalResult{Status: "failed", ExitCode: &code, InputBytes: 4, OutputBytes: 5, UIDVerifiedBeforeStart: true, ReasonCode: "process_exited"}
	if err := backend.SendExit(result); err != nil {
		t.Fatal(err)
	}
	if got := <-outputDone; !bytes.Equal(got, output) {
		t.Fatalf("output changed: %v", got)
	}
	<-client.Done()
	got, ok := client.Result()
	if !ok || got.ExitCode == nil || *got.ExitCode != 42 {
		t.Fatalf("lost result: %+v", got)
	}
	<-backend.Done()
	if n, err := backend.Write([]byte("late")); n != 0 || !errors.Is(err, ErrInvalidFrame) {
		t.Fatalf("output after exit: %d, %v", n, err)
	}
}

func TestTerminalNonterminalClosePreservesCancellation(t *testing.T) {
	client, _ := terminalPair(t)
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	<-client.Done()
	if n, err := client.Write([]byte("late")); n != 0 || !errors.Is(err, context.Canceled) {
		t.Fatalf("write after cancellation: %d, %v", n, err)
	}
}

func TestTerminalExitCleanupPreservesEOFForDelayedConsumer(t *testing.T) {
	client, backend := terminalPair(t)
	if err := backend.SendReady(); err != nil {
		t.Fatal(err)
	}
	if err := client.WaitReady(t.Context()); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := backend.SendExit(contractresource.PodTerminalResult{Status: "succeeded", ExitCode: &zero, UIDVerifiedBeforeStart: true}); err != nil {
		t.Fatal(err)
	}
	// Force transport cleanup to finish before the caller resumes io.Copy.
	// Closing the reader as well as the writer changes a real EOF into
	// ErrClosedPipe, which must never invalidate an already verified exit.
	<-client.Done()
	var buffer [1]byte
	if n, err := client.Read(buffer[:]); n != 0 || err != io.EOF {
		t.Fatalf("normal terminal exit lost EOF after cleanup: n=%d err=%v", n, err)
	}
	result, known := client.Result()
	if !known || result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatal("verified result lost", result)
	}
}

func TestTerminalWireRejectsMalformedAndAmbiguousFrames(t *testing.T) {
	for name, data := range map[string]string{
		"oversize":             strings.Repeat(" ", contractresource.NativeTerminalMaxFrameBytes+1),
		"duplicate":            `{"type":"input","type":"eof"}`,
		"null-extra":           `{"type":"ready","result":null}`,
		"zero-extra":           `{"type":"ready","width":0}`,
		"unknown":              `{"type":"input","data":"AA==","bearer":"private"}`,
		"trailing":             `{"type":"eof"}{}`,
		"missing-result-field": `{"type":"exit","result":{"status":"unknown"}}`,
		"null-result-field":    `{"type":"exit","result":{"status":"unknown","inputBytes":null,"outputBytes":0,"uidVerifiedBeforeStart":false,"targetChanged":false}}`,
		"nested-duplicate":     `{"type":"exit","result":{"status":"unknown","status":"canceled","inputBytes":0,"outputBytes":0,"uidVerifiedBeforeStart":false,"targetChanged":false}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeFrame([]byte(data), strings.Contains(data, "input") || strings.Contains(data, "eof")); err == nil {
				t.Fatal("accepted malformed frame")
			}
		})
	}
}

func TestTerminalWireRejectsOutOfOrderAndInputAfterEOF(t *testing.T) {
	for _, scenario := range []string{"output-before-ready", "double-ready", "input-after-eof", "binary-frame"} {
		t.Run(scenario, func(t *testing.T) {
			client, backend := terminalPair(t)
			switch scenario {
			case "output-before-ready":
				_ = backend.conn.WriteJSON(contractresource.NativeTerminalFrame{Type: "output", Data: "AA=="})
			case "double-ready":
				_ = backend.SendReady()
				_ = backend.conn.WriteJSON(contractresource.NativeTerminalFrame{Type: "ready"})
			case "input-after-eof":
				_ = backend.SendReady()
				_ = client.WaitReady(t.Context())
				_ = client.SendEOF()
				_ = client.conn.WriteJSON(contractresource.NativeTerminalFrame{Type: "input", Data: "AA=="})
			case "binary-frame":
				_ = client.conn.WriteMessage(websocket.BinaryMessage, []byte(`{"type":"eof"}`))
			}
			target := client
			if scenario == "input-after-eof" || scenario == "binary-frame" {
				target = backend
			}
			select {
			case <-target.Done():
			case <-time.After(time.Second):
				t.Fatal("invalid stream remained active")
			}
			if !errors.Is(target.Err(), ErrInvalidFrame) {
				t.Fatalf("wrong category: %v", target.Err())
			}
		})
	}
}

func TestTerminalWireAggregateLimitsAndCancelUnblocksIO(t *testing.T) {
	client, backend := terminalPair(t)
	_ = backend.SendReady()
	_ = client.WaitReady(t.Context())
	go func() { _, _ = io.Copy(io.Discard, backend) }()
	data := bytes.Repeat([]byte{'a'}, int(contractresource.NativePodTerminalMaxInputBytes))
	if n, err := client.Write(data); err != nil || n != len(data) {
		t.Fatalf("limit boundary n=%d err=%v", n, err)
	}
	if _, err := client.Write([]byte("x")); !errors.Is(err, ErrInputLimit) {
		t.Fatalf("limit not enforced: %v", err)
	}
	<-client.Done()
	input, _ := client.Counts()
	if input != contractresource.NativePodTerminalMaxInputBytes {
		t.Fatalf("input counter=%d", input)
	}
	if _, known := client.Result(); known {
		t.Fatal("disconnect invented an exit result")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if size := backend.Next(ctx); size != nil && size.Width != 80 {
		t.Fatalf("unexpected size: %+v", size)
	}
}

func TestTerminalRawPeerCannotExceedAggregateInput(t *testing.T) {
	client, backend := terminalPair(t)
	_ = backend.SendReady()
	_ = client.WaitReady(t.Context())
	go func() { _, _ = io.Copy(io.Discard, backend) }()
	chunk := base64.StdEncoding.EncodeToString(make([]byte, contractresource.NativeTerminalMaxChunkBytes))
	for n := 0; n < 17; n++ {
		if err := client.conn.WriteJSON(contractresource.NativeTerminalFrame{Type: "input", Data: chunk}); err != nil {
			t.Fatal(err)
		}
	}
	<-backend.Done()
	if !errors.Is(backend.Err(), ErrInputLimit) {
		t.Fatalf("raw peer exceeded budget: %v", backend.Err())
	}
}

func TestTerminalProviderClosingStdinStillAllowsFinalOutputAndExit(t *testing.T) {
	client, backend := terminalPair(t)
	_ = backend.SendReady()
	_ = client.WaitReady(t.Context())
	if err := backend.Input().Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := client.Write([]byte("late input after process exit")); err != nil {
		t.Fatal(err)
	}
	outputDone := make(chan []byte, 1)
	go func() { data, _ := io.ReadAll(client); outputDone <- data }()
	if _, err := backend.Write([]byte("done")); err != nil {
		t.Fatal(err)
	}
	zero := int64(0)
	if err := backend.SendExit(contractresource.PodTerminalResult{Status: "succeeded", ExitCode: &zero, OutputBytes: 4, UIDVerifiedBeforeStart: true}); err != nil {
		t.Fatal(err)
	}
	if data := <-outputDone; string(data) != "done" {
		t.Fatalf("output=%q", data)
	}
	if _, known := client.Result(); !known {
		t.Fatal("closing provider stdin lost final outcome")
	}
}
