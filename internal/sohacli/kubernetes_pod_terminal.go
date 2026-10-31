package sohacli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"slices"

	contractresource "github.com/opensoha/soha-contracts/resource"
	"golang.org/x/term"
)

type podTerminalOptions struct {
	Action, Name, Profile, Cluster, Namespace, UID, Container, Shell, ID, Output string
	Timeout                                                                      int64
	Yes                                                                          bool
}

func runKubernetesPodTerminal(ctx context.Context, args []string, rt Runtime) error {
	options, err := parsePodTerminalOptions(args, rt)
	if err != nil {
		return err
	}
	var inputFile, outputFile *os.File
	if options.Action == "terminal" {
		var inputOK, outputOK bool
		inputFile, inputOK = rt.In.(*os.File)
		outputFile, outputOK = rt.Out.(*os.File)
		if !inputOK || !outputOK || !term.IsTerminal(int(inputFile.Fd())) || !term.IsTerminal(int(outputFile.Fd())) {
			return usageError{message: "terminal requires interactive stdin and stdout; use k8s pods exec for non-interactive argv"}
		}
	}
	client, err := loadComputeClient(ctx, rt, options.Profile)
	if err != nil {
		return err
	}
	if options.Action != "terminal" {
		format, err := normalizeOutputFormat(options.Output, "json", "yaml")
		if err != nil {
			return err
		}
		if options.Action == "terminal-close" {
			if err := confirmWorkbenchAction(rt, options.Yes, "Close native Pod terminal "+terminalLabel(options.ID)+"? Closing requests cancellation; it does not prove remote process termination."); err != nil {
				return err
			}
		}
		item, err := client.readOrClosePodTerminal(ctx, options, options.Action == "terminal-close")
		if err != nil {
			return err
		}
		return writeStructuredOutput(rt.Out, format, item)
	}
	input, err := (contractresource.PodTerminalInput{ExpectedUID: options.UID, Container: options.Container, Shell: options.Shell, TimeoutSeconds: options.Timeout}).Normalized()
	if err != nil {
		return usageError{message: err.Error()}
	}
	width, height, err := term.GetSize(int(outputFile.Fd()))
	if err != nil {
		return fmt.Errorf("cannot read terminal dimensions")
	}
	input.Width, input.Height = uint16(min(max(width, 1), contractresource.NativeTerminalMaxDimension)), uint16(min(max(height, 1), contractresource.NativeTerminalMaxDimension))
	if err := confirmWorkbenchAction(rt, options.Yes, "Open "+input.Shell+" in Pod "+terminalLabel(options.Name)+" UID "+terminalLabel(options.UID)+" in cluster "+terminalLabel(options.Cluster)+" namespace "+terminalLabel(options.Namespace)+"?"); err != nil {
		return err
	}
	if rt.TerminalContext != nil {
		var cancel context.CancelFunc
		ctx, cancel = rt.TerminalContext(ctx)
		defer cancel()
	}
	return runNativePodTerminal(ctx, rt, client, options, input, inputFile, outputFile)
}

func parsePodTerminalOptions(args []string, rt Runtime) (podTerminalOptions, error) {
	var options podTerminalOptions
	if len(args) == 0 || !slices.Contains([]string{"terminal", "terminal-session", "terminal-close"}, args[0]) {
		return options, usageError{message: "pods requires terminal, terminal-session or terminal-close"}
	}
	options.Action = args[0]
	leading, rest := extractLeadingPositionals(args[1:], 1)
	fs := newRuntimeFlagSet("k8s pods "+options.Action, rest, rt)
	fs.StringVar(&options.Profile, "profile", "", "profile name")
	fs.StringVar(&options.Cluster, "cluster-id", "", "Soha cluster ID")
	fs.StringVar(&options.Namespace, "namespace", "default", "one Kubernetes namespace")
	fs.StringVar(&options.UID, "expected-uid", "", "UID of the reviewed Pod")
	fs.StringVar(&options.Container, "container", "", "container to freeze")
	fs.StringVar(&options.Shell, "shell", "/bin/sh", "PTY shell: /bin/sh or /bin/bash")
	fs.Int64Var(&options.Timeout, "session-timeout-seconds", 300, "hard terminal lease duration, 1 to 900 seconds")
	fs.StringVar(&options.ID, "session-id", "", "original native terminal session UUID")
	fs.StringVar(&options.Output, "output", "json", "metadata output: json or yaml; unavailable for interactive terminal")
	fs.BoolVar(&options.Yes, "yes", false, "confirm the reviewed action; server permissions still apply")
	if err := fs.Parse(rest); err != nil {
		return options, err
	}
	positionals := append(leading, fs.Args()...)
	if len(positionals) != 1 {
		return options, usageError{message: "provide exactly one Pod name"}
	}
	options.Name = positionals[0]
	allowed := []string{"cluster-id", "namespace", "session-id"}
	outputSet := false
	fs.Visit(func(value *flag.Flag) {
		if value.Name == "output" {
			outputSet = true
		}
	})
	switch options.Action {
	case "terminal":
		allowed = []string{"cluster-id", "namespace", "expected-uid", "container", "shell", "session-timeout-seconds", "yes"}
		if outputSet {
			return options, usageError{message: "interactive terminal does not accept --output"}
		}
	case "terminal-close":
		allowed = append(allowed, "yes")
	}
	if err := checkWorkbenchFlags(fs, allowed...); err != nil {
		return options, err
	}
	if _, err := kubernetesPodTerminalPath(options.Cluster, options.Namespace, options.Name, options.ID); err != nil {
		return options, err
	}
	if options.Action == "terminal" {
		if options.Timeout < 1 {
			return options, usageError{message: "terminal timeout must be from 1 to 900 seconds"}
		}
		if _, err := (contractresource.PodTerminalInput{ExpectedUID: options.UID, Container: options.Container, Shell: options.Shell, TimeoutSeconds: options.Timeout}).Normalized(); err != nil {
			return options, usageError{message: err.Error()}
		}
	} else if options.ID == "" {
		return options, usageError{message: "provide the original --session-id"}
	}
	return options, nil
}
