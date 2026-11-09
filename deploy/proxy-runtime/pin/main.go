// pin prints installation policy from a live kubectl -o json response.
// It never connects to Kubernetes or changes a workload.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func main() {
	if err := run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(input io.Reader, output io.Writer) error {
	raw, err := io.ReadAll(io.LimitReader(input, 4<<20+1))
	if err != nil || len(raw) > 4<<20 {
		return fmt.Errorf("read bounded live workload JSON")
	}
	var header struct{ Kind string }
	if json.Unmarshal(raw, &header) != nil {
		return fmt.Errorf("invalid workload JSON")
	}
	var template corev1.PodTemplateSpec
	var uid, namespace, name string
	switch header.Kind {
	case "StatefulSet":
		var item appsv1.StatefulSet
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		template, uid, namespace, name = item.Spec.Template, string(item.UID), item.Namespace, item.Name
	case "Deployment":
		var item appsv1.Deployment
		if err := json.Unmarshal(raw, &item); err != nil {
			return err
		}
		template, uid, namespace, name = item.Spec.Template, string(item.UID), item.Namespace, item.Name
	default:
		return fmt.Errorf("only live StatefulSet or Deployment is supported")
	}
	if uid == "" || namespace == "" || name == "" || len(template.Spec.Containers) == 0 {
		return fmt.Errorf("live UID, namespace, name and PodTemplateSpec are required")
	}
	encoded, err := json.Marshal(template)
	if err != nil {
		return err
	}
	return json.NewEncoder(output).Encode(map[string]string{
		"workloadKind": header.Kind, "namespace": namespace, "name": name,
		"uid": uid, "templateDigest": fmt.Sprintf("sha256:%x", sha256.Sum256(encoded)),
	})
}
