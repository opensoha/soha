package runtime

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	contractresource "github.com/opensoha/soha/contracts/resource"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	utilexec "k8s.io/client-go/util/exec"
)

func InspectPodExecutionTarget(ctx context.Context, client kubernetes.Interface, namespace, name, expectedUID, container string) (contractresource.PodExecutionTarget, error) {
	var empty contractresource.PodExecutionTarget
	if client == nil || len(validation.IsDNS1123Label(namespace)) > 0 || len(validation.IsDNS1123Subdomain(name)) > 0 || expectedUID == "" || len(expectedUID) > 256 || strings.IndexFunc(expectedUID, func(r rune) bool { return unicode.IsControl(r) || unicode.IsSpace(r) }) >= 0 {
		return empty, apierrors.NewBadRequest("valid namespace, Pod name and reviewed UID are required")
	}
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return empty, err
	}
	if pod.Name != name || pod.Namespace != namespace || string(pod.UID) != expectedUID || pod.DeletionTimestamp != nil {
		return empty, apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, name, fmt.Errorf("pod identity changed or deletion was requested"))
	}
	if container == "" {
		container = pod.Annotations["kubectl.kubernetes.io/default-container"]
		if container == "" && len(pod.Spec.Containers) == 1 {
			container = pod.Spec.Containers[0].Name
		}
	}
	found := false
	for _, item := range append(append([]corev1.Container(nil), pod.Spec.Containers...), pod.Spec.InitContainers...) {
		if item.Name == container {
			found = true
		}
	}
	for _, item := range pod.Spec.EphemeralContainers {
		if item.Name == container {
			found = true
		}
	}
	if container == "" || !found {
		return empty, apierrors.NewBadRequest("select one existing Pod container")
	}
	return contractresource.PodExecutionTarget{Namespace: namespace, PodName: name, UID: expectedUID, Container: container, CheckedAt: time.Now().UTC()}, nil
}

type PodExecutorFactory func(namespace, name string, options corev1.PodExecOptions) (remotecommand.Executor, error)

