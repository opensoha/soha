package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/soha/soha/internal/agent/config"
	apiresponse "github.com/soha/soha/internal/api/response"
	domaindocker "github.com/soha/soha/internal/domain/docker"
	"sigs.k8s.io/yaml"
)

const (
	dockerRuntimeDefaultLogTailLines = 200
	dockerRuntimeMaxLogTailLines     = 2000
	dockerRuntimeDefaultListLimit    = 200
	dockerRuntimeMaxListLimit        = 1000
	dockerRuntimeDefaultReadLimit    = 256 * 1024
	dockerRuntimeMaxReadLimit        = 1024 * 1024
)

var (
	dockerRuntimeServiceNamePattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	dockerRuntimeUpgrader           = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
)

type dockerRuntimeRequest struct {
	ProjectID      string         `json:"projectId"`
	ProjectName    string         `json:"projectName"`
	ComposeContent string         `json:"composeContent"`
	EnvContent     string         `json:"envContent,omitempty"`
	Config         map[string]any `json:"config,omitempty"`
	ServiceName    string         `json:"serviceName,omitempty"`
	TailLines      int            `json:"tailLines,omitempty"`
	Target         string         `json:"target,omitempty"`
	Path           string         `json:"path,omitempty"`
	Limit          int            `json:"limit,omitempty"`
	LimitBytes     int64          `json:"limitBytes,omitempty"`
	Shell          string         `json:"shell,omitempty"`
}

type dockerRuntimeMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
}

type dockerRuntimeWorkspace struct {
	Dir         string
	ProjectName string
}

type dockerRuntimeFlushWriter struct {
	writer  io.Writer
	flusher http.Flusher
}

func (w dockerRuntimeFlushWriter) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	if w.flusher != nil {
		w.flusher.Flush()
	}
	return n, err
}

func registerDockerRuntimeRoutes(router *gin.Engine, cfg cfgpkg.Config) {
	group := router.Group(fmt.Sprintf("%s/docker/runtime", cfg.HTTP.BasePath))
	group.Use(authRequiredAnyMiddleware(cfg.Auth.BearerToken, cfg.ControlPlane.BearerToken))
	{
		group.POST("/logs", handleDockerRuntimeLogs)
		group.POST("/logs/stream", handleDockerRuntimeLogStream)
		group.GET("/terminal", handleDockerRuntimeTerminal)
		group.POST("/volumes", handleDockerRuntimeVolumes)
		group.POST("/volume-files", handleDockerRuntimeVolumeFiles)
		group.POST("/volume-file", handleDockerRuntimeVolumeFile)
	}
}

func handleDockerRuntimeLogs(c *gin.Context) {
	var req dockerRuntimeRequest
	if !bindDockerRuntimeRequest(c, &req) {
		return
	}
	workspace, cleanup, err := prepareDockerRuntimeWorkspace(req)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	defer cleanup()

	tailLines := normalizeDockerRuntimeTailLines(req.TailLines)
	output, err := runDockerRuntimeCommand(c.Request.Context(), workspace.Dir, append(dockerRuntimeComposeArgs(workspace, "logs", "--tail", strconv.Itoa(tailLines)), req.ServiceName)...)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, domaindocker.ProjectRuntimeLogs{
		ProjectID:   req.ProjectID,
		ServiceName: req.ServiceName,
		TailLines:   tailLines,
		Content:     string(output),
		Source:      "agent_docker_cli",
	})
}

func handleDockerRuntimeLogStream(c *gin.Context) {
	var req dockerRuntimeRequest
	if !bindDockerRuntimeRequest(c, &req) {
		return
	}
	workspace, cleanup, err := prepareDockerRuntimeWorkspace(req)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	defer cleanup()

	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Cache-Control", "no-cache")
	c.Status(http.StatusOK)
	flusher, _ := c.Writer.(http.Flusher)
	writer := dockerRuntimeFlushWriter{writer: c.Writer, flusher: flusher}
	tailLines := normalizeDockerRuntimeTailLines(req.TailLines)
	args := append(dockerRuntimeComposeArgs(workspace, "logs", "-f", "--tail", strconv.Itoa(tailLines)), req.ServiceName)
	if err := streamDockerRuntimeCommand(c.Request.Context(), workspace.Dir, writer, writer, args...); err != nil && !errors.Is(err, context.Canceled) {
		_, _ = fmt.Fprintf(writer, "\n[docker-runtime] %v\n", err)
	}
}

