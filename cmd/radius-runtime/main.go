// radius-runtime is an independently installed, Linux-only FreeRADIUS host.
// The Core server never starts this process or inherits its runtime privileges.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	runtime "github.com/opensoha/soha/internal/radiusruntime"
)

type configuration struct {
	Installation runtime.Installation        `json:"installation"`
	Control      runtime.ClientConfiguration `json:"control"`
}

func load(path string) (configuration, error) {
	if err := runtime.ApprovedRootFile(path); err != nil {
		return configuration{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return configuration{}, fmt.Errorf("open approved runtime configuration")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Size() > 32768 {
		return configuration{}, runtime.ErrDenied
	}
	return decodeConfiguration(file)
}
func decodeConfiguration(reader io.Reader) (configuration, error) {
	decoder := json.NewDecoder(io.LimitReader(reader, 32769))
	decoder.DisallowUnknownFields()
	var c configuration
	if err := decoder.Decode(&c); err != nil {
		return configuration{}, runtime.ErrInvalid
	}
	if decoder.Decode(new(any)) != io.EOF {
		return configuration{}, runtime.ErrInvalid
	}
	approved, err := c.Installation.ApproveControl(c.Control)
	if err != nil {
		return configuration{}, err
	}
	c.Control = approved
	return c, nil
}
func run(ctx context.Context, path string) error {
	config, err := load(path)
	if err != nil {
		return err
	}
	client, err := runtime.NewControlClient(config.Control)
	if err != nil {
		return err
	}
	defer client.Close()
	supervisor, err := runtime.NewSupervisor(config.Installation, client, client)
	if err != nil {
		return err
	}
	return supervisor.Run(ctx)
}

type commandOptions struct {
	config string
	digest string
}

func parseCommandOptions(args []string) (commandOptions, error) {
	flags := flag.NewFlagSet("radius-runtime", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	c := commandOptions{}
	flags.StringVar(&c.config, "config", "/etc/soha-radius-runtime/runtime.json", "installer-approved private configuration")
	flags.StringVar(&c.digest, "configuration-digest", "", "print the digest of a protected configuration directory")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return commandOptions{}, err
		}
		return commandOptions{}, runtime.ErrInvalid
	}
	if flags.NArg() != 0 {
		return commandOptions{}, runtime.ErrInvalid
	}
	configSet, digestSet := false, false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configSet = true
		}
		if f.Name == "configuration-digest" {
			digestSet = true
		}
	})
	if digestSet && (c.digest == "" || configSet) {
		return commandOptions{}, runtime.ErrInvalid
	}
	return c, nil
}
func executeCommand(ctx context.Context, c commandOptions, output io.Writer) error {
	if c.digest != "" {
		digest, err := runtime.ConfigurationDigest(c.digest)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(output, digest)
		return err
	}
	return run(ctx, c.config)
}
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	options, err := parseCommandOptions(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(os.Stdout, "Usage: radius-runtime [--config <private runtime.json>] | --configuration-digest <protected directory>")
		return
	}
	if err == nil {
		err = executeCommand(ctx, options, os.Stdout)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "RADIUS runtime stopped; inspect trusted control receipt and owner state")
		os.Exit(1)
	}
}
