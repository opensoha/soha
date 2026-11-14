package sohacli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/gorilla/websocket"
	protocol "github.com/opensoha/soha/contracts/docker/runtime"
	"github.com/opensoha/soha/contracts/resource"
	"golang.org/x/term"
)

type dockerTerminalOptions struct {
	Action, Project, Service, CID, Shell, ID, Profile, Output string
	Timeout                                                   int64
	Yes                                                       bool
}

func runDockerNativeTerminal(ctx context.Context, args []string, rt Runtime) error {
	opt, err := parseDockerTerminalOptions(args, rt)
	if err != nil {
		return err
	}
	var in, out *os.File
	if opt.Action == "terminal" {
		in, _ = rt.In.(*os.File)
		out, _ = rt.Out.(*os.File)
		if in == nil || out == nil || !term.IsTerminal(int(in.Fd())) || !term.IsTerminal(int(out.Fd())) {
			return usageError{message: "Docker terminal requires interactive stdin and stdout"}
		}
	}
	client, err := loadComputeClient(ctx, rt, opt.Profile)
	if err != nil {
		return err
	}
	if strings.HasPrefix(client.Token, "soha_gpat_") || strings.HasPrefix(client.Token, "soha_gsat_") {
		return fmt.Errorf("governed_ai_unsupported: native terminal requires an ordinary user or explicitly permitted Key; no Docker REST request was sent")
	}
	if opt.Action != "terminal" {
		format, err := normalizeOutputFormat(opt.Output, "json", "yaml")
		if err != nil {
			return err
		}
		if opt.Action == "terminal-close" {
			if err := confirmWorkbenchAction(rt, opt.Yes, "Close Docker terminal "+terminalLabel(opt.ID)+"? Closing requests cancellation; remote process termination requires verification."); err != nil {
				return err
			}
		}
		item, err := client.readOrCloseDockerTerminal(ctx, opt, opt.Action == "terminal-close")
		if err != nil {
			return err
		}
		return writeStructuredOutput(rt.Out, format, item)
	}
	input, err := (protocol.TerminalInput{ExpectedContainerID: opt.CID, ServiceName: opt.Service, Shell: opt.Shell, TimeoutSeconds: opt.Timeout}).Normalized()
	if err != nil {
		return usageError{message: err.Error()}
	}
	width, height, err := term.GetSize(int(out.Fd()))
	if err != nil {
		return fmt.Errorf("cannot read terminal dimensions")
	}
	input.Width, input.Height = uint16(min(max(width, 1), resource.NativeTerminalMaxDimension)), uint16(min(max(height, 1), resource.NativeTerminalMaxDimension))
	if err := confirmWorkbenchAction(rt, opt.Yes, "Open "+input.Shell+" in Docker project "+terminalLabel(opt.Project)+" service "+terminalLabel(opt.Service)+" full container "+terminalLabel(opt.CID)+"?"); err != nil {
		return err
	}
	if rt.TerminalContext != nil {
		var cancel context.CancelFunc
		ctx, cancel = rt.TerminalContext(ctx)
		defer cancel()
	}
	created, err := client.createDockerTerminal(ctx, opt, input)
	if err != nil {
		return err
	}
	opt.ID = created.Session.ID
	return runNativeTerminal(ctx, rt, nativeTerminalPlan{ID: opt.ID, DeadlineAt: created.Session.DeadlineAt, Width: input.Width, Height: input.Height,
		Dial: func(ctx context.Context) (*websocket.Conn, error) {
			return client.dialDockerTerminal(ctx, opt, created)
		},
		Close: func(ctx context.Context) error {
			_, err := client.readOrCloseDockerTerminal(ctx, opt, true)
			return err
		}}, in, out)
}

func parseDockerTerminalOptions(args []string, rt Runtime) (dockerTerminalOptions, error) {
	var opt dockerTerminalOptions
	if len(args) == 0 || !slices.Contains([]string{"terminal", "terminal-session", "terminal-close"}, args[0]) {
		return opt, usageError{message: "provide a native Docker terminal action"}
	}
	opt.Action = args[0]
	leading, rest := extractLeadingPositionals(args[1:], 1)
	fs := newRuntimeFlagSet("docker projects "+opt.Action, rest, rt)
	fs.StringVar(&opt.Profile, "profile", "", "profile name")
	fs.StringVar(&opt.Service, "service", "", "registered Compose service name")
	fs.StringVar(&opt.CID, "expected-container-id", "", "reviewed full 64-character container ID")
	fs.StringVar(&opt.Shell, "shell", "/bin/sh", "PTY shell: /bin/sh or /bin/bash")
	fs.Int64Var(&opt.Timeout, "session-timeout-seconds", 300, "hard terminal lease duration: 1..900 seconds")
	fs.StringVar(&opt.ID, "session-id", "", "original terminal session UUID")
	fs.StringVar(&opt.Output, "output", "json", "metadata output: json or yaml")
	fs.BoolVar(&opt.Yes, "yes", false, "confirm the reviewed action; server permissions still apply")
	if err := fs.Parse(rest); err != nil {
		return opt, err
	}
	ids := append(leading, fs.Args()...)
	if len(ids) != 1 {
		return opt, usageError{message: "provide exactly one Docker project ID"}
	}
	opt.Project = ids[0]
	allowed := []string{"session-id"}
	outputSet := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "output" {
			outputSet = true
		}
	})
	switch opt.Action {
	case "terminal":
		allowed = []string{"service", "expected-container-id", "shell", "session-timeout-seconds", "yes"}
		if outputSet {
			return opt, usageError{message: "interactive terminal does not accept --output"}
		}
	case "terminal-close":
		allowed = append(allowed, "yes")
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return opt, err
	}
	if _, err := dockerTerminalPath(opt.Project, opt.ID); err != nil {
		return opt, err
	}
	if opt.Action == "terminal" {
		if opt.Timeout < 1 {
			return opt, usageError{message: "terminal timeout must be 1..900 seconds"}
		}
		if _, err := (protocol.TerminalInput{ExpectedContainerID: opt.CID, ServiceName: opt.Service, Shell: opt.Shell, TimeoutSeconds: opt.Timeout}).Normalized(); err != nil {
			return opt, usageError{message: err.Error()}
		}
	} else if opt.ID == "" {
		return opt, usageError{message: "provide the original --session-id"}
	}
	return opt, nil
}