func handleDockerRuntimeTerminal(c *gin.Context) {
	conn, err := dockerRuntimeUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	var initMessage dockerRuntimeMessage
	if err := conn.ReadJSON(&initMessage); err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: "terminal init message is required"})
		return
	}
	if initMessage.Type != "init" || strings.TrimSpace(initMessage.Data) == "" {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: "terminal init message is required"})
		return
	}
	var req dockerRuntimeRequest
	if err := json.Unmarshal([]byte(initMessage.Data), &req); err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: "invalid terminal init payload"})
		return
	}
	if err := validateDockerRuntimeRequest(req); err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}
	workspace, cleanup, err := prepareDockerRuntimeWorkspace(req)
	if err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}
	defer cleanup()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	shell := normalizeDockerRuntimeShell(req.Shell)
	args := append(dockerRuntimeComposeArgs(workspace, "exec", "-T"), req.ServiceName, shell)
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = workspace.Dir
	stdin, err := cmd.StdinPipe()
	if err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}
	if err := cmd.Start(); err != nil {
		_ = conn.WriteJSON(dockerRuntimeMessage{Type: "error", Message: err.Error()})
		return
	}

	var writeMu sync.Mutex
	write := func(message dockerRuntimeMessage) bool {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(message) == nil
	}
	go pipeDockerRuntimeOutput(stdout, func(data string) bool {
		return write(dockerRuntimeMessage{Type: "stdout", Data: data})
	})
	go pipeDockerRuntimeOutput(stderr, func(data string) bool {
		return write(dockerRuntimeMessage{Type: "stderr", Data: data})
	})

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer stdin.Close()
		for {
			var message dockerRuntimeMessage
			if err := conn.ReadJSON(&message); err != nil {
				cancel()
				return
			}
			switch message.Type {
			case "input":
				if _, err := io.WriteString(stdin, message.Data); err != nil {
					cancel()
					return
				}
			case "close":
				cancel()
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	select {
	case <-readDone:
	default:
		cancel()
	}
	if waitErr != nil && ctx.Err() == nil {
		_ = write(dockerRuntimeMessage{Type: "error", Message: waitErr.Error()})
		return
	}
	_ = write(dockerRuntimeMessage{Type: "exit", Message: "terminal session closed"})
}

func handleDockerRuntimeVolumes(c *gin.Context) {
	var req dockerRuntimeRequest
	if !bindDockerRuntimeRequest(c, &req) {
		return
	}
	apiresponse.Item(c, http.StatusOK, dockerRuntimeProjectVolumes(req))
}

func handleDockerRuntimeVolumeFiles(c *gin.Context) {
	var req dockerRuntimeRequest
	if !bindDockerRuntimeRequest(c, &req) {
		return
	}
	volume, ok := findDockerRuntimeVolume(dockerRuntimeProjectVolumes(req), req.Target)
	if !ok {
		apiresponse.Error(c, http.StatusNotFound, "not_found", "docker project volume not found")
		return
	}
	workspace, cleanup, err := prepareDockerRuntimeWorkspace(req)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	defer cleanup()

	innerPath := normalizeDockerRuntimeInnerPath(req.Path)
	containerPath := joinDockerRuntimeContainerPath(volume.Target, innerPath)
	limit := normalizeDockerRuntimeListLimit(req.Limit)
	script := dockerRuntimeListScript(containerPath, limit)
	output, err := runDockerRuntimeCommand(c.Request.Context(), workspace.Dir, append(dockerRuntimeComposeArgs(workspace, "exec", "-T", req.ServiceName, "sh", "-lc"), script)...)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, domaindocker.ProjectVolumeFileList{
		ProjectID:   req.ProjectID,
		ServiceName: req.ServiceName,
		Target:      volume.Target,
		Path:        innerPath,
		Items:       parseDockerRuntimeFileEntries(innerPath, output),
	})
}

func handleDockerRuntimeVolumeFile(c *gin.Context) {
	var req dockerRuntimeRequest
	if !bindDockerRuntimeRequest(c, &req) {
		return
	}
	volume, ok := findDockerRuntimeVolume(dockerRuntimeProjectVolumes(req), req.Target)
	if !ok {
		apiresponse.Error(c, http.StatusNotFound, "not_found", "docker project volume not found")
		return
	}
	workspace, cleanup, err := prepareDockerRuntimeWorkspace(req)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	defer cleanup()

	innerPath := normalizeDockerRuntimeInnerPath(req.Path)
	containerPath := joinDockerRuntimeContainerPath(volume.Target, innerPath)
	limitBytes := normalizeDockerRuntimeReadLimit(req.LimitBytes)
	script := dockerRuntimeReadScript(containerPath, limitBytes)
	output, err := runDockerRuntimeCommand(c.Request.Context(), workspace.Dir, append(dockerRuntimeComposeArgs(workspace, "exec", "-T", req.ServiceName, "sh", "-lc"), script)...)
	if err != nil {
		dockerRuntimeWriteError(c, err)
		return
	}
	sizeBytes, content := parseDockerRuntimeFileContent(output)
	apiresponse.Item(c, http.StatusOK, domaindocker.ProjectVolumeFileContent{
		ProjectID:   req.ProjectID,
		ServiceName: req.ServiceName,
		Target:      volume.Target,
		Path:        innerPath,
		Content:     content,
		SizeBytes:   sizeBytes,
		Truncated:   sizeBytes > limitBytes,
	})
}

