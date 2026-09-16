package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"sigs.k8s.io/yaml"
)

func TestCreateResolvedManifestPreservesOriginalUIDWithoutManifestContent(t *testing.T) {
	for _, uid := range []string{"original-uid", ""} {
		t.Run("uid="+uid, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/api/v1/platform/resources" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": sohaapi.KubernetesResourceAgentCreateResult{
					OperationID: "original-operation", Status: sohaapi.KubernetesResourceCreateBatchStatusSucceeded,
					Items: []sohaapi.KubernetesResourceCreateResultItem{{
						Status:      sohaapi.KubernetesResourceCreateResultStatusSucceeded,
						ResourceRef: &sohaapi.KubernetesResourceRef{APIVersion: "v1", Kind: "Secret", Name: "app", Namespace: "platform", UID: uid},
					}},
				}})
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			view, err := client.CreateResolvedManifest(context.Background(), "original-operation", "cluster-a", domainresource.ResolvedCreateManifest{
				Ref:     domainresource.ResourceCreateRef{APIVersion: "v1", Kind: "Secret", Name: "app", Namespace: "platform", Namespaced: true, UID: "caller-uid"},
				Content: "apiVersion: v1\nkind: Secret\nmetadata:\n  name: app\nstringData:\n  token: private-value\n",
			})
			if err != nil {
				t.Fatal(err)
			}
			var receipt struct {
				Metadata struct{ UID string } `json:"metadata"`
			}
			if err := yaml.Unmarshal([]byte(view.Content), &receipt); err != nil || receipt.Metadata.UID != uid {
				t.Fatalf("receipt UID = %q error=%v, want %q", receipt.Metadata.UID, err, uid)
			}
			if strings.Contains(view.Content, "private-value") || strings.Contains(view.Content, "stringData") || strings.Contains(view.Content, "caller-uid") {
				t.Fatalf("receipt includes manifest content: %s", view.Content)
			}
		})
	}
}
