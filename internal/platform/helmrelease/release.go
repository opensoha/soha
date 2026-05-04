package helmrelease

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"

	"sigs.k8s.io/yaml"
)

var gzipMagicHeader = []byte{0x1f, 0x8b, 0x08}

type Release struct {
	Name      string                 `json:"name,omitempty"`
	Info      *Info                  `json:"info,omitempty"`
	Chart     *Chart                 `json:"chart,omitempty"`
	Config    map[string]any         `json:"config,omitempty"`
	Manifest  string                 `json:"manifest,omitempty"`
	Version   int                    `json:"version,omitempty"`
	Namespace string                 `json:"namespace,omitempty"`
	Labels    map[string]string      `json:"-"`
	Raw       map[string]any         `json:"-"`
	Hooks     []map[string]any       `json:"hooks,omitempty"`
	Meta      map[string]interface{} `json:"-"`
}

type Info struct {
	FirstDeployed time.Time `json:"first_deployed,omitempty"`
	LastDeployed  time.Time `json:"last_deployed,omitempty"`
	Deleted       time.Time `json:"deleted,omitempty"`
	Description   string    `json:"description,omitempty"`
	Status        string    `json:"status,omitempty"`
	Notes         string    `json:"notes,omitempty"`
}

type Chart struct {
	Metadata *ChartMetadata `json:"metadata,omitempty"`
}

type ChartMetadata struct {
	Name        string            `json:"name,omitempty"`
	Version     string            `json:"version,omitempty"`
	AppVersion  string            `json:"appVersion,omitempty"`
	Description string            `json:"description,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

func Decode(encoded string, labels map[string]string) (*Release, error) {
	decodedBytes, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, err
	}
	if len(decodedBytes) > 3 && bytes.Equal(decodedBytes[:3], gzipMagicHeader) {
		reader, err := gzip.NewReader(bytes.NewReader(decodedBytes))
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		uncompressed, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		decodedBytes = uncompressed
	}

	var release Release
	if err := json.Unmarshal(decodedBytes, &release); err != nil {
		return nil, err
	}
	release.Labels = cloneStringMap(labels)
	return &release, nil
}

func ValuesYAML(release *Release) (string, error) {
	if release == nil || len(release.Config) == 0 {
		return "{}\n", nil
	}
	content, err := yaml.Marshal(release.Config)
	if err != nil {
		return "", err
	}
	return string(content), nil
}

func Digest(content string) string {
	if strings.TrimSpace(content) == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func cloneStringMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
