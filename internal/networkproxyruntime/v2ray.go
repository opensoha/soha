package networkproxyruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

type v2rayStat struct {
	Name  string `json:"name"`
	Value int64  `json:"value,string"`
}

func (e *engine) v2rayHealth(ctx context.Context) error {
	_, _, _, err := e.v2rayStats(ctx)
	return err
}

func (e *engine) v2rayStats(ctx context.Context) (string, int64, int64, error) {
	versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	versionOutput, err := exec.CommandContext(versionCtx, e.config.EngineBinary, "version").Output() // #nosec G204 -- binary is selected by the local operator, not control-plane input.
	if err != nil || len(versionOutput) > 4096 {
		return "", 0, 0, fmt.Errorf("V2Ray version probe failed")
	}
	version := strings.Fields(string(versionOutput))
	if len(version) < 2 || version[0] != "V2Ray" || len(version[1]) > 64 {
		return "", 0, 0, fmt.Errorf("V2Ray version response is invalid")
	}
	statsCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	// #nosec G204 -- operator-owned binary, fixed API arguments, and a validated loopback controller origin.
	command := exec.CommandContext(statsCtx, e.config.EngineBinary, "api", "stats", "-json",
		"-s", strings.TrimPrefix(e.config.ControllerURL, "http://"), "-regexp",
		`^inbound>>>.*>>>traffic>>>(uplink|downlink)$`)
	raw, err := command.Output()
	if err != nil || len(raw) > 2<<20 {
		return "", 0, 0, fmt.Errorf("V2Ray StatsService probe failed")
	}
	upload, download, err := v2rayTotals(raw)
	if err != nil {
		return "", 0, 0, err
	}
	return version[1], upload, download, nil
}

func v2rayTotals(raw []byte) (int64, int64, error) {
	var response struct {
		Stat []v2rayStat `json:"stat"`
	}
	if err := json.Unmarshal(raw, &response); err != nil {
		return 0, 0, fmt.Errorf("V2Ray StatsService response is invalid")
	}
	var upload, download int64
	for _, stat := range response.Stat {
		if stat.Value < 0 {
			return 0, 0, fmt.Errorf("V2Ray StatsService counter is invalid")
		}
		if strings.HasPrefix(stat.Name, "inbound>>>soha-api>>>") {
			continue
		}
		switch {
		case strings.HasSuffix(stat.Name, ">>>uplink"):
			upload += stat.Value
		case strings.HasSuffix(stat.Name, ">>>downlink"):
			download += stat.Value
		}
	}
	return upload, download, nil
}
