package resource

import (
	"fmt"
	"regexp"
	"time"
	"unicode"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/util/validation"
)

// PodArgvToolInput mirrors the closed KubernetesPodArgvToolInput schema.
// Gateway-only schemas are not HTTP operations and are pruned by the Go HTTP
// generator. These helpers share the existing runtime's semantic validation;
// they neither authorize a caller nor start a process.
type PodArgvToolInput struct {
	ClusterID      string       `json:"clusterId"`
	Namespace      string       `json:"namespace"`
	PodName        string       `json:"podName"`
	IdempotencyKey string       `json:"idempotencyKey"`
	Execution      PodArgvInput `json:"execution"`
}

func (input PodArgvToolInput) Normalized() (PodArgvToolInput, error) {
	if !validPodToolTarget(input.ClusterID, input.Namespace, input.PodName) || !validPodToolKey(input.IdempotencyKey) {
		return PodArgvToolInput{}, fmt.Errorf("valid cluster, namespace, Pod and original execution key are required")
	}
	execution, err := input.Execution.Normalized()
	if err != nil {
		return PodArgvToolInput{}, err
	}
	input.Execution = execution
	return input, nil
}

type PodExecutionLookupInput struct {
	ClusterID      string `json:"clusterId"`
	Namespace      string `json:"namespace"`
	PodName        string `json:"podName"`
	ExecutionID    string `json:"executionId,omitempty"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
}

// PodGatewayResult is a safe Gateway projection, not the raw native receipt.
// Receipt never carries Output and always has OutputAvailable=false. Text
// output may be redacted; its byte counters retain original source counts.
// Binary output must be withheld instead of forwarding encoded secret bytes.
type PodGatewayResult struct {
	Receipt     PodGatewayReceipt   `json:"receipt"`
	OutputState string              `json:"outputState"`
	Output      *PodExecutionOutput `json:"output,omitempty"`
}

// PodGatewayReceipt cannot hold raw process streams. Construct Gateway results
// through NewPodGatewayResult; its outputAvailable flag is always false.
type PodGatewayReceipt struct {
	ID              string     `json:"id"`
	Status          string     `json:"status"`
	ClusterID       string     `json:"clusterId"`
	Namespace       string     `json:"namespace"`
	PodName         string     `json:"podName"`
	ExpectedUID     string     `json:"expectedUid"`
	Container       string     `json:"container"`
	CreatedAt       time.Time  `json:"createdAt"`
	DeadlineAt      time.Time  `json:"deadlineAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	OutputAvailable bool       `json:"outputAvailable"`
}

func NewPodGatewayResult(receipt PodExecutionReceipt) PodGatewayResult {
	return PodGatewayResult{OutputState: "unavailable", Receipt: PodGatewayReceipt{
		ID: receipt.ID, Status: receipt.Status, ClusterID: receipt.ClusterID,
		Namespace: receipt.Namespace, PodName: receipt.PodName,
		ExpectedUID: receipt.ExpectedUID, Container: receipt.Container,
		CreatedAt: receipt.CreatedAt, DeadlineAt: receipt.DeadlineAt,
		ExpiresAt: receipt.ExpiresAt, FinishedAt: receipt.FinishedAt,
	}}
}

var podToolExecutionID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func (input PodExecutionLookupInput) Validate() error {
	if !validPodToolTarget(input.ClusterID, input.Namespace, input.PodName) {
		return fmt.Errorf("valid cluster, namespace and Pod are required")
	}
	if (input.ExecutionID == "") == (input.IdempotencyKey == "") {
		return fmt.Errorf("exactly one original execution ID or key is required")
	}
	if input.ExecutionID != "" && !podToolExecutionID.MatchString(input.ExecutionID) || input.IdempotencyKey != "" && !validPodToolKey(input.IdempotencyKey) {
		return fmt.Errorf("invalid original execution ID or key")
	}
	return nil
}

func validPodToolTarget(cluster, namespace, pod string) bool {
	if cluster == "" || !utf8.ValidString(cluster) || utf8.RuneCountInString(cluster) > 256 {
		return false
	}
	for _, char := range cluster {
		if unicode.IsControl(char) || unicode.IsSpace(char) || char == '\ufeff' {
			return false
		}
	}
	return len(validation.IsDNS1123Label(namespace)) == 0 && len(validation.IsDNS1123Subdomain(pod)) == 0
}

func validPodToolKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for _, char := range key {
		if char < '!' || char > '~' {
			return false
		}
	}
	return true
}
