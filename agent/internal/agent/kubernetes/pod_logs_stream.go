package kubernetes

import (
	"context"
	"io"

	corev1 "k8s.io/api/core/v1"
)

func (c *Client) StreamPodLogs(ctx context.Context, namespace, name, container string, tailLines, sinceSeconds int64, stdout io.Writer, expectedUID string) error {
	if err := c.CheckPodUID(ctx, namespace, name, expectedUID); err != nil {
		return err
	}
	options := &corev1.PodLogOptions{
		Container: container,
		Follow:    true,
	}
	if tailLines > 0 {
		options.TailLines = &tailLines
	}
	if sinceSeconds > 0 {
		options.SinceSeconds = &sinceSeconds
	}
	stream, err := c.typed.CoreV1().Pods(namespace).GetLogs(name, options).Stream(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()
	_, err = io.Copy(stdout, stream)
	return err
}
