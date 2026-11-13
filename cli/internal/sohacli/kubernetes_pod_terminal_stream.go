package sohacli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"github.com/opensoha/soha/contracts/resource/terminalstream"
	"golang.org/x/term"
)

func runNativePodTerminal(ctx context.Context, rt Runtime, client APIClient, options podTerminalOptions, input contractresource.PodTerminalInput, inputFile, outputFile *os.File) (result error) {
	created, err := client.createPodTerminal(ctx, options, input)
	if err != nil {
		return err
	}
	options.ID = created.Session.ID
	return runNativeTerminal(ctx, rt, nativeTerminalPlan{ID: options.ID, DeadlineAt: created.Session.DeadlineAt, Width: input.Width, Height: input.Height,
		Dial: func(ctx context.Context) (*websocket.Conn, error) {
			return client.dialPodTerminal(ctx, options, created)
		},
		Close: func(ctx context.Context) error {
			_, err := client.readOrClosePodTerminal(ctx, options, true)
			return err
		}}, inputFile, outputFile)
}

type nativeTerminalPlan struct {
	ID            string
	DeadlineAt    time.Time
	Width, Height uint16
	Dial          func(context.Context) (*websocket.Conn, error)
	Close         func(context.Context) error
}

func runNativeTerminal(ctx context.Context, rt Runtime, plan nativeTerminalPlan, inputFile, outputFile *os.File) (result error) {
	var err error
	known := false
	defer func() {
		if known {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := plan.Close(cleanup); err != nil {
			result = errors.Join(result, fmt.Errorf("terminal close was not confirmed; query the original --session-id %s", plan.ID))
		}
	}()
	if _, err = fmt.Fprintf(rt.Err, "terminal session %s; Ctrl-] disconnects locally; remote process termination requires verification\n", plan.ID); err != nil {
		return err
	}
	ctx, cancel := context.WithDeadline(ctx, plan.DeadlineAt)
	defer cancel()
	conn, err := plan.Dial(ctx)
	if err != nil {
		return err
	}
	stream := terminalstream.NewClient(ctx, conn)
	// Endpoint.Close only cancels local readers; the remote close is verified by
	// the independent plan.Close cleanup above.
	defer func() { _ = stream.Close() }()
	if err = stream.WaitReady(ctx); err != nil {
		return fmt.Errorf("terminal did not become ready; query the original --session-id %s", plan.ID)
	}
	state, err := term.MakeRaw(int(inputFile.Fd()))
	if err != nil {
		return fmt.Errorf("cannot enter raw terminal mode")
	}
	defer func() {
		if err := term.Restore(int(inputFile.Fd()), state); err != nil {
			result = errors.Join(result, fmt.Errorf("restore terminal failed"))
		}
	}()
	outcome, err := streamNativeTerminal(ctx, rt, stream, inputFile, outputFile, plan.Width, plan.Height)
	if err != nil {
		return fmt.Errorf("terminal ended without a verified final outcome; query the original --session-id %s: %w", plan.ID, err)
	}
	known = true
	switch outcome.Status {
	case "succeeded":
		return nil
	case "failed":
		return remoteProcessExitError{code: *outcome.ExitCode}
	default:
		return fmt.Errorf("terminal ended with %s; remote effects require verification", outcome.Status)
	}
}

func streamNativeTerminal(ctx context.Context, rt Runtime, stream *terminalstream.Endpoint, inputFile, outputFile *os.File, width, height uint16) (contractresource.PodTerminalResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		if copyNativeTerminalInput(ctx, stream, inputFile) != nil {
			cancel()
			_ = stream.Close()
		}
	}()
	go func() {
		defer workers.Done()
		resizeNativeTerminal(ctx, stream, outputFile, int(width), int(height))
	}()
	defer func() { cancel(); _ = stream.Close(); workers.Wait() }()
	written, copyErr := io.Copy(rt.Out, stream)
	result, known := stream.Result()
	inputBytes, _ := stream.Counts()
	if copyErr != nil || !known || result.Validate() != nil || result.OutputBytes != written || result.InputBytes > inputBytes {
		if ctx.Err() != nil {
			return contractresource.PodTerminalResult{}, ctx.Err()
		}
		return contractresource.PodTerminalResult{}, fmt.Errorf("no valid terminal receipt")
	}
	return result, nil
}

func copyNativeTerminalInput(ctx context.Context, stream *terminalstream.Endpoint, file *os.File) error {
	buffer := make([]byte, 16<<10)
	for ctx.Err() == nil {
		n, err := readVMTerminalFile(ctx, file, buffer)
		data := buffer[:n]
		escape := bytes.IndexByte(data, 0x1d)
		if escape >= 0 {
			data = data[:escape]
		}
		if len(data) > 0 {
			if _, writeErr := stream.Write(data); writeErr != nil {
				return writeErr
			}
		}
		if escape >= 0 {
			return context.Canceled
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return stream.SendEOF()
			}
			return err
		}
	}
	return ctx.Err()
}

func resizeNativeTerminal(ctx context.Context, stream *terminalstream.Endpoint, file *os.File, width, height int) {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cols, rows, err := term.GetSize(int(file.Fd()))
			if err != nil || cols < 1 || rows < 1 {
				continue
			}
			cols, rows = min(cols, contractresource.NativeTerminalMaxDimension), min(rows, contractresource.NativeTerminalMaxDimension)
			if cols != width || rows != height {
				if stream.Resize(uint16(cols), uint16(rows)) != nil {
					_ = stream.Close()
					return
				}
				width, height = cols, rows
			}
		}
	}
}
