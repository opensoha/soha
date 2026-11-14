// Run after installation: kubectl get deployment NAME -n NAMESPACE -o json |
// GOWORK=off go run ./deploy/outpost/slot-readback.go
// Input must be the live, API-defaulted Deployment, never Helm output.
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"

	appsv1 "k8s.io/api/apps/v1"
)

func main() {
	var deployment appsv1.Deployment
	if err := json.NewDecoder(os.Stdin).Decode(&deployment); err != nil {
		panic(err)
	}
	if deployment.UID == "" || deployment.Name == "" || deployment.Namespace == "" {
		panic("live Deployment UID, name and namespace are required")
	}
	template, err := json.Marshal(deployment.Spec.Template)
	if err != nil {
		panic(err)
	}
	result := struct {
		Namespace      string `json:"namespace"`
		Name           string `json:"name"`
		UID            string `json:"uid"`
		TemplateDigest string `json:"templateDigest"`
	}{deployment.Namespace, deployment.Name, string(deployment.UID), fmt.Sprintf("sha256:%x", sha256.Sum256(template))}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		panic(err)
	}
}