func bindDockerRuntimeRequest(c *gin.Context, req *dockerRuntimeRequest) bool {
	if err := c.ShouldBindJSON(req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid docker runtime payload")
		return false
	}
	if err := validateDockerRuntimeRequest(*req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", err.Error())
		return false
	}
	return true
}

func validateDockerRuntimeRequest(req dockerRuntimeRequest) error {
	if strings.TrimSpace(req.ProjectID) == "" {
		return errors.New("projectId is required")
	}
	if strings.TrimSpace(req.ProjectName) == "" {
		return errors.New("projectName is required")
	}
	if strings.TrimSpace(req.ComposeContent) == "" {
		return errors.New("composeContent is required")
	}
	if strings.TrimSpace(req.ServiceName) == "" {
		return errors.New("serviceName is required")
	}
	if !dockerRuntimeServiceNamePattern.MatchString(strings.TrimSpace(req.ServiceName)) {
		return errors.New("serviceName is invalid")
	}
	services := dockerRuntimeComposeServiceNames(req.ComposeContent)
	if len(services) > 0 && !slices.Contains(services, strings.TrimSpace(req.ServiceName)) {
		return fmt.Errorf("serviceName %s is not defined in compose", req.ServiceName)
	}
	return nil
}

func prepareDockerRuntimeWorkspace(req dockerRuntimeRequest) (dockerRuntimeWorkspace, func(), error) {
	dir, err := os.MkdirTemp("", "soha-docker-runtime-*")
	if err != nil {
		return dockerRuntimeWorkspace{}, func() {}, fmt.Errorf("create docker runtime workspace: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(strings.TrimSpace(req.ComposeContent)+"\n"), 0o600); err != nil {
		cleanup()
		return dockerRuntimeWorkspace{}, func() {}, fmt.Errorf("write compose.yaml: %w", err)
	}
	envPath := filepath.Join(dir, ".env")
	if strings.TrimSpace(req.EnvContent) != "" {
		if err := os.WriteFile(envPath, []byte(strings.TrimSpace(req.EnvContent)+"\n"), 0o600); err != nil {
			cleanup()
			return dockerRuntimeWorkspace{}, func() {}, fmt.Errorf("write .env: %w", err)
		}
	}
	return dockerRuntimeWorkspace{
		Dir:         dir,
		ProjectName: normalizeDockerRuntimeProjectName(req.ProjectName),
	}, cleanup, nil
}

func dockerRuntimeComposeArgs(workspace dockerRuntimeWorkspace, args ...string) []string {
	base := []string{"compose", "-p", workspace.ProjectName, "-f", "compose.yaml"}
	return append(base, args...)
}

func runDockerRuntimeCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	err := cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return output.Bytes(), fmt.Errorf("docker runtime command timed out")
	}
	if err != nil {
		return output.Bytes(), fmt.Errorf("docker runtime command failed: %w: %s", err, strings.TrimSpace(output.String()))
	}
	return output.Bytes(), nil
}

func streamDockerRuntimeCommand(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil {
		return err
	}
	return ctx.Err()
}

func pipeDockerRuntimeOutput(reader io.Reader, write func(string) bool) {
	buffer := make([]byte, 4096)
	for {
		n, err := reader.Read(buffer)
		if n > 0 && !write(string(buffer[:n])) {
			return
		}
		if err != nil {
			return
		}
	}
}

func dockerRuntimeWriteError(c *gin.Context, err error) {
	apiresponse.Error(c, http.StatusBadGateway, "docker_runtime_failed", fmt.Sprintf("docker runtime request failed: %v", err))
}

