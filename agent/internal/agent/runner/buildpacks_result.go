package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

func readBuildpacksFile(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("buildpacks output is missing, oversized or not a regular file")
	}
	file, err := os.Open(path) // #nosec G304 -- Private task output; Lstat/Stat/SameFile reject symlinks and file replacement.
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("buildpacks output changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("buildpacks output exceeded the limit")
	}
	return data, err
}

func (r *Runner) buildpacksResult(ctx context.Context, image, root, home string) (map[string]any, error) {
	data, err := readBuildpacksFile(filepath.Join(root, "report", "report.toml"), 65536)
	if err != nil {
		return nil, err
	}
	var report struct {
		Image struct {
			Digest string   `toml:"digest"`
			Tags   []string `toml:"tags"`
		} `toml:"image"`
	}
	if _, err := toml.Decode(string(data), &report); err != nil || len(report.Image.Digest) != 71 || !imageDigestPattern.MatchString(report.Image.Digest) || !containsString(report.Image.Tags, image) {
		return nil, fmt.Errorf("buildpacks publish report has no matching image digest")
	}
	if err := r.verifyBuildpacksImage(ctx, image, report.Image.Digest, home); err != nil {
		return nil, err
	}
	files, err := buildpacksSBOMFiles(filepath.Join(root, "sbom"))
	if err != nil {
		return nil, err
	}
	result := map[string]any{"image": image, "imageDigest": report.Image.Digest, "sbom": files, "reportDigest": fmt.Sprintf("sha256:%x", sha256.Sum256(data))}
	result["artifact"], result["artifacts"] = buildImageArtifact(result, image), buildArtifactList(result, image)
	return result, nil
}

func (r *Runner) verifyBuildpacksImage(ctx context.Context, image, digest, home string) error {
	args := []string{"manifest", "inspect", "--verbose"}
	registry, _, _ := strings.Cut(image, "/")
	if containsString(r.cfg.Buildpacks.InsecureRegistries, registry) {
		args = append(args, "--insecure")
	}
	output, err := buildpacksCommand(ctx, "", r.buildpacksEnvironment(home), "docker", append(args, image)...)
	if err != nil {
		return err
	}
	// Docker CLI verbose output is ImageManifest.Descriptor, not a top-level Digest.
	var manifest struct {
		Descriptor struct {
			Digest   string                            `json:"digest"`
			Platform struct{ OS, Architecture string } `json:"platform"`
		} `json:"Descriptor"`
	}
	if json.Unmarshal([]byte(output), &manifest) != nil || manifest.Descriptor.Digest != digest {
		return fmt.Errorf("registry image digest does not match the Buildpacks report")
	}
	platform := manifest.Descriptor.Platform.OS + "/" + manifest.Descriptor.Platform.Architecture
	if platform != r.cfg.Buildpacks.Platform {
		return fmt.Errorf("published image platform mismatch")
	}
	return nil
}

func buildpacksSBOMFiles(root string) ([]map[string]any, error) {
	files := []map[string]any{}
	var total int64
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if len(files) >= 256 {
			return fmt.Errorf("too many Buildpacks SBOM files")
		}
		data, err := readBuildpacksFile(path, 4<<20)
		if err != nil {
			return err
		}
		total += int64(len(data))
		if total > 20<<20 {
			return fmt.Errorf("buildpacks SBOM output exceeded the limit")
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, map[string]any{"path": filepath.ToSlash(relative), "digest": fmt.Sprintf("sha256:%x", sha256.Sum256(data)), "size": len(data)})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("buildpacks SBOM output is missing")
	}
	return files, nil
}
