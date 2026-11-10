package delivery

import (
	"crypto/sha256"
	"fmt"
	"strings"
)

func KubernetesImportServiceKey(clusterID, namespace string, workload KubernetesWorkloadImportInput) string {
	scope := sha256.Sum256([]byte(clusterID + "\x00" + namespace + "\x00" + workload.CRDName))
	name := workload.WorkloadName
	if len(name) > 40 {
		name = name[:40]
	}
	identity := sha256.Sum256([]byte(workload.WorkloadName))
	return fmt.Sprintf("k8s-%s-%s-%x-%x", strings.ToLower(workload.WorkloadKind), name, scope[:8], identity[:4])
}