func dockerRuntimeProjectVolumes(req dockerRuntimeRequest) []domaindocker.ProjectVolume {
	volumes := make([]domaindocker.ProjectVolume, 0)
	seen := map[string]bool{}
	add := func(volume domaindocker.ProjectVolume) {
		volume.Target = path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(volume.Target), "/"))
		if volume.Target == "." || volume.Target == "/" {
			return
		}
		if seen[volume.Target] {
			return
		}
		volume.BrowseSupported = true
		seen[volume.Target] = true
		volumes = append(volumes, volume)
	}
	for _, volume := range dockerRuntimeComposeVolumes(req.ComposeContent, req.ServiceName) {
		add(volume)
	}
	for _, volume := range dockerRuntimeConfigVolumes(req.Config) {
		add(volume)
	}
	return volumes
}

func dockerRuntimeComposeVolumes(content string, serviceName string) []domaindocker.ProjectVolume {
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	services := mapValueAny(raw["services"])
	service := mapValueAny(services[strings.TrimSpace(serviceName)])
	items, _ := service["volumes"].([]any)
	out := make([]domaindocker.ProjectVolume, 0, len(items))
	for _, item := range items {
		if volume, ok := parseDockerRuntimeVolumeSpec(item); ok {
			out = append(out, volume)
		}
	}
	return out
}

func dockerRuntimeConfigVolumes(config map[string]any) []domaindocker.ProjectVolume {
	items, _ := config["volumes"].([]any)
	out := make([]domaindocker.ProjectVolume, 0, len(items))
	for _, raw := range items {
		item := mapValueAny(raw)
		target := stringValueAny(firstPresent(item["target"], item["containerPath"], item["mountPath"]))
		if strings.TrimSpace(target) == "" {
			continue
		}
		out = append(out, domaindocker.ProjectVolume{
			Name:     stringValueAny(item["name"]),
			Type:     stringValueAny(item["type"]),
			Source:   stringValueAny(firstPresent(item["source"], item["hostPath"], item["name"])),
			Target:   target,
			ReadOnly: boolValueAny(firstPresent(item["readOnly"], item["readonly"], item["ro"])),
			SubPath:  stringValueAny(item["subPath"]),
		})
	}
	return out
}

func parseDockerRuntimeVolumeSpec(raw any) (domaindocker.ProjectVolume, bool) {
	switch value := raw.(type) {
	case string:
		return parseDockerRuntimeVolumeString(value)
	default:
		item := mapValueAny(raw)
		target := stringValueAny(item["target"])
		if strings.TrimSpace(target) == "" {
			return domaindocker.ProjectVolume{}, false
		}
		return domaindocker.ProjectVolume{
			Name:     stringValueAny(item["source"]),
			Type:     stringValueAny(item["type"]),
			Source:   stringValueAny(item["source"]),
			Target:   target,
			ReadOnly: boolValueAny(firstPresent(item["read_only"], item["readOnly"], item["readonly"])),
		}, true
	}
}

func parseDockerRuntimeVolumeString(value string) (domaindocker.ProjectVolume, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return domaindocker.ProjectVolume{}, false
	}
	parts := strings.Split(trimmed, ":")
	if len(parts) == 1 {
		return domaindocker.ProjectVolume{Target: parts[0], Type: "anonymous"}, true
	}
	readOnly := false
	if len(parts) >= 3 {
		mode := strings.ToLower(strings.TrimSpace(parts[len(parts)-1]))
		readOnly = mode == "ro" || strings.Contains(mode, "ro")
	}
	return domaindocker.ProjectVolume{
		Name:     parts[0],
		Source:   parts[0],
		Target:   parts[1],
		ReadOnly: readOnly,
	}, true
}

func findDockerRuntimeVolume(volumes []domaindocker.ProjectVolume, target string) (domaindocker.ProjectVolume, bool) {
	normalized := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(target), "/"))
	for _, volume := range volumes {
		if path.Clean(volume.Target) == normalized {
			return volume, true
		}
	}
	return domaindocker.ProjectVolume{}, false
}

func dockerRuntimeListScript(containerPath string, limit int) string {
	return strings.Join([]string{
		"p=" + shellQuote(containerPath),
		"limit=" + strconv.Itoa(limit),
		`[ -d "$p" ] || { echo "target is not a directory" >&2; exit 2; }`,
		"count=0",
		`for f in "$p"/* "$p"/.[!.]* "$p"/..?*; do`,
		`  [ -e "$f" ] || [ -L "$f" ] || continue`,
		`  name=${f##*/}`,
		`  kind=file; [ -d "$f" ] && kind=directory; [ -L "$f" ] && kind=symlink`,
		`  size=$(stat -c %s "$f" 2>/dev/null || wc -c < "$f" 2>/dev/null || echo 0)`,
		`  mtime=$(stat -c %y "$f" 2>/dev/null || echo "")`,
		`  printf '%s\t%s\t%s\t%s\n' "$kind" "$size" "$mtime" "$name"`,
		`  count=$((count + 1))`,
		`  [ "$count" -ge "$limit" ] && break`,
		"done",
	}, "\n")
}

