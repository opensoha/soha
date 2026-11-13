package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
)

var buildpacksCacheName = regexp.MustCompile(`^soha-cnb-[a-f0-9]{64}-(build|launch)$`)

type buildpacksCacheVolume struct {
	Name      string
	UsageData *struct{ Size, RefCount int64 }
}

// Query byte counts directly; Docker CLI rounds its human-readable Size field.
func (r *Runner) buildpacksCacheVolumes(ctx context.Context) ([]buildpacksCacheVolume, error) {
	host, err := url.Parse(r.cfg.Buildpacks.DockerHost)
	if err != nil || host.Scheme != "unix" && host.Scheme != "tcp" {
		return nil, fmt.Errorf("invalid dedicated Buildpacks daemon endpoint")
	}
	network, address := "tcp", host.Host
	if host.Scheme == "unix" {
		network, address = "unix", host.Path
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/system/df?type=volume", nil)
	if err != nil {
		return nil, err
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("buildpacks cache usage unavailable")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("buildpacks cache usage status %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, buildpacksOutputLimit+1))
	if err != nil || len(data) > buildpacksOutputLimit {
		return nil, fmt.Errorf("buildpacks cache usage response exceeds limit or is incomplete")
	}
	var result struct{ Volumes []buildpacksCacheVolume }
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("invalid Buildpacks cache usage")
	}
	return result.Volumes, nil
}

func buildpacksCacheEvictions(volumes []buildpacksCacheVolume, limit int64) ([]string, int64, error) {
	if limit <= 0 {
		limit = cfgpkg.BuildpacksDefaultCacheMaxBytes
	}
	var names []string
	var total int64
	if len(volumes) > 1024 {
		return nil, 0, fmt.Errorf("too many Buildpacks cache volumes")
	}
	for _, volume := range volumes {
		if !buildpacksCacheName.MatchString(volume.Name) {
			continue
		}
		if volume.UsageData == nil || volume.UsageData.Size < 0 || volume.UsageData.RefCount != 0 || volume.UsageData.Size > 1<<50 {
			return nil, 0, fmt.Errorf("buildpacks cache size or stop state is unknown")
		}
		total += volume.UsageData.Size
		names = append(names, volume.Name)
	}
	if total <= limit {
		names = nil
	}
	return names, total, nil
}

// Called with an empty dedicated daemon, before checkout and after container cleanup.
func (r *Runner) trimBuildpacksCache(ctx context.Context) error {
	if r.cfg.Buildpacks.Runtime == "podman" {
		// The daemonless backend retains no build cache between tasks.
		return nil
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := r.checkBuildpacksDaemon(ctx); err != nil {
		return err
	}
	volumes, err := r.buildpacksCacheVolumes(ctx)
	if err != nil {
		return err
	}
	names, _, err := buildpacksCacheEvictions(volumes, r.cfg.Buildpacks.CacheMaxBytes)
	if err != nil || len(names) == 0 {
		return err
	}
	// ponytail: evict the owned cache together; use per-key LRU if measured rebuild churn warrants it.
	if _, err := buildpacksCommand(ctx, "", r.buildpacksEnvironment(""), "docker", append([]string{"volume", "rm"}, names...)...); err != nil {
		return err
	}
	volumes, err = r.buildpacksCacheVolumes(ctx)
	if err != nil {
		return err
	}
	names, _, err = buildpacksCacheEvictions(volumes, r.cfg.Buildpacks.CacheMaxBytes)
	if err != nil {
		return err
	}
	if len(names) != 0 {
		return fmt.Errorf("buildpacks cache eviction is unconfirmed")
	}
	return nil
}
