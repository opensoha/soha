package networkproxyruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"sigs.k8s.io/yaml"
)

type engine struct {
	config        Config
	controller    *clashController
	secret        string
	process       *exec.Cmd
	wait          chan error
	startedAt     time.Time
	bootstrapAt   time.Time
	fileHash      string
	protection    *domain.Protection
	egress        *egressGuard
	compiledRules []compiledMihomoRule
}

func newEngine(config Config, secret string) *engine {
	e := &engine{config: config, secret: secret}
	if config.Engine != domain.EngineV2Ray {
		e.controller = newClashController(config.ControllerURL, secret)
	}
	return e
}

func (e *engine) command(path string, check bool) *exec.Cmd {
	var args []string
	switch e.config.Engine {
	case domain.EngineMihomo:
		args = []string{"-f", path}
		if check {
			args = append([]string{"-t"}, args...)
		}
	case domain.EngineSingBox:
		if check {
			args = []string{"check", "-c", path}
		} else {
			args = []string{"run", "-c", path}
		}
	case domain.EngineV2Ray:
		if check {
			args = []string{"test", "-format=json", "-config", path}
		} else {
			args = []string{"run", "-format=json", "-config", path}
		}
	}
	command := exec.Command(e.config.EngineBinary, args...) // #nosec G204 -- binary is operator-owned local configuration; arguments never use a shell.
	configureOwnedEngineProcess(command)
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	return command
}

func (e *engine) render(content string) ([]byte, error) {
	controller, err := url.Parse(e.config.ControllerURL)
	if err != nil {
		return nil, err
	}
	address := controller.Host
	switch e.config.Engine {
	case domain.EngineMihomo:
		return renderMihomo(content, address, e.secret)
	case domain.EngineSingBox:
		return renderSingBox(content, address, e.secret)
	case domain.EngineV2Ray:
		return renderV2Ray(content, address)
	}
	return nil, fmt.Errorf("proxy engine is unsupported")
}

func renderMihomo(content, address, secret string) ([]byte, error) {
	var document map[string]any
	if err := yaml.UnmarshalStrict([]byte(content), &document); err != nil || document == nil {
		return nil, fmt.Errorf("mihomo configuration is invalid")
	}
	document["external-controller"] = address
	document["secret"] = secret
	return yaml.Marshal(document)
}

func renderSingBox(content, address, secret string) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal([]byte(content), &document); err != nil || document == nil {
		return nil, fmt.Errorf("sing-box configuration is invalid")
	}
	experimental, err := object(document["experimental"])
	if err != nil {
		return nil, err
	}
	experimental["clash_api"] = map[string]any{"external_controller": address, "secret": secret}
	document["experimental"] = experimental
	return json.Marshal(document)
}

func renderV2Ray(content, address string) ([]byte, error) {
	var document map[string]any
	if err := json.Unmarshal([]byte(content), &document); err != nil || document == nil {
		return nil, fmt.Errorf("V2Ray configuration is invalid")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return nil, fmt.Errorf("V2Ray StatsService requires IPv4 loopback")
	}
	var portNumber int
	if _, err := fmt.Sscanf(port, "%d", &portNumber); err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("V2Ray StatsService port is invalid")
	}
	if document["api"] != nil || document["stats"] != nil {
		return nil, fmt.Errorf("V2Ray API and stats are managed by the proxy runtime")
	}
	document["api"] = map[string]any{"tag": "soha-api", "services": []string{"StatsService"}}
	document["stats"] = map[string]any{}
	inbounds, ok := document["inbounds"].([]any)
	if !ok {
		return nil, fmt.Errorf("V2Ray inbounds are required")
	}
	for _, raw := range inbounds {
		inbound, ok := raw.(map[string]any)
		if !ok || inbound["tag"] == "soha-api" {
			return nil, fmt.Errorf("V2Ray inbound tag is reserved or invalid")
		}
	}
	document["inbounds"] = append(inbounds, map[string]any{"tag": "soha-api", "listen": host, "port": portNumber,
		"protocol": "dokodemo-door", "settings": map[string]any{"address": host}})
	routing, err := object(document["routing"])
	if err != nil {
		return nil, err
	}
	rules, _ := routing["rules"].([]any)
	routing["rules"] = append([]any{map[string]any{"type": "field", "inboundTag": []string{"soha-api"}, "outboundTag": "soha-api"}}, rules...)
	document["routing"] = routing
	policy, err := object(document["policy"])
	if err != nil {
		return nil, err
	}
	system, err := object(policy["system"])
	if err != nil {
		return nil, err
	}
	system["statsInboundUplink"] = true
	system["statsInboundDownlink"] = true
	policy["system"] = system
	document["policy"] = policy
	return json.Marshal(document)
}

func object(value any) (map[string]any, error) {
	if value == nil {
		return map[string]any{}, nil
	}
	result, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("proxy configuration section must be an object")
	}
	return result, nil
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".soha-proxy-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}

func (e *engine) validate(ctx context.Context, content []byte) error {
	file, err := os.CreateTemp(e.config.StateDir, ".candidate-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := e.command(file.Name(), true)
	command = exec.CommandContext(checkCtx, command.Path, command.Args[1:]...) // #nosec G204 -- reuse the operator-owned engine command with a validation deadline.
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		return fmt.Errorf("proxy configuration validation failed")
	}
	return nil
}

