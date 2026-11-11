package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func TestRolloutWithKubernetes(t *testing.T) {
	kubeconfig := os.Getenv("SOHA_ROLLOUT_TEST_KUBECONFIG")
	if kubeconfig == "" {
		t.Skip("set the explicit loopback Rollouts/Traefik fixture kubeconfig and SOHA_ROLLOUT_TEST_IMAGE")
	}
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	operatorRequire(t, err)
	endpoint, err := url.Parse(config.Host)
	if err != nil || endpoint.Hostname() != "127.0.0.1" {
		t.Fatal("only the isolated loopback fixture cluster is supported")
	}
	config.Timeout = 10 * time.Second
	client, err := dynamic.NewForConfig(config)
	operatorRequire(t, err)
	kube, err := kubernetes.NewForConfig(config)
	operatorRequire(t, err)
	namespace := "soha-workflow-r6-progressive"
	for _, strategy := range []string{"blueGreen", "canary"} {
		t.Run(strategy, func(t *testing.T) {
			name := "r6-runtime-" + strings.ToLower(strategy)
			base := rolloutFixture(t, strategy)
			encoded, _ := json.Marshal(base)
			encoded = []byte(strings.ReplaceAll(strings.ReplaceAll(string(encoded), "web", name), "demo", namespace))
			// The web provider key is a protocol field, not a resource name.
			encoded = []byte(strings.ReplaceAll(string(encoded), `"`+name+`":{`, `"web":{`))
			operatorRequire(t, json.Unmarshal(encoded, &base))
			for _, document := range base {
				if document.GetKind() == "IngressRoute" {
					operatorRequire(t, unstructured.SetNestedStringSlice(document.Object, []string{"web"}, "spec", "entryPoints"))
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			owner := "opensoha-manifest/" + name
			operation := name + "-" + time.Now().UTC().Format("150405.000000")
			options := metav1.PatchOptions{FieldManager: owner}
			documents := func(version string) []*unstructured.Unstructured {
				items := make([]*unstructured.Unstructured, len(base))
				for i, item := range base {
					items[i] = item.DeepCopy()
				}
				rate := 1
				if version == "bad" {
					rate = 0
				}
				response := fmt.Sprintf(`{"version":%q,"successRate":%d}`, version, rate)
				command := fmt.Sprintf("cat > /tmp/serve-http <<'SERVER'\n#!/bin/sh\nwhile IFS= read -r line; do [ \"$line\" = \"$(printf '\\r')\" ] && break; done\nprintf 'HTTP/1.1 200 OK\\r\\nContent-Type: application/json\\r\\nContent-Length: %d\\r\\nConnection: close\\r\\n\\r\\n%s'\nSERVER\nchmod 700 /tmp/serve-http\nexec nc -lk -p 8080 -e /tmp/serve-http", len(response), response)
				containers := []any{map[string]any{"name": "app", "image": os.Getenv("SOHA_ROLLOUT_TEST_IMAGE"), "imagePullPolicy": "IfNotPresent", "env": []any{}, "command": []any{"/bin/sh", "-c"}, "args": []any{command}, "ports": []any{map[string]any{"containerPort": int64(8080)}}, "readinessProbe": map[string]any{"httpGet": map[string]any{"path": "/", "port": int64(8080)}, "periodSeconds": int64(2)}}}
				operatorRequire(t, unstructured.SetNestedSlice(items[0].Object, containers, "spec", "template", "spec", "containers"))
				return items
			}
			t.Cleanup(func() {
				if t.Failed() {
					t.Log("failed fixture retained:", name)
					return
				}
				cleanup, stop := context.WithTimeout(context.Background(), 20*time.Second)
				defer stop()
				for _, document := range base {
					gvk := document.GroupVersionKind()
					_ = client.Resource(rolloutResourceTypes[gvk.Group+"/"+gvk.Kind]).Namespace(namespace).Delete(cleanup, document.GetName(), metav1.DeleteOptions{})
				}
			})
			readHTTP := func(service string) (string, error) {
				body, err := kube.CoreV1().RESTClient().Get().Namespace(namespace).Resource("services").Name(service + ":8080").SubResource("proxy").DoRaw(ctx)
				if err != nil {
					return "", err
				}
				var response struct {
					Version string `json:"version"`
				}
				err = json.Unmarshal(body, &response)
				return response.Version, err
			}
			checkHTTP := func(service, version string) {
				err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 15*time.Second, true, func(context.Context) (bool, error) {
					actual, err := readHTTP(service)
					return err == nil && actual == version, nil
				})
				operatorRequire(t, err)
			}
			control := func(items []*unstructured.Unstructured, op, action string) {
				err := wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
					observed, err := ObserveRollout(ctx, client, items, owner, op)
					if err != nil {
						return false, err
					}
					_, err = ControlRollout(ctx, client, items, owner, op, sohaapi.ProgressiveRolloutControlInput{Action: sohaapi.ProgressiveRolloutControlInputAction(action), UID: observed.State.UID, ResourceVersion: observed.State.ResourceVersion})
					if apierrors.IsConflict(err) {
						return false, nil
					}
					return err == nil, err
				})
				operatorRequire(t, err)
			}
			preflight := options
			preflight.DryRun = []string{metav1.DryRunAll}
			_, err := ApplyRollout(ctx, client, documents("v1"), preflight, operation+"-preflight")
			operatorRequire(t, err)
			live, err := ApplyRollout(ctx, client, documents("v1"), options, operation+"-v1")
			if err != nil {
				t.Fatalf("initial rollout: %v; live=%v", err, live)
			}
			uid := live.GetUID()
			checkHTTP(name+"-stable", "v1")
			if strategy == "canary" {
				checkHTTP("r6-traefik", "v1")
			}
			for _, version := range []string{"v2", "v1"} {
				items := documents(version)
				op := operation + "-" + version + "-update"
				finished := make(chan error, 1)
				go func() { _, err := ApplyRollout(ctx, client, items, options, op); finished <- err }()
				var paused RolloutObservation
				completed := false
				err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 70*time.Second, true, func(context.Context) (bool, error) {
					select {
					case err := <-finished:
						if err == nil && version == "v1" {
							completed = true
							return true, nil
						}
						if err == nil {
							return false, fmt.Errorf("update completed without a manual pause")
						}
						return false, err
					default:
					}
					observed, err := ObserveRollout(ctx, client, items, owner, op)
					if err != nil {
						return false, nil
					}
					paused = observed
					if !paused.State.Paused || paused.State.ObservedGeneration == nil || *paused.State.ObservedGeneration != paused.State.Generation {
						return false, nil
					}
					if strategy == "blueGreen" {
						if len(paused.State.Metrics) == 0 {
							return false, nil
						}
						for _, metric := range paused.State.Metrics {
							if metric.Phase != "Successful" {
								return false, nil
							}
						}
					}
					return true, nil
				})
				operatorRequire(t, err)
				if completed {
					observed, err := ObserveRollout(ctx, client, items, owner, op)
					operatorRequire(t, err)
					if observed.Health != "healthy" || observed.State.UID != string(uid) {
						t.Fatal("native fast rollback lost health or identity")
					}
					checkHTTP(name+"-stable", version)
					if strategy == "canary" {
						checkHTTP("r6-traefik", version)
					}
					t.Logf("%s native fast rollback restored known healthy v1 and actual HTTP", strategy)
					continue
				}
				previous := "v1"
				if version == "v1" {
					previous = "v2"
				}
				checkHTTP(name+"-stable", previous)
				checkHTTP(name+"-preview", version)
				if strategy == "canary" {
					if paused.State.CanaryWeight == nil || *paused.State.CanaryWeight != 20 || paused.State.StableWeight == nil || *paused.State.StableWeight != 80 {
						t.Fatalf("wrong native traffic weights: %+v", paused.State)
					}
					counts := map[string]int{}
					for i := 0; i < 80; i++ {
						version, err := readHTTP("r6-traefik")
						operatorRequire(t, err)
						counts[version]++
					}
					if counts[previous] == 0 || counts[version] == 0 || len(counts) != 2 {
						t.Fatalf("actual routed traffic did not reach both revisions: %v", counts)
					}
					t.Logf("actual routed traffic at 20 percent: %v", counts)
				}
				control(items, op, "promote")
				select {
				case err := <-finished:
					operatorRequire(t, err)
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
				observed, err := ObserveRollout(ctx, client, items, owner, op)
				operatorRequire(t, err)
				if observed.Health != "healthy" || observed.State.UID != string(uid) {
					t.Fatalf("updated rollout is not healthy with the same UID: %+v", observed)
				}
				if len(observed.State.Metrics) == 0 {
					t.Fatal("completed rollout lost its native analysis evidence")
				}
				for _, metric := range observed.State.Metrics {
					if metric.Phase != "Successful" || metric.Count != metric.TargetCount || metric.StartedAt == "" || metric.FinishedAt == "" || metric.StartedAt >= metric.FinishedAt {
						t.Fatalf("completed rollout lost its full successful analysis window: %+v", metric)
					}
				}
				checkHTTP(name+"-stable", version)
				if strategy == "canary" {
					checkHTTP("r6-traefik", version)
				}
				t.Logf("%s version %s passed actual HTTP, analysis and same-UID reconciliation", strategy, version)
			}
			for _, mode := range []string{"bad", "cancel"} {
				items, op := documents(mode), operation+"-"+mode
				runCtx, stop := context.WithCancel(ctx)
				finished := make(chan error, 1)
				go func() { _, err := ApplyRollout(runCtx, client, items, options, op); finished <- err }()
				if mode == "cancel" || strategy == "canary" {
					err := wait.PollUntilContextTimeout(ctx, 250*time.Millisecond, 70*time.Second, true, func(context.Context) (bool, error) {
						select {
						case err := <-finished:
							return false, fmt.Errorf("rollout finished before control: %v", err)
						default:
						}
						observed, err := ObserveRollout(ctx, client, items, owner, op)
						return err == nil && observed.State.Paused && observed.State.ObservedGeneration != nil && *observed.State.ObservedGeneration == observed.State.Generation, nil
					})
					operatorRequire(t, err)
					if mode == "bad" {
						control(items, op, "promote")
					} else {
						control(items, op, "pause")
						operatorRequire(t, wait.PollUntilContextTimeout(ctx, 200*time.Millisecond, 10*time.Second, true, func(context.Context) (bool, error) {
							observed, err := ObserveRollout(ctx, client, items, owner, op)
							return err == nil && observed.State.ObservedGeneration != nil && *observed.State.ObservedGeneration == observed.State.Generation, nil
						}))
						control(items, op, "promote") // Remove spec.paused; retain the native promotion pause.
						stop()
					}
				}
				select {
				case err := <-finished:
					stop()
					if err == nil || errors.Is(err, ErrRolloutStopUnconfirmed) {
						t.Fatalf("%s did not fail with confirmed native stop: %v", mode, err)
					}
				case <-ctx.Done():
					stop()
					t.Fatal(ctx.Err())
				}
				observed, err := ObserveRollout(ctx, client, items, owner, op)
				operatorRequire(t, err)
				if !rolloutStopped(observed.State) || observed.State.UID != string(uid) {
					t.Fatalf("%s left active traffic: %+v", mode, observed.State)
				}
				checkHTTP(name+"-stable", "v1")
				if strategy == "canary" {
					checkHTTP("r6-traefik", "v1")
				}
				_, err = ApplyRollout(ctx, client, documents("v1"), options, op+"-restore")
				operatorRequire(t, err)
				checkHTTP(name+"-stable", "v1")
				t.Logf("%s %s confirmed stable traffic, retained failed desired version, then explicit v1 restore", strategy, mode)
			}
		})
	}
}
