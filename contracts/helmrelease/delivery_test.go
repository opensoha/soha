package helmrelease

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"reflect"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestHelmDeliveryBoundaries(t *testing.T) {
	for _, repository := range []string{"https://charts.example.test", "https://charts.example.test/private/", "oci://registry.example.test/team"} {
		if err := ValidateSource(sohaapi.DeploymentTemplateHelmSource{RepositoryURL: repository, Chart: "app", Version: "1.2.3-beta.1+build"}); err != nil {
			t.Fatal(err)
		}
	}
	for _, repository := range []string{"file:///tmp/chart", "http://example.test", "https://user:pass@example.test", "https://example.test?token=secret", "https://example.test/a/../b", "https://example.test/%2e%2e/b"} {
		if err := ValidateSource(sohaapi.DeploymentTemplateHelmSource{RepositoryURL: repository, Chart: "app", Version: "1.2.3"}); err == nil {
			t.Fatalf("accepted unsafe source %s", repository)
		}
	}
	image := "registry.example.test/app@sha256:" + strings.Repeat("a", 64)
	values := map[string]any{"workers": []any{map[string]any{"image": "old"}}}
	mappings := []sohaapi.HelmImageMapping{{ContainerName: "main", Path: "/workers/0/image", Value: sohaapi.HelmImageMappingValueImage}}
	result, err := MapImages(values, mappings, map[string]string{"main": image})
	if err != nil || !reflect.DeepEqual(result, map[string]any{"workers": []any{map[string]any{"image": image}}}) || !reflect.DeepEqual(values, map[string]any{"workers": []any{map[string]any{"image": "old"}}}) {
		t.Fatalf("image mapping did not preserve input: %v", err)
	}
	for _, invalid := range []string{"/workers/2/image", "/workers/01/image", "/workers/-/image", "/workers/0/image/child", "/bad~2/path"} {
		mappings[0].Path = invalid
		if _, err := MapImages(values, mappings, map[string]string{"main": image}); err == nil {
			t.Fatalf("accepted invalid image path %s", invalid)
		}
	}
	mappings[0].Path = "/image"
	if _, err := MapImages(nil, mappings, map[string]string{"main": image}); err != nil {
		t.Fatal(err)
	}
	if _, err := MapImages(nil, mappings, map[string]string{"main": "registry/app:latest"}); err == nil {
		t.Fatal("accepted mutable image")
	}
	valid := testChartArchive(t, []tar.Header{{Name: "app/Chart.yaml", Mode: 0600, Size: 1}})
	if err := ValidateChartArchive(valid); err != nil {
		t.Fatal(err)
	}
	for _, headers := range [][]tar.Header{
		{{Name: "../Chart.yaml", Size: 1}},
		{{Name: "app/Chart.yaml", Size: 1}, {Name: "app/Chart.yaml", Size: 1}},
		{{Name: "app/Chart.yaml", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}},
		{{Name: "app/Chart.yaml", Size: 1}, {Name: "app/large", Size: MaxChartExpandedBytes}},
		{{Name: "app/Chart.yaml", Size: 1}, {Name: "app/crds/example.yaml", Size: 1}},
		{{Name: "app/Chart.yaml", Size: 1}, {Name: "app/charts/child/crds/example.yaml", Size: 1}},
	} {
		if err := ValidateChartArchive(testChartArchive(t, headers)); err == nil {
			t.Fatal("accepted unsafe archive")
		}
	}
	budget, files := int64(100), 0
	if err := validateChartArchive(valid, &budget, &files, 0); err == nil {
		t.Fatal("expanded archive budget was not enforced")
	}
	for _, schema := range []string{
		`{"$ref":"file:///etc/config.json"}`,
		`{"properties":{"value":{"$ref":"https://example.test/schema"}}}`,
		`{"allOf":[{"$dynamicRef":"../other.json#/$defs/value"}]}`,
		`{"$schema":"https://example.test/dialect"}`,
	} {
		if err := validateValuesSchema([]byte(schema)); err == nil {
			t.Fatalf("accepted external schema reference: %s", schema)
		}
	}
	if err := validateValuesSchema([]byte(`{"$schema":"http://json-schema.org/draft-07/schema#","$defs":{"value":{"type":"string"}},"properties":{"value":{"$ref":"#/$defs/value"}}}`)); err != nil {
		t.Fatal(err)
	}
}

func testChartArchive(t *testing.T, headers []tar.Header) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zip)
	for _, header := range headers {
		if err := writer.WriteHeader(&header); err != nil {
			t.Fatal(err)
		}
		if header.Size > 0 {
			if _, err := writer.Write(bytes.Repeat([]byte("x"), int(header.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zip.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