func dockerRuntimeReadScript(containerPath string, limitBytes int64) string {
	return strings.Join([]string{
		"p=" + shellQuote(containerPath),
		"limit=" + strconv.FormatInt(limitBytes, 10),
		`[ -f "$p" ] || { echo "target is not a regular file" >&2; exit 2; }`,
		`size=$(stat -c %s "$p" 2>/dev/null || wc -c < "$p" 2>/dev/null || echo 0)`,
		`printf '__SOHA_SIZE__%s\n' "$size"`,
		`head -c "$limit" "$p"`,
	}, "\n")
}

func parseDockerRuntimeFileEntries(parentPath string, output []byte) []domaindocker.ProjectVolumeFileEntry {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	items := make([]domaindocker.ProjectVolumeFileEntry, 0, len(lines))
	for _, line := range lines {
		fields := strings.SplitN(line, "\t", 4)
		if len(fields) < 4 {
			continue
		}
		size, _ := strconv.ParseInt(strings.TrimSpace(fields[1]), 10, 64)
		name := strings.TrimSpace(fields[3])
		if name == "" {
			continue
		}
		items = append(items, domaindocker.ProjectVolumeFileEntry{
			Name:       name,
			Path:       normalizeDockerRuntimeInnerPath(path.Join(parentPath, name)),
			Kind:       strings.TrimSpace(fields[0]),
			SizeBytes:  size,
			ModifiedAt: strings.TrimSpace(fields[2]),
		})
	}
	return items
}

func parseDockerRuntimeFileContent(output []byte) (int64, string) {
	const marker = "__SOHA_SIZE__"
	raw := string(output)
	firstLine, rest, ok := strings.Cut(raw, "\n")
	if !ok || !strings.HasPrefix(firstLine, marker) {
		return int64(len(output)), raw
	}
	size, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimPrefix(firstLine, marker)), 10, 64)
	return size, rest
}

func normalizeDockerRuntimeProjectName(value string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
			continue
		}
		if r == '.' || r == ' ' {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_")
	if out == "" {
		return "soha-docker"
	}
	return out
}

func normalizeDockerRuntimeTailLines(value int) int {
	if value <= 0 {
		return dockerRuntimeDefaultLogTailLines
	}
	if value > dockerRuntimeMaxLogTailLines {
		return dockerRuntimeMaxLogTailLines
	}
	return value
}

func normalizeDockerRuntimeListLimit(value int) int {
	if value <= 0 {
		return dockerRuntimeDefaultListLimit
	}
	if value > dockerRuntimeMaxListLimit {
		return dockerRuntimeMaxListLimit
	}
	return value
}

func normalizeDockerRuntimeReadLimit(value int64) int64 {
	if value <= 0 {
		return dockerRuntimeDefaultReadLimit
	}
	if value > dockerRuntimeMaxReadLimit {
		return dockerRuntimeMaxReadLimit
	}
	return value
}

func normalizeDockerRuntimeInnerPath(value string) string {
	cleaned := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(value), "/"))
	if cleaned == "." || cleaned == "" {
		return "/"
	}
	return cleaned
}

func joinDockerRuntimeContainerPath(target string, innerPath string) string {
	cleanTarget := path.Clean("/" + strings.TrimPrefix(strings.TrimSpace(target), "/"))
	cleanInner := strings.TrimPrefix(normalizeDockerRuntimeInnerPath(innerPath), "/")
	if cleanInner == "" {
		return cleanTarget
	}
	return path.Join(cleanTarget, cleanInner)
}

func normalizeDockerRuntimeShell(value string) string {
	switch strings.TrimSpace(value) {
	case "/bin/bash", "bash":
		return "/bin/bash"
	case "/bin/sh", "sh", "":
		return "/bin/sh"
	default:
		return "/bin/sh"
	}
}

func dockerRuntimeComposeServiceNames(content string) []string {
	raw := map[string]any{}
	if err := yaml.Unmarshal([]byte(content), &raw); err != nil {
		return nil
	}
	services := mapValueAny(raw["services"])
	names := make([]string, 0, len(services))
	for name := range services {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

func mapValueAny(raw any) map[string]any {
	mapped, ok := raw.(map[string]any)
	if !ok || mapped == nil {
		return map[string]any{}
	}
	return mapped
}

func firstPresent(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func stringValueAny(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func boolValueAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "true" || normalized == "1" || normalized == "yes" || normalized == "ro"
	default:
		return false
	}
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
