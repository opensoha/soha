package sohacli

import (
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"
)

var (
	version = "0.1.0"
	commit  = "unknown"
	date    = "unknown"
)

type VersionInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	GoOS    string `json:"goos"`
	GoArch  string `json:"goarch"`
}

func BuildInfo() VersionInfo {
	return VersionInfo{
		Version: firstNonEmptyString(version, "0.1.0"),
		Commit:  firstNonEmptyString(commit, "unknown"),
		Date:    firstNonEmptyString(date, "unknown"),
		GoOS:    runtime.GOOS,
		GoArch:  runtime.GOARCH,
	}
}

func writeVersion(out io.Writer, jsonOutput bool) error {
	info := BuildInfo()
	if jsonOutput {
		raw, err := json.MarshalIndent(info, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, string(raw))
		return err
	}
	_, err := fmt.Fprintf(out, "soha %s\ncommit: %s\ndate: %s\ngo: %s/%s\n", strings.TrimSpace(info.Version), info.Commit, info.Date, info.GoOS, info.GoArch)
	return err
}
