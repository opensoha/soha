package helmrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"sigs.k8s.io/yaml"
)

// FrozenChartArchive transports already rendered resources through native Helm
// install/upgrade/rollback, including hooks. It never evaluates source templates
// again. The original source digest remains part of the delivery snapshot.
func FrozenChartArchive(original []byte, manifest string, hooks []string) ([]byte, error) {
	if len(manifest) > 2<<20 || len(hooks) > 50 {
		return nil, errors.New("helm rendered resources exceed the snapshot limit")
	}
	total := len(manifest)
	for _, hook := range hooks {
		total += len(hook)
	}
	if total > 4<<20 {
		return nil, errors.New("helm rendered hooks exceed the snapshot limit")
	}
	if err := ValidateChartArchive(original); err != nil {
		return nil, err
	}
	metadata, err := chartMetadata(original)
	if err != nil {
		return nil, err
	}
	delete(metadata, "dependencies")
	metadataBytes, err := yaml.Marshal(metadata)
	if err != nil {
		return nil, err
	}
	files := []struct{ name, content string }{{"Chart.yaml", string(metadataBytes)}, {"values.yaml", "{}\n"}, {"templates/resources.yaml", "{{ print " + strconv.Quote(manifest) + " }}"}}
	for index, hook := range hooks {
		files = append(files, struct{ name, content string }{fmt.Sprintf("templates/hook-%d.yaml", index), "{{ print " + strconv.Quote(hook) + " }}"})
	}
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zip)
	for _, file := range files {
		if err := writer.WriteHeader(&tar.Header{Name: "frozen/" + file.name, Mode: 0600, Size: int64(len(file.content))}); err != nil {
			return nil, err
		}
		if _, err := io.WriteString(writer, file.content); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	if err := zip.Close(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func chartMetadata(content []byte) (map[string]any, error) {
	zip, err := gzip.NewReader(bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	defer func() { _ = zip.Close() }()
	reader := tar.NewReader(zip)
	for {
		header, err := reader.Next()
		if err != nil {
			return nil, errors.New("helm chart metadata is missing")
		}
		if strings.Count(header.Name, "/") != 1 || !strings.HasSuffix(header.Name, "/Chart.yaml") {
			continue
		}
		content, err := io.ReadAll(reader)
		if err != nil {
			return nil, err
		}
		var metadata map[string]any
		if err := yaml.Unmarshal(content, &metadata); err != nil {
			return nil, errors.New("helm chart metadata is invalid")
		}
		return metadata, nil
	}
}
