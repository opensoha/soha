package kubernetes

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

type PortForwardSession struct {
	localPort int
	stopCh    chan struct{}
	doneCh    chan struct{}
	once      sync.Once
}

func (s *PortForwardSession) LocalPort() int {
	if s == nil {
		return 0
	}
	return s.localPort
}

func (s *PortForwardSession) Stop() {
	if s == nil {
		return
	}
	s.once.Do(func() {
		close(s.stopCh)
		select {
		case <-s.doneCh:
		case <-time.After(5 * time.Second):
		}
	})
}

func (c *Client) StartPortForward(ctx context.Context, namespace, kind, name string, remotePort int) (*PortForwardSession, error) {
	if c == nil || c.typed == nil || c.restConfig == nil {
		return nil, fmt.Errorf("kubernetes client is not configured")
	}
	podName, targetPort, err := c.resolvePortForwardTarget(ctx, namespace, kind, name, remotePort)
	if err != nil {
		return nil, err
	}
	serverURL := c.typed.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("portforward").
		URL()
	roundTripper, upgrader, err := spdy.RoundTripperFor(c.restConfig)
	if err != nil {
		return nil, fmt.Errorf("build spdy transport: %w", err)
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: roundTripper}, http.MethodPost, serverURL)

	stopCh := make(chan struct{})
	readyCh := make(chan struct{})
	doneCh := make(chan struct{})
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}

	forwarder, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf(":%d", targetPort)}, stopCh, readyCh, outBuf, errBuf)
	if err != nil {
		return nil, fmt.Errorf("build port forwarder: %w", err)
	}
	session := &PortForwardSession{stopCh: stopCh, doneCh: doneCh}

	errCh := make(chan error, 1)
	go func() {
		defer close(doneCh)
		if err := forwarder.ForwardPorts(); err != nil {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-readyCh:
		ports, err := forwarder.GetPorts()
		if err != nil {
			session.Stop()
			return nil, fmt.Errorf("read forwarded port: %w", err)
		}
		if len(ports) == 0 || int(ports[0].Local) <= 0 {
			session.Stop()
			return nil, fmt.Errorf("port forward became ready without a local port")
		}
		session.localPort = int(ports[0].Local)
		return session, nil
	case err := <-errCh:
		if err == nil {
			err = fmt.Errorf("port forward exited before becoming ready")
		}
		return nil, fmt.Errorf("start port forward: %w", err)
	case <-time.After(10 * time.Second):
		session.Stop()
		return nil, fmt.Errorf("port forward did not become ready within 10s (stderr: %s)", strings.TrimSpace(errBuf.String()))
	case <-ctx.Done():
		session.Stop()
		return nil, ctx.Err()
	}
}

func (c *Client) resolvePortForwardTarget(ctx context.Context, namespace, kind, name string, port int) (string, int, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "service":
		svc, err := c.typed.CoreV1().Services(namespace).Get(queryCtx, name, metav1.GetOptions{})
		if err != nil {
			return "", 0, fmt.Errorf("get service: %w", err)
		}
		if len(svc.Spec.Selector) == 0 {
			return "", 0, fmt.Errorf("service %s has no pod selector", name)
		}
		var servicePort *corev1.ServicePort
		for i := range svc.Spec.Ports {
			if int(svc.Spec.Ports[i].Port) == port && (svc.Spec.Ports[i].Protocol == "" || svc.Spec.Ports[i].Protocol == corev1.ProtocolTCP) {
				servicePort = &svc.Spec.Ports[i]
				break
			}
		}
		if servicePort == nil {
			return "", 0, fmt.Errorf("service %s does not declare TCP port %d", name, port)
		}
		podList, err := c.typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(svc.Spec.Selector).String(),
		})
		if err != nil {
			return "", 0, fmt.Errorf("list pods: %w", err)
		}
		var targetError error
		for _, pod := range podList.Items {
			if pod.Status.Phase == corev1.PodRunning && portForwardPodReady(pod) {
				targetPort, err := resolveTCPServiceTargetPort(svc, *servicePort, &pod)
				if err != nil {
					targetError = err
					continue
				}
				return pod.Name, int(targetPort), nil
			}
		}
		if targetError != nil {
			return "", 0, fmt.Errorf("resolve service target port: %w", targetError)
		}
		return "", 0, fmt.Errorf("no ready pod found for service %s", name)
	case "pod":
		if _, err := c.typed.CoreV1().Pods(namespace).Get(queryCtx, name, metav1.GetOptions{}); err != nil {
			return "", 0, fmt.Errorf("get pod: %w", err)
		}
		return name, port, nil
	default:
		return "", 0, fmt.Errorf("unsupported target kind %s", kind)
	}
}

func resolveTCPServiceTargetPort(service *corev1.Service, port corev1.ServicePort, pod *corev1.Pod) (int32, error) {
	if service.Spec.ClusterIP == corev1.ClusterIPNone {
		return port.Port, nil
	}
	if port.TargetPort.Type == intstr.Int {
		if port.TargetPort.IntVal == 0 {
			return port.Port, nil
		}
		return port.TargetPort.IntVal, nil
	}
	name := port.TargetPort.StrVal
	for _, containers := range [][]corev1.Container{pod.Spec.Containers, pod.Spec.InitContainers} {
		for _, container := range containers {
			for _, candidate := range container.Ports {
				if candidate.Name == name && (candidate.Protocol == "" || candidate.Protocol == corev1.ProtocolTCP) {
					return candidate.ContainerPort, nil
				}
			}
		}
	}
	return 0, fmt.Errorf("pod %s has no TCP container port named %s", pod.Name, name)
}

func portForwardPodReady(pod corev1.Pod) bool {
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
