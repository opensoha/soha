package kubernetes

import (
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	kyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

func TestInstallationWorkbenchActionConformance(t *testing.T) {
	for _, file := range []string{"../../../configs/agent.config.yaml", "../../../deploy/kubernetes/base/soha-agent.yaml"} {
		content, err := os.ReadFile(file) //nolint:gosec // Fixed repository-owned installation fixture paths above.
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(file, "/deploy/") {
			decoder := kyaml.NewYAMLOrJSONDecoder(strings.NewReader(string(content)), 4096)
			content = nil
			for {
				var object struct {
					Kind  string            `json:"kind"`
					Data  map[string]string `json:"data"`
					Rules []struct {
						APIGroups []string `json:"apiGroups"`
						Resources []string `json:"resources"`
						Verbs     []string `json:"verbs"`
					} `json:"rules"`
				}
				if err := decoder.Decode(&object); err == io.EOF {
					break
				} else if err != nil {
					t.Fatal(err)
				}
				if object.Kind == "ClusterRole" {
					for _, rule := range object.Rules {
						if slices.Contains(rule.APIGroups, "apiextensions.k8s.io") && slices.Contains(rule.Resources, "customresourcedefinitions") {
							for _, verb := range rule.Verbs {
								if verb != "get" && verb != "list" && verb != "watch" {
									t.Fatalf("starter implicitly granted CRD definition %s", verb)
								}
							}
						}
					}
				}
				if object.Kind == "ConfigMap" {
					content = []byte(object.Data["agent.config.yaml"])
				}
			}
		}
		var config struct {
			Security struct {
				Actions []string `json:"allowed_actions"`
			} `json:"security"`
		}
		if err := yaml.Unmarshal(content, &config); err != nil {
			t.Fatal(err)
		}
		actual := []string{}
		for _, action := range config.Security.Actions {
			if strings.HasPrefix(action, "platform.") || action == "runtime.execution_tasks.cancel" {
				actual = append(actual, action)
			}
		}
		expected, _ := contractresource.KubernetesAgentProfileActions("workbench")
		slices.Sort(actual)
		slices.Sort(expected)
		if !slices.Equal(actual, expected) {
			t.Fatalf("%s action drift: got %v; want %v", file, actual, expected)
		}
	}
}
