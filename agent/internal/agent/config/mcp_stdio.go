package config

import (
	json "encoding/json/v2"
	"errors"
	"path"
	"strings"

	contractenv "github.com/opensoha/soha/contracts/agent/environment"
)

type MCPStdioAssetRef struct {
	ID      string
	Version string
}

func loadMCPStdioAssets(filename string) (map[MCPStdioAssetRef][]string, error) {
	if filename == "" {
		return nil, nil
	}
	raw, err := ReadPrivateFile(filename, 256<<10)
	if err != nil {
		return nil, err
	}
	var entries []struct {
		ID      string   `json:"id"`
		Version string   `json:"version"`
		Argv    []string `json:"argv"`
	}
	if json.Unmarshal(raw, &entries, json.RejectUnknownMembers(true)) != nil || entries == nil || len(entries) > 64 {
		return nil, errors.New("expected a bounded MCP stdio asset array")
	}
	assets := make(map[MCPStdioAssetRef][]string, len(entries))
	for _, entry := range entries {
		ref := MCPStdioAssetRef{ID: entry.ID, Version: entry.Version}
		if !contractenv.ValidPersonalMCPStdioAssetRef(ref.ID, ref.Version) || assets[ref] != nil || !validMCPStdioArgv(entry.Argv) {
			return nil, errors.New("invalid or duplicate MCP stdio asset")
		}
		assets[ref] = entry.Argv
	}
	return assets, nil
}

func validMCPStdioArgv(argv []string) bool {
	if len(argv) == 0 || len(argv) > 64 || !path.IsAbs(argv[0]) || path.Clean(argv[0]) != argv[0] || argv[0] == "/" {
		return false
	}
	for _, arg := range argv {
		if len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return false
		}
	}
	return true
}