// ExecutePodArgv checks UID before connecting and again after execution.
// Kubernetes exec has no atomic UID precondition; this function never claims
// otherwise, and never retries an uncertain command.
func ExecutePodArgv(ctx context.Context, client kubernetes.Interface, config *rest.Config, namespace, name string, input contractresource.PodArgvInput, factory PodExecutorFactory) (contractresource.PodArgvResult, error) {
	var result contractresource.PodArgvResult
	input, err := input.Normalized()
	if err != nil {
		return result, apierrors.NewBadRequest(err.Error())
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	runCtx, cancel := context.WithTimeout(ctx, time.Duration(input.TimeoutSeconds)*time.Second)
	defer cancel()
	target, err := InspectPodExecutionTarget(runCtx, client, namespace, name, input.ExpectedUID, input.Container)
	if err != nil {
		return result, err
	}
	if err := runCtx.Err(); err != nil {
		return result, err
	}
	stdin, _ := base64.StdEncoding.Strict().DecodeString(input.StdinBase64)
	options := corev1.PodExecOptions{Container: target.Container, Command: input.Argv, Stdin: len(stdin) > 0, Stdout: true, Stderr: true, TTY: false}
	if factory == nil {
		if config == nil || client.CoreV1().RESTClient() == nil {
			return result, apierrors.NewBadRequest("Pod exec transport is unavailable")
		}
		factory = func(namespace, name string, options corev1.PodExecOptions) (remotecommand.Executor, error) {
			request := client.CoreV1().RESTClient().Post().Resource("pods").Name(name).Namespace(namespace).SubResource("exec")
			request.VersionedParams(&options, scheme.ParameterCodec)
			return newPodExecutionExecutor(config, name, request.URL())
		}
	}
	executor, err := factory(namespace, name, options)
	if err != nil {
		return result, err
	}
	budget := &podOutputBudget{maximum: int(input.MaxOutputBytes)}
	stdout, stderr := &podOutputBuffer{budget: budget}, &podOutputBuffer{budget: budget}
	streamOptions := remotecommand.StreamOptions{Stdout: stdout, Stderr: stderr}
	if len(stdin) > 0 {
		streamOptions.Stdin = bytes.NewReader(stdin)
	}
	execErr := executor.StreamWithContext(runCtx, streamOptions)
	// An explicit API rejection of the upgrade is a denied operation, rather
	// than a process exit or an unclassified transport failure.
	if apierrors.IsForbidden(execErr) || apierrors.IsUnauthorized(execErr) || apierrors.IsNotFound(execErr) || apierrors.IsBadRequest(execErr) {
		return result, execErr
	}
	outData, outBytes, outTruncated := stdout.snapshot()
	errData, errBytes, errTruncated := stderr.snapshot()
	outValue, outEncoding := encodePodExecutionStream(outData)
	errValue, errEncoding := encodePodExecutionStream(errData)
	output := contractresource.PodExecutionOutput{Status: "succeeded", Stdout: outValue, Stderr: errValue, StdoutEncoding: outEncoding, StderrEncoding: errEncoding, StdoutBytes: outBytes, StderrBytes: errBytes, MaxOutputBytes: input.MaxOutputBytes, StdoutTruncated: outTruncated, StderrTruncated: errTruncated, UIDVerifiedBeforeStart: true}
	var remoteExit utilexec.ExitError
	switch {
	case execErr == nil:
		code := int64(0)
		output.ExitCode = &code
	case errors.As(execErr, &remoteExit) && remoteExit.Exited() && remoteExit.ExitStatus() >= 0:
		code := int64(remoteExit.ExitStatus())
		if code > 4294967295 {
			output.Status = "transport_lost"
			output.ReasonCode = "invalid_exit_status"
		} else {
			output.ExitCode = &code
			output.Status = "failed"
			output.ReasonCode = "process_exited"
		}
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		output.Status = "timed_out"
		output.ReasonCode = "deadline_exceeded"
	case errors.Is(runCtx.Err(), context.Canceled):
		output.Status = "canceled"
		output.ReasonCode = "request_canceled"
	default:
		output.Status = "transport_lost"
		output.ReasonCode = "transport_lost"
	}
	// No new provider read after cancellation/revocation. A normal completion
	// receives a short final identity check, which can never repeat the command.
	if runCtx.Err() == nil {
		verifyCtx, verifyCancel := context.WithTimeout(runCtx, 3*time.Second)
		defer verifyCancel()
		_, verifyErr := InspectPodExecutionTarget(verifyCtx, client, namespace, name, input.ExpectedUID, target.Container)
		if verifyErr != nil {
			if apierrors.IsConflict(verifyErr) || apierrors.IsNotFound(verifyErr) {
				output.TargetChanged = true
				output.ReasonCode = "target_changed_after_execution"
			} else {
				output.ReasonCode = "target_verification_unavailable"
			}
			output.Status = "unknown"
		}
	}
	result.Target, result.Output = target, output
	return result, nil
}

func encodePodExecutionStream(data []byte) (string, string) {
	if utf8.Valid(data) {
		return string(data), "utf-8"
	}
	return base64.StdEncoding.EncodeToString(data), "base64"
}

type podOutputBudget struct {
	mu            sync.Mutex
	maximum, used int
}
type podOutputBuffer struct {
	budget    *podOutputBudget
	data      []byte
	total     int64
	truncated bool
}

func (b *podOutputBuffer) Write(data []byte) (int, error) {
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	b.total += int64(len(data))
	keep := min(len(data), b.budget.maximum-b.budget.used)
	b.data = append(b.data, data[:keep]...)
	b.budget.used += keep
	if keep < len(data) {
		b.truncated = true
	}
	return len(data), nil
}

func (b *podOutputBuffer) snapshot() ([]byte, int64, bool) {
	b.budget.mu.Lock()
	defer b.budget.mu.Unlock()
	return append([]byte(nil), b.data...), b.total, b.truncated
}
