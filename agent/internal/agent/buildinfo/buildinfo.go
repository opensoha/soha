package buildinfo

import (
	"fmt"
	"runtime"
	"strings"
)

var (
	Name    = "soha-agent"
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type Info struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	Date      string `json:"date"`
	GoVersion string `json:"goVersion"`
	GOOS      string `json:"goos"`
	GOARCH    string `json:"goarch"`
}

func Current() Info {
	return Info{
		Name:      valueOrDefault(Name, "soha-agent"),
		Version:   valueOrDefault(Version, "dev"),
		Commit:    valueOrDefault(Commit, "unknown"),
		Date:      valueOrDefault(Date, "unknown"),
		GoVersion: runtime.Version(),
		GOOS:      runtime.GOOS,
		GOARCH:    runtime.GOARCH,
	}
}

func Human() string {
	info := Current()
	return fmt.Sprintf("%s version %s (commit %s, built %s, %s %s/%s)",
		info.Name,
		info.Version,
		info.Commit,
		info.Date,
		info.GoVersion,
		info.GOOS,
		info.GOARCH,
	)
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}
