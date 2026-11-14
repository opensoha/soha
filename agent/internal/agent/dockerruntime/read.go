package dockerruntime

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"github.com/opensoha/soha/contracts/streamlimit"
)

// ReadLogs addresses the original full container ID. It never resolves a
// service name through Compose or falls back to a replacement container.
func ReadLogs(ctx context.Context, request protocol.ReadRequest) (protocol.ReadResult, error) {
	req, err := request.Normalized("logs")
	if err != nil {
		return protocol.ReadResult{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := InspectContainer(ctx, req.Container); err != nil {
		return protocol.ReadResult{}, err
	}
	args := []string{"container", "logs", "--timestamps", "--tail", strconv.Itoa(req.TailLines)}
	if req.SinceSeconds > 0 {
		args = append(args, "--since", strconv.FormatInt(req.SinceSeconds, 10)+"s")
	}
	args = append(args, req.Container.ContainerID)
	output := streamlimit.NewLimitedBuffer(int(req.LimitBytes) + 1)
	// #nosec G204 -- fixed executable; normalized requests supply a full container ID and bounded numeric options as separate arguments, without a shell.
	command := exec.CommandContext(ctx, "docker", args...)
	command.WaitDelay = 2 * time.Second
	command.Stdout, command.Stderr = output, output
	if command.Run() != nil {
		return protocol.ReadResult{}, errors.New("docker container logs could not be read")
	}
	content, truncated, err := boundedUTF8(output.String(), int(req.LimitBytes), output.Truncated())
	if err != nil {
		return protocol.ReadResult{}, err
	}
	result := newReadResult(req)
	result.Content, result.Encoding, result.SizeBytes = content, "utf-8", int64(len(content))
	result.ReturnedBytes = int64(len(content))
	result.Truncated, result.Complete = truncated, !truncated
	return result, nil
}

func boundedUTF8(value string, limit int, truncated bool) (string, bool, error) {
	if len(value) > limit {
		value, truncated = value[:limit], true
	}
	// A truncation may cut the final UTF-8 rune. Invalid bytes anywhere else
	// remain an error rather than being silently rewritten.
	if truncated {
		for len(value) > 0 && !utf8.ValidString(value) {
			start := len(value) - 1
			for start > 0 && !utf8.RuneStart(value[start]) {
				start--
			}
			if utf8.FullRuneInString(value[start:]) {
				break
			}
			value = value[:start]
		}
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return "", false, errors.New("docker read result is not UTF-8 text")
	}
	return value, truncated, nil
}

func newReadResult(req protocol.ReadRequest) protocol.ReadResult {
	return protocol.ReadResult{
		Protocol: protocol.ReadProtocol, Action: req.Action, ProjectID: req.ProjectID,
		Container: req.Container, Target: req.Target, Path: req.Path, Complete: true,
		Volumes: []protocol.Volume{}, Files: []protocol.FileEntry{},
	}
}
