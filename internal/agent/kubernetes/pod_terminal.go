package kubernetes

import (
	"context"
	"io"
	"net/http"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
)

func (c *Client) StreamPodTerminal(ctx context.Context, namespace, name, container, shell string, stdin io.Reader, stdout, stderr io.Writer, sizeQueue remotecommand.TerminalSizeQueue, expectedUID string) error {
	if err := c.CheckPodUID(ctx, namespace, name, expectedUID); err != nil {
		return err
	}
	request := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(name).
		Namespace(namespace).
		SubResource("exec")
	request.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{shell},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	executor, err := remotecommand.NewSPDYExecutor(c.restConfig, http.MethodPost, request.URL())
	if err != nil {
		return err
	}
	return executor.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               true,
		TerminalSizeQueue: sizeQueue,
	})
}
