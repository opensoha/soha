package resourcebackend

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
)

func TestLoadHelmChartPrivateHTTPSAndOCI(t *testing.T) {
	archive := helmSourceTestArchive(t)
	config := []byte(`{"name":"app","version":"1.2.3","apiVersion":"v2"}`)
	manifest, err := json.Marshal(map[string]any{
		"schemaVersion": 2, "mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{"mediaType": "application/vnd.cncf.helm.config.v1+json", "digest": helmrelease.SHA256(config), "size": len(config)},
		"layers": []any{map[string]any{"mediaType": "application/vnd.cncf.helm.chart.content.v1.tar+gzip", "digest": helmrelease.SHA256(archive), "size": len(archive)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok || username != "chart-user" || password != "chart-password" {
			w.Header().Set("WWW-Authenticate", `Basic realm="charts"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var data []byte
		switch r.URL.Path {
		case "/index.yaml":
			data = []byte(fmt.Sprintf("apiVersion: v1\nentries:\n  app:\n  - apiVersion: v2\n    name: app\n    version: 1.2.3\n    digest: %s\n    urls: [app-1.2.3.tgz]\n", strings.TrimPrefix(helmrelease.SHA256(archive), "sha256:")))
		case "/app-1.2.3.tgz", "/v2/team/app/blobs/" + helmrelease.SHA256(archive):
			data = archive
		case "/v2/team/app/blobs/" + helmrelease.SHA256(config):
			data = config
		case "/v2/team/app/manifests/1.2.3", "/v2/team/app/manifests/" + helmrelease.SHA256(manifest):
			data = manifest
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		case "/v2/":
			data = []byte("{}")
		default:
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Docker-Content-Digest", helmrelease.SHA256(data))
		w.Header().Set("Content-Length", fmt.Sprint(len(data)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(data)
		}
	}))
	defer server.Close()
	credentials := map[string]string{"CHART_USERNAME": "chart-user", "CHART_PASSWORD": "chart-password", "CHART_CA_CERT": string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))}
	for _, sourceURL := range []string{server.URL, strings.Replace(server.URL, "https://", "oci://", 1) + "/team"} {
		t.Run(sourceURL[:3], func(t *testing.T) {
			source := sohaapi.DeploymentTemplateHelmSource{RepositoryURL: sourceURL, Chart: "app", Version: "1.2.3"}
			result, data, err := (HelmChartLoader{}).LoadChart(context.Background(), source, credentials)
			if err != nil || !bytes.Equal(data, archive) || result.Digest != helmrelease.SHA256(archive) || !result.HasValuesSchema || result.Name != "app" || len(result.DefaultValues) != 1 {
				t.Fatalf("private chart load failed: %+v %v", result, err)
			}
			verifyHelmChartSourceRejections(t, source, credentials)
		})
	}
}

func helmSourceTestArchive(t *testing.T) []byte {
	t.Helper()
	var buffer bytes.Buffer
	zip := gzip.NewWriter(&buffer)
	writer := tar.NewWriter(zip)
	for name, content := range map[string]string{
		"app/Chart.yaml":         "apiVersion: v2\nname: app\nversion: 1.2.3\n",
		"app/values.yaml":        "replicaCount: 1\n",
		"app/values.schema.json": `{"type":"object","properties":{"replicaCount":{"type":"integer","minimum":1}}}`,
	} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: int64(len(content))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
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

func verifyHelmChartSourceRejections(t *testing.T, source sohaapi.DeploymentTemplateHelmSource, credentials map[string]string) {
	t.Helper()
	source.Digest = "sha256:" + strings.Repeat("a", 64)
	if _, _, err := (HelmChartLoader{}).LoadChart(context.Background(), source, credentials); err == nil {
		t.Fatal("accepted changed chart content")
	}
	source.Digest = ""
	if _, _, err := (HelmChartLoader{}).LoadChart(context.Background(), source, map[string]string{"CHART_CA_CERT": credentials["CHART_CA_CERT"]}); err == nil {
		t.Fatal("private chart downloaded without scoped credentials")
	}
}
