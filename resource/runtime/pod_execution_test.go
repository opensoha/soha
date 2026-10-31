package runtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	contractresource "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

type podExecutorStub struct {
	run func(context.Context, remotecommand.StreamOptions) error
}

func (s podExecutorStub) Stream(options remotecommand.StreamOptions) error {
	return s.run(context.Background(), options)
}
func (s podExecutorStub) StreamWithContext(ctx context.Context, options remotecommand.StreamOptions) error {
	return s.run(ctx, options)
}

func executionFixturePod() *corev1.Pod {
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "fixture", Namespace: "test", UID: types.UID("reviewed")}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "main"}}}}
}

func TestPodArgvPreservesUpgradeHTTPDenialWithoutReadingProviderBody(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				if strings.HasSuffix(request.URL.Path, "/exec") {
					w.WriteHeader(code)
					_, _ = io.WriteString(w, strings.Repeat("PRIVATE_PROVIDER_MARKER", 5000))
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(executionFixturePod())
			}))
			defer server.Close()
			config := &rest.Config{Host: server.URL}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				t.Fatal(err)
			}
			_, err = ExecutePodArgv(t.Context(), client, config, "test", "fixture", contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"true"}}, nil)
			var status apierrors.APIStatus
			if !errors.As(err, &status) || int(status.Status().Code) != code || strings.Contains(err.Error(), "PRIVATE_PROVIDER_MARKER") {
				t.Fatalf("upgrade lost its safe HTTP category: %v", err)
			}
		})
	}
}

func TestPodArgvUsesExactArgumentsAndCombinedOutputBudget(t *testing.T) {
	pod := executionFixturePod()
	client := fake.NewClientset(pod)
	input := contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"printf", "; $(literal)\n", ""}, StdinBase64: base64.StdEncoding.EncodeToString([]byte("input")), MaxOutputBytes: 8}
	calls := 0
	factory := func(namespace, name string, options corev1.PodExecOptions) (remotecommand.Executor, error) {
		calls++
		if namespace != "test" || name != "fixture" || !reflect.DeepEqual(options.Command, input.Argv) || options.Container != "main" || options.TTY || !options.Stdin || !options.Stdout || !options.Stderr {
			t.Fatalf("changed exec options: %+v", options)
		}
		return podExecutorStub{run: func(_ context.Context, streams remotecommand.StreamOptions) error {
			data, _ := io.ReadAll(streams.Stdin)
			if string(data) != "input" {
				t.Fatal("stdin changed")
			}
			_, _ = io.WriteString(streams.Stdout, "123456")
			_, _ = io.WriteString(streams.Stderr, "abcdef")
			return nil
		}}, nil
	}
	result, err := ExecutePodArgv(t.Context(), client, nil, "test", "fixture", input, factory)
	if err != nil || calls != 1 || result.Output.Status != "succeeded" || result.Output.ExitCode == nil || *result.Output.ExitCode != 0 || result.Output.Stdout != "123456" || result.Output.Stderr != "ab" || result.Output.StdoutBytes != 6 || result.Output.StderrBytes != 6 || result.Output.StdoutTruncated || !result.Output.StderrTruncated || result.Target.UID != "reviewed" {
		t.Fatalf("result=%+v err=%v calls=%d", result, err, calls)
	}
}

func TestPodArgvRetainsKnownExitCodeButNeverEchoesTransportErrors(t *testing.T) {
	for _, scenario := range []string{"exit", "transport", "changed", "binary", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			client := fake.NewClientset(executionFixturePod())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			input := contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"diagnostic"}, MaxOutputBytes: 1}
			factory := func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
				return podExecutorStub{run: func(_ context.Context, streams remotecommand.StreamOptions) error {
					switch scenario {
					case "exit":
						return utilexec.CodeExitError{Err: errors.New("PRIVATE_VALUE"), Code: 42}
					case "transport":
						return errors.New("PRIVATE_VALUE")
					case "changed":
						pod, _ := client.CoreV1().Pods("test").Get(t.Context(), "fixture", metav1.GetOptions{})
						pod.UID = "replacement"
						_, err := client.CoreV1().Pods("test").Update(t.Context(), pod, metav1.UpdateOptions{})
						return err
					case "binary":
						_, _ = streams.Stdout.Write([]byte{255, 254})
						return nil
					case "canceled":
						cancel()
						return context.Canceled
					default:
						return nil
					}
				}}, nil
			}
			result, err := ExecutePodArgv(ctx, client, nil, "test", "fixture", input, factory)
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), "PRIVATE_VALUE") {
				t.Fatal("transport error leaked")
			}
			switch scenario {
			case "exit":
				if result.Output.Status != "failed" || result.Output.ExitCode == nil || *result.Output.ExitCode != 42 {
					t.Fatalf("exit=%+v", result.Output)
				}
			case "transport":
				if result.Output.Status != "transport_lost" || result.Output.ExitCode != nil {
					t.Fatalf("transport=%+v", result.Output)
				}
			case "changed":
				if result.Output.Status != "unknown" || !result.Output.TargetChanged {
					t.Fatalf("target=%+v", result.Output)
				}
			case "binary":
				data, err := contractresource.DecodePodExecutionStream(result.Output.Stdout, result.Output.StdoutEncoding)
				if err != nil || len(data) != 1 || data[0] != 255 || !result.Output.StdoutTruncated {
					t.Fatalf("binary=%+v err=%v", result.Output, err)
				}
			case "canceled":
				if result.Output.Status != "canceled" || result.Output.ExitCode != nil || len(client.Actions()) != 1 {
					t.Fatalf("cancel=%+v actions=%d", result.Output, len(client.Actions()))
				}
			}
		})
	}
}

func TestPodArgvRejectsChangedUIDAndAmbiguousContainerBeforeFactory(t *testing.T) {
	for _, scenario := range []string{"uid", "container", "deleting", "canceled"} {
		t.Run(scenario, func(t *testing.T) {
			pod := executionFixturePod()
			if scenario == "uid" {
				pod.UID = "replacement"
			}
			if scenario == "container" {
				pod.Spec.Containers = append(pod.Spec.Containers, corev1.Container{Name: "other"})
			}
			if scenario == "deleting" {
				now := metav1.Now()
				pod.DeletionTimestamp = &now
			}
			client := fake.NewClientset(pod)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if scenario == "canceled" {
				cancel()
			}
			calls := 0
			factory := func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
				calls++
				return nil, errors.New("must not open exec")
			}
			_, err := ExecutePodArgv(ctx, client, nil, "test", "fixture", contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"true"}}, factory)
			if err == nil || calls != 0 {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
			if scenario == "uid" && !apierrors.IsConflict(err) {
				t.Fatal(err)
			}
		})
	}
}
