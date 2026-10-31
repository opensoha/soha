package sohacli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strings"
)

func checkWorkbenchFlags(fs *flag.FlagSet, allowed ...string) error {
	set := map[string]bool{"profile": true, "output": true}
	for _, name := range allowed {
		set[name] = true
	}
	var rejected string
	fs.Visit(func(f *flag.Flag) {
		if !set[f.Name] && rejected == "" {
			rejected = f.Name
		}
	})
	if rejected != "" {
		return usageError{message: "--" + rejected + " does not apply to this action"}
	}
	return nil
}

func readWorkbenchInput(rt Runtime, file, inline string, target any) error {
	if (strings.TrimSpace(file) == "") == (strings.TrimSpace(inline) == "") {
		return usageError{message: "provide exactly one of --input FILE|- or --input-json JSON"}
	}
	value, err := readJSONInput(rt, file, inline)
	if err != nil {
		return fmt.Errorf("cannot read workbench JSON input: %w", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cannot encode workbench input")
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid workbench request shape: %w", err)
	}
	return nil
}

func confirmWorkbenchAction(rt Runtime, yes bool, description string) error {
	if yes {
		return nil
	}
	confirmed, err := confirmAction(rt, description)
	if err != nil {
		return err
	}
	if !confirmed {
		return fmt.Errorf("action declined; use --yes only after reviewing the target and input")
	}
	return nil
}
