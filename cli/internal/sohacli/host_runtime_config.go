package sohacli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

var managedEnvName = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func runHostConfig(ctx context.Context, args []string, rt Runtime) error {
	fs := newRuntimeFlagSet("host config", args, rt)
	var setNames, removeNames repeatableFlag
	fs.Var(&setNames, "set-env", "stage the value of a local environment variable using its name")
	fs.Var(&removeNames, "remove-env", "remove an environment variable from subsequent revisions")
	yes := fs.Bool("yes", false, "confirm changes to runtime configuration")
	profile := fs.String("profile", "", "profile name")
	output := fs.String("output", "text", "output format: text or json")
	positionals, err := parseHostInterspersedFlags(fs, args)
	if err != nil {
		return err
	}
	appID, err := singleHostAppID(positionals, "config")
	if err != nil {
		return err
	}
	format, err := normalizeOutputFormat(*output, "text", "json")
	if err != nil {
		return usageError{message: err.Error()}
	}
	update, err := readHostEnvUpdate(setNames, removeNames, *yes)
	if err != nil {
		return err
	}
	client, err := loadHostClient(ctx, rt, *profile)
	if err != nil {
		return err
	}
	result, err := hostRuntimeConfig(ctx, client, appID, update)
	if err != nil {
		return err
	}
	if format == "json" {
		return json.NewEncoder(rt.Out).Encode(result)
	}
	if _, err := fmt.Fprintf(rt.Out, "Database warning threshold: %d bytes\n", result.DataWarningBytes); err != nil {
		return err
	}
	for _, name := range result.EnvironmentKeys {
		if _, err := fmt.Fprintf(rt.Out, "%s = [configured]\n", safeHostField(name)); err != nil {
			return err
		}
	}
	return nil
}

func readHostEnvUpdate(setNames, removeNames []string, yes bool) (*sohaapi.UpdateHostedAppRuntimeConfigRequest, error) {
	if len(setNames)+len(removeNames) == 0 {
		return nil, nil
	}
	if !yes || len(setNames) > 32 || len(removeNames) > 32 {
		return nil, usageError{message: "runtime configuration changes require --yes and at most 32 set or remove names"}
	}
	req := &sohaapi.UpdateHostedAppRuntimeConfigRequest{}
	seen := map[string]bool{}
	for _, names := range [][]string{setNames, removeNames} {
		for _, name := range names {
			if !managedEnvName.MatchString(name) || seen[name] {
				return nil, usageError{message: "each environment variable must have a unique valid name"}
			}
			seen[name] = true
		}
	}
	for _, name := range setNames {
		value, exists := os.LookupEnv(name)
		if !exists || value == "" || len(value) > 4096 {
			return nil, usageError{message: "the named local environment variable is missing, empty or over 4096 bytes"}
		}
		req.SetEnvironment = append(req.SetEnvironment, sohaapi.HostedAppEnvironmentVariableInput{Name: name, Value: value})
	}
	req.RemoveEnvironment = removeNames
	return req, nil
}

func hostRuntimeConfig(ctx context.Context, client *hostClient, appID string, update *sohaapi.UpdateHostedAppRuntimeConfigRequest) (sohaapi.HostedAppRuntimeConfig, error) {
	if update == nil {
		out, err := client.sdk.GetHostedAppRuntimeConfig(ctx, appID)
		return out, client.wrapErr(err)
	}
	out, err := client.sdk.UpdateHostedAppRuntimeConfig(ctx, appID, *update)
	if err != nil {
		// A remote proxy can echo a submitted value in an error response.
		return sohaapi.HostedAppRuntimeConfig{}, errors.New("runtime configuration was not confirmed")
	}
	return out, nil
}
