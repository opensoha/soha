package kubernetes

import (
	"context"
	"errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"time"
)

// CheckPodUID uses a live read; old clients may omit UID, saved sessions must provide it.
func (c *Client) CheckPodUID(ctx context.Context, namespace, name, expectedUID string) error {
	if expectedUID == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	pod, err := c.typed.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(pod.UID) != expectedUID {
		return apierrors.NewConflict(schema.GroupResource{Resource: "pods"}, name, errors.New("pod identity changed; open the current Pod"))
	}
	return nil
}
