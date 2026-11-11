package runtime

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	contractresource "github.com/opensoha/soha-contracts/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

type terminalSizes struct{}

func (terminalSizes) Next(context.Context) *contractresource.NativeTerminalSize {
	return &contractresource.NativeTerminalSize{Width: 120, Height: 40}
}

func TestNativeTerminalUsesPTYMergedOutputAndExactShell(t *testing.T) {
	client := fake.NewClientset(executionFixturePod())
	stdin := []byte{0, 255, '\n'}
	var output bytes.Buffer
	factory := func(_, _ string, options corev1.PodExecOptions) (remotecommand.Executor, error) {
		if !options.TTY || !options.Stdin || !options.Stdout || options.Stderr || options.Container != "main" || !reflect.DeepEqual(options.Command, []string{"/bin/bash"}) {
			t.Fatalf("unsafe PTY options: %+v", options)
		}
		return podExecutorStub{run: func(_ context.Context, streams remotecommand.StreamOptions) error {
			if streams.Stderr != nil || !streams.Tty {
				t.Fatal("PTY must negotiate TTY and merged output in the stream transport")
			}
			size := streams.TerminalSizeQueue.Next()
			if size == nil || size.Width != 120 || size.Height != 40 {
				t.Fatal("resize lost")
			}
			data, _ := io.ReadAll(streams.Stdin)
			if !bytes.Equal(data, stdin) {
				t.Fatal("binary stdin changed")
			}
			_, err := streams.Stdout.Write([]byte{255, 0, 27})
			return err
		}}, nil
	}
	result, err := StreamPodTerminal(t.Context(), client, nil, "test", "fixture", contractresource.PodTerminalInput{ExpectedUID: "reviewed", Shell: "bash"}, io.NopCloser(bytes.NewReader(stdin)), &output, terminalSizes{}, factory)
	if err != nil || result.Status != "succeeded" || result.InputBytes != 3 || result.OutputBytes != 3 || !bytes.Equal(output.Bytes(), []byte{255, 0, 27}) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if err = result.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestNativeTerminalExitCancelTimeoutAndChangedTarget(t *testing.T) {
	for _, scenario := range []string{"exit", "transport", "changed", "canceled", "deadline", "invalid-exit"} {
		t.Run(scenario, func(t *testing.T) {
			client := fake.NewClientset(executionFixturePod())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			factory := func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
				return podExecutorStub{run: func(runCtx context.Context, _ remotecommand.StreamOptions) error {
					switch scenario {
					case "exit":
						return utilexec.CodeExitError{Err: errors.New("PRIVATE"), Code: 42}
					case "transport":
						return errors.New("PRIVATE")
					case "invalid-exit":
						return utilexec.CodeExitError{Err: errors.New("PRIVATE"), Code: 0}
					case "changed":
						pod, _ := client.CoreV1().Pods("test").Get(ctx, "fixture", metav1.GetOptions{})
						pod.UID = "replacement"
						_, err := client.CoreV1().Pods("test").Update(ctx, pod, metav1.UpdateOptions{})
						return err
					case "canceled":
						cancel()
						return nil
					case "deadline":
						<-runCtx.Done()
						return nil
					}
					return nil
				}}, nil
			}
			input := contractresource.PodTerminalInput{ExpectedUID: "reviewed", TimeoutSeconds: 1}
			result, err := StreamPodTerminal(ctx, client, nil, "test", "fixture", input, io.NopCloser(strings.NewReader("")), io.Discard, terminalSizes{}, factory)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{"exit": "failed", "transport": "transport_lost", "changed": "unknown", "canceled": "canceled", "deadline": "timed_out", "invalid-exit": "transport_lost"}[scenario]
			if result.Status != want {
				t.Fatalf("result=%+v want=%s", result, want)
			}
			if err = result.Validate(); err != nil {
				t.Fatal(err)
			}
			if scenario == "canceled" && len(client.Actions()) != 1 {
				t.Fatal("read Kubernetes after cancellation")
			}
		})
	}
}

func TestNativeTerminalRejectsUIDBeforeOpeningAndRetainsForbidden(t *testing.T) {
	client := fake.NewClientset(executionFixturePod())
	called := false
	factory := func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
		called = true
		return nil, errors.New("must not call")
	}
	_, err := StreamPodTerminal(t.Context(), client, nil, "test", "fixture", contractresource.PodTerminalInput{ExpectedUID: "changed"}, io.NopCloser(strings.NewReader("")), io.Discard, terminalSizes{}, factory)
	if !apierrors.IsConflict(err) || called {
		t.Fatalf("err=%v called=%v", err, called)
	}
	factory = func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
		return podExecutorStub{run: func(context.Context, remotecommand.StreamOptions) error { return apierrors.NewUnauthorized("PRIVATE") }}, nil
	}
	_, err = StreamPodTerminal(t.Context(), client, nil, "test", "fixture", contractresource.PodTerminalInput{ExpectedUID: "reviewed"}, io.NopCloser(strings.NewReader("")), io.Discard, terminalSizes{}, factory)
	if !apierrors.IsUnauthorized(err) {
		t.Fatalf("lost API denial: %v", err)
	}
}

func TestNativeTerminalBoundsProviderIOAndUnblocksStdinOnDeadline(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	reader := &terminalInputReader{reader: bytes.NewReader(make([]byte, contractresource.NativePodTerminalMaxInputBytes+1)), cancel: cancel}
	if n, err := io.Copy(io.Discard, reader); n != contractresource.NativePodTerminalMaxInputBytes || !errors.Is(err, errTerminalInputLimit) {
		t.Fatalf("input n=%d err=%v", n, err)
	}
	if ctx.Err() == nil {
		t.Fatal("input limit did not cancel")
	}
	ctx, cancel = context.WithCancel(t.Context())
	defer cancel()
	writer := &terminalOutputWriter{writer: io.Discard, cancel: cancel}
	writer.count.Store(contractresource.NativePodTerminalMaxOutputBytes - 2)
	if n, err := writer.Write([]byte("abcd")); n != 2 || !errors.Is(err, errTerminalOutputLimit) {
		t.Fatalf("output n=%d err=%v", n, err)
	}
	if ctx.Err() == nil {
		t.Fatal("output limit did not cancel")
	}
	client := fake.NewClientset(executionFixturePod())
	pipe, peer := io.Pipe()
	defer func() { _ = peer.Close() }()
	finished := make(chan error, 1)
	factory := func(_, _ string, _ corev1.PodExecOptions) (remotecommand.Executor, error) {
		return podExecutorStub{run: func(_ context.Context, streams remotecommand.StreamOptions) error {
			_, err := io.Copy(io.Discard, streams.Stdin)
			finished <- err
			return err
		}}, nil
	}
	result, err := StreamPodTerminal(t.Context(), client, nil, "test", "fixture", contractresource.PodTerminalInput{ExpectedUID: "reviewed", TimeoutSeconds: 1}, pipe, io.Discard, terminalSizes{}, factory)
	if err != nil || result.Status != "timed_out" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("stdin remained blocked")
	}
}