func (e *engine) start(ctx context.Context) error {
	if e.egress == nil || e.egress.handler == nil || !e.egress.handler.Ready() {
		return fmt.Errorf("proxy egress guard is inactive")
	}
	e.egress.release()
	command := e.command(e.config.ConfigFile, false)
	if err := command.Start(); err != nil {
		return fmt.Errorf("start proxy engine: %w", err)
	}
	e.process = command
	started := false
	defer func() {
		if !started {
			e.stop()
		}
	}()
	e.startedAt = time.Now().UTC()
	e.wait = make(chan error, 1)
	go func() { e.wait <- command.Wait() }()
	for attempt := 0; attempt < 20; attempt++ {
		if err := e.health(ctx); err == nil {
			started = true
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return fmt.Errorf("proxy engine health check failed")
}

func (e *engine) stop() {
	e.egress.close()
	e.egress = nil
	if e.process == nil {
		return
	}
	_ = e.process.Process.Signal(syscall.SIGTERM)
	select {
	case <-e.wait:
	case <-time.After(5 * time.Second):
		_ = e.process.Process.Kill()
		<-e.wait
	}
	e.process = nil
	e.wait = nil
}

func (e *engine) health(ctx context.Context) error {
	if e.egress == nil || e.egress.handler == nil || !e.egress.handler.Ready() {
		return fmt.Errorf("proxy egress guard expired")
	}
	if e.process == nil {
		return fmt.Errorf("proxy engine is stopped")
	}
	select {
	case <-e.wait:
		e.process = nil
		return fmt.Errorf("proxy engine exited")
	default:
	}
	if err := e.egress.verify(ctx); err != nil {
		return err
	}
	if e.fileHash != "" {
		raw, err := os.ReadFile(e.config.ConfigFile)
		if err != nil || fmt.Sprintf("sha256:%x", sha256.Sum256(raw)) != e.fileHash {
			return fmt.Errorf("proxy configuration file drifted")
		}
	}
	if e.controller != nil {
		if e.protection != nil {
			if err := e.verifyProtection(ctx); err != nil {
				return err
			}
		}
		_, err := e.controller.version(ctx)
		return err
	}
	return e.v2rayHealth(ctx)
}

func (e *engine) apply(ctx context.Context, desired domain.Configuration) domain.Applied {
	e.stop()
	if !desired.Enabled {
		return domain.Applied{Revision: desired.Revision, Status: "applied", ReadbackHash: desired.Digest()}
	}
	old, readErr := os.ReadFile(e.config.ConfigFile)
	if readErr != nil && !os.IsNotExist(readErr) {
		return domain.Applied{Revision: desired.Revision, Status: "rejected", ReasonCode: "read_failed",
			ReadbackHash: fmt.Sprintf("sha256:%x", sha256.Sum256(nil))}
	}
	hadOld := readErr == nil
	oldHash := sha256.Sum256(old)
	result := domain.Applied{Revision: desired.Revision, Status: "rejected", ReadbackHash: fmt.Sprintf("sha256:%x", oldHash)}
	rollback := func() {
		e.stop()
		if hadOld {
			if err := writeAtomic(e.config.ConfigFile, old); err != nil {
				result.ReasonCode = "rollback_failed"
				return
			}
		} else if err := os.Remove(e.config.ConfigFile); err != nil && !os.IsNotExist(err) {
			result.ReasonCode = "rollback_failed"
			return
		}
		// Restore the file only: an opaque replacement may revoke access granted by its predecessor.
		result.Status = "rolled-back"
	}
	if err := validateProtection(desired); err != nil {
		result.ReasonCode = "protection_invalid"
		return result
	}
	rendered, err := e.render(desired.Content)
	if err != nil {
		result.ReasonCode = "render_failed"
		return result
	}
	rendered, err = protectConfiguration(e.config.Engine, rendered, desired.Protection)
	if err != nil {
		result.ReasonCode = "protection_compile_failed"
		return result
	}
	rendered, err = e.guardConfiguration(ctx, rendered, desired)
	if err != nil {
		result.ReasonCode = "bootstrap_guard_failed"
		return result
	}
	if err := e.prepareRuleReadback(rendered); err != nil {
		e.stop()
		result.ReasonCode = "rule_readback_unsupported"
		return result
	}
	committed := false
	defer func() {
		if !committed {
			e.stop()
		}
	}()
	if err := e.validate(ctx, rendered); err != nil {
		result.ReasonCode = "validation_failed"
		return result
	}
	if err := writeAtomic(e.config.ConfigFile, rendered); err != nil {
		result.ReasonCode = "write_failed"
		return result
	}
	e.fileHash = fmt.Sprintf("sha256:%x", sha256.Sum256(rendered))
	e.protection = desired.Protection
	if desired.Enabled {
		if err := e.start(ctx); err != nil {
			result.ReasonCode = "start_failed"
			rollback()
			return result
		}
	}
	actual, err := os.ReadFile(e.config.ConfigFile)
	if err != nil || !strings.EqualFold(fmt.Sprintf("%x", sha256.Sum256(actual)), fmt.Sprintf("%x", sha256.Sum256(rendered))) {
		result.ReasonCode = "readback_failed"
		rollback()
		return result
	}
	e.bootstrapAt = time.Now().UTC()
	committed = true
	result.Status, result.ReadbackHash = "applied", desired.Digest()
	return result
}

func (e *engine) close(ctx context.Context, id string) (string, error) {
	if e.controller == nil {
		return "failed", fmt.Errorf("connection close is unsupported")
	}
	return e.controller.close(ctx, id)
}

func (e *engine) prepareRuleReadback(raw []byte) error {
	if e.config.Engine != domain.EngineMihomo {
		e.compiledRules = nil
		return nil
	}
	var err error
	e.compiledRules, err = expectedMihomoRules(raw)
	return err
}
