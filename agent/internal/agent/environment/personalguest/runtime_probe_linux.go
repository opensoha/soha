//go:build linux

package personalguest

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

// Readiness has bounded retained fixtures; it cannot format or delete data.
func (b *runtimeBroker) probeFiles(ctx context.Context) error {
	root, err := os.OpenRoot(runtimeWorkspace(b.plan))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	path := ".soha-runtime-proof-" + b.plan.ArtifactSHA256 + ".txt"
	const content = "Soha personal runtime file proof\n"
	data, err := readRuntimeFile(root, path)
	if errors.Is(err, os.ErrNotExist) {
		op := guest.RuntimeOperation{Kind: "file_write", File: &guest.RuntimeFileInput{Path: path, ContentBase64: base64.StdEncoding.EncodeToString([]byte(content))}}
		if _, err := runtimeFile(ctx, root, op); err != nil {
			return err
		}
		data, err = readRuntimeFile(root, path)
	}
	if err != nil || string(data) != content {
		return errors.New("runtime file read/write proof failed")
	}
	return nil
}

func (b *runtimeBroker) probeDocker(ctx context.Context, authorized guest.RuntimeOperation) error {
	root, err := os.OpenRoot(filepath.Dir(b.cfg.StateFile))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := "runtime-docker-proof-" + b.plan.ArtifactSHA256 + ".json"
	if file, err := root.Open(name); err == nil {
		defer func() { _ = file.Close() }()
		var old struct{ ArtifactSHA256, Image string }
		if json.NewDecoder(file).Decode(&old) != nil || old.ArtifactSHA256 != b.plan.ArtifactSHA256 || old.Image != b.plan.DockerImage {
			return errors.New("docker proof identity changed")
		}
		_, code, err := runSandboxCommand(ctx, "/usr/bin/docker", "image", "inspect", b.plan.DockerImage)
		if err != nil || *code != 0 {
			return errors.New("current approved Docker image unavailable")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	probe := authorized
	probe.Kind, probe.DockerImage = "docker", b.plan.DockerImage
	probe.Request.Action, probe.Request.Argv, probe.Request.TaskID = "execute", []string{"/usr/bin/printf", "SOHA_DOCKER_PROOF"}, ""
	output, code, err := b.docker(ctx, probe)
	if err != nil || *code != 0 || *output != "SOHA_DOCKER_PROOF" {
		return errors.New("bounded Docker runtime proof failed")
	}
	file, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(map[string]string{"ArtifactSHA256": b.plan.ArtifactSHA256, "Image": b.plan.DockerImage}); err != nil {
		return err
	}
	return file.Sync()
}
