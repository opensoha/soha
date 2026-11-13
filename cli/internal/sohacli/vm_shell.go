package sohacli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"golang.org/x/term"
)

func runVMShell(ctx context.Context, rt Runtime, client APIClient, binding sohaapi.VirtualMachineAccessBinding) (result error) {
	cols, rows := 80, 24
	fd := -1
	if file, ok := rt.In.(*os.File); ok && term.IsTerminal(int(file.Fd())) {
		fd = int(file.Fd())
		if width, height, err := term.GetSize(fd); err == nil {
			cols, rows = width, height
		}
	}
	var envelope sohaapi.VirtualMachineSSHSessionEnvelope
	path := virtualizationPath + "/vms/" + url.PathEscape(binding.VMID) + "/ssh-sessions"
	if err := client.doJSON(ctx, http.MethodPost, path, client.Token, nil, sohaapi.VirtualMachineSSHSessionInput{BindingRevision: binding.Revision, Cols: cols, Rows: rows}, &envelope); err != nil {
		return err
	}
	path = virtualizationPath + "/ssh-sessions/" + url.PathEscape(envelope.Data.ID)
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := client.doJSON(cleanup, http.MethodDelete, path, client.Token, nil, nil, nil); err != nil {
			if result == nil {
				result = fmt.Errorf("session cleanup was not confirmed: %w", err)
			}
		}
	}()
	endpoint, err := url.Parse(client.ServerURL)
	if err != nil || endpoint.Host == "" || endpoint.User != nil || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return fmt.Errorf("invalid SSH control-plane endpoint")
	}
	if endpoint.Scheme == "https" {
		endpoint.Scheme = "wss"
	} else {
		endpoint.Scheme = "ws"
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + path + "/stream"
	endpoint.RawQuery, endpoint.Fragment = "", ""
	header := http.Header{"Authorization": {"Bearer " + client.effectiveToken(client.Token)}, "User-Agent": {userAgent}}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second, Proxy: http.ProxyFromEnvironment}
	if transport, ok := client.httpClient().Transport.(*http.Transport); ok {
		dialer.TLSClientConfig = transport.TLSClientConfig
		dialer.NetDialContext = transport.DialContext
	}
	var conn *websocket.Conn
	for attempt := 0; attempt < 2; attempt++ {
		var response *http.Response
		conn, response, err = dialer.DialContext(ctx, endpoint.String(), header)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		if err == nil || attempt > 0 || response == nil || response.StatusCode != http.StatusUnauthorized || !client.canRefresh() {
			break
		}
		token, refreshErr := client.refreshAfterUnauthorized(ctx)
		if refreshErr != nil {
			return refreshErr
		}
		header.Set("Authorization", "Bearer "+token)
	}
	if err != nil {
		return fmt.Errorf("open VM SSH stream: %w", err)
	}
	defer func() { _ = conn.Close() }()
	conn.SetReadLimit(128 << 10)
	if fd >= 0 {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return err
		}
		defer func() {
			if err := term.Restore(fd, state); err != nil {
				result = errors.Join(result, fmt.Errorf("restore terminal: %w", err))
			}
		}()
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(streamCtx, func() { _ = conn.Close() })
	defer stop()
	var writeMu sync.Mutex
	write := func(frame sohaapi.VirtualMachineSSHFrame) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(frame)
	}
	inputDone := make(chan error, 1)
	go func() {
		err := copyVMInput(streamCtx, rt.In, write)
		inputDone <- err
		if err != nil && streamCtx.Err() == nil {
			cancel()
		}
	}()
	resizeDone := make(chan struct{})
	go func() {
		defer close(resizeDone)
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-streamCtx.Done():
				return
			case <-ticker.C:
				if fd < 0 {
					continue
				}
				width, height, err := term.GetSize(fd)
				if err == nil && (width != cols || height != rows) && width > 0 && height > 0 {
					cols, rows = width, height
					if write(sohaapi.VirtualMachineSSHFrame{Type: "resize", Cols: cols, Rows: rows}) != nil {
						cancel()
						return
					}
				}
			}
		}
	}()
	defer func() {
		cancel()
		<-resizeDone
		select {
		case <-inputDone:
		case <-time.After(time.Second):
			if result == nil {
				result = fmt.Errorf("terminal input cancellation was not confirmed")
			}
		}
	}()
	for {
		var frame sohaapi.VirtualMachineSSHFrame
		if err := conn.ReadJSON(&frame); err != nil {
			return fmt.Errorf("SSH connection ended without a terminal receipt; effects may be unknown: %w", err)
		}
		switch frame.Type {
		case "output":
			data := []byte(frame.Data)
			if frame.DataEncoding == "base64" {
				data, err = base64.StdEncoding.DecodeString(frame.Data)
				if err != nil {
					return fmt.Errorf("invalid SSH output encoding")
				}
			} else if frame.DataEncoding != "" && frame.DataEncoding != "utf8" {
				return fmt.Errorf("invalid SSH output encoding")
			}
			if len(data) > 65536 {
				return fmt.Errorf("SSH output frame is too large")
			}
			if _, err := rt.Out.Write(data); err != nil {
				return err
			}
		case "close":
			if frame.ExitCode == nil {
				return fmt.Errorf("SSH shell exit status is unknown")
			}
			if *frame.ExitCode != 0 {
				return fmt.Errorf("remote shell exited with code %d", *frame.ExitCode)
			}
			return nil
		case "error":
			return fmt.Errorf("remote shell: %s", frame.Message)
		default:
			return fmt.Errorf("invalid SSH stream frame")
		}
	}
}

func copyVMInput(ctx context.Context, input io.Reader, write func(sohaapi.VirtualMachineSSHFrame) error) error {
	buffer := make([]byte, 16<<10)
	for ctx.Err() == nil {
		var n int
		var err error
		if file, ok := input.(*os.File); ok {
			n, err = readVMTerminalFile(ctx, file, buffer)
		} else {
			n, err = input.Read(buffer)
		}
		if n > 0 {
			if writeErr := write(sohaapi.VirtualMachineSSHFrame{Type: "input", Data: base64.StdEncoding.EncodeToString(buffer[:n]), DataEncoding: "base64"}); writeErr != nil {
				return writeErr
			}
		}
		if err != nil {
			if err == io.EOF {
				return write(sohaapi.VirtualMachineSSHFrame{Type: "close"})
			}
			return err
		}
	}
	return ctx.Err()
}
