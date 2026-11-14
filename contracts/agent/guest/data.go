package guest

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const DataPlanVersion = "opensoha.dev/personal-guest-data-plan/v1"

// Core reserves operationId before fixed privileged dispatch. Liveness alone
// cannot authorize this plan; GET /bindings/{id}/data-operations/{operationId}
// returns it only for that current, running owner-authorized operation.
type DataPlan struct {
	SchemaVersion  string  `json:"schemaVersion"`
	OperationID    string  `json:"operationId"`
	Binding        Binding `json:"binding"`
	DiskQuotaBytes int64   `json:"diskQuotaBytes"`
}

func (p DataPlan) Validate(now time.Time) error {
	if p.SchemaVersion != DataPlanVersion || !uuidPattern.MatchString(p.OperationID) || p.Binding.Validate() != nil || p.Binding.Provider != "pve" || now.Before(p.Binding.IssuedAt) || !now.Before(p.Binding.ExpiresAt) || p.DiskQuotaBytes < 16<<20 || p.DiskQuotaBytes%1024 != 0 || p.DiskQuotaBytes >= p.Binding.DataDiskBytes {
		return errors.New("invalid or expired personal data operation")
	}
	return nil
}

type DataResult struct {
	DataRef      string `json:"dataRef"`
	FilesystemID string `json:"filesystemId"`
	DiskQuota    int64  `json:"diskQuotaBytes"`
	DiskUsed     int64  `json:"diskUsedBytes"`
	Reconciled   bool   `json:"reconciled"`
}

// Provisioning contains public identity material and a bootstrap-only challenge.
// It is encrypted in the durable task and supplied only to the fixed QGA installer.
// The private key is generated in the VM and never transported through Core.
type Provisioning struct {
	Plan           DataPlan `json:"plan"`
	Endpoint       string   `json:"endpoint"`
	EnrollmentID   string   `json:"enrollmentId"`
	Challenge      string   `json:"challenge"`
	CertificatePEM string   `json:"certificatePEM"`
	ClientCAPEM    string   `json:"clientCAPEM"`
	ArtifactSHA256 string   `json:"artifactSHA256"`
	CPUQuotaMilli  int64    `json:"cpuQuotaMilli"`
	MemoryBytes    int64    `json:"memoryBytes"`
	PidsLimit      int64    `json:"pidsLimit"`
	Network        bool     `json:"network"`
}

func (p Provisioning) Validate(now time.Time) error {
	u, err := url.Parse(p.Endpoint)
	_, tokenErr := EnrollmentTokenHash("Bearer " + p.Challenge)
	if p.Plan.Validate(now) != nil || err != nil || u.Scheme != "https" || u.Host == "" || u.Path != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || len(p.Endpoint) > 512 || strings.TrimSpace(p.Endpoint) != p.Endpoint || tokenErr != nil || (Enrollment{SchemaVersion: EnrollmentVersion, EnrollmentID: p.EnrollmentID}).Validate() != nil || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(p.ArtifactSHA256) || len(p.CertificatePEM) < 1 || len(p.CertificatePEM) > 4096 || len(p.ClientCAPEM) < 1 || len(p.ClientCAPEM) > 8192 || p.CPUQuotaMilli < 1 || p.CPUQuotaMilli > 8000 || p.MemoryBytes < 32<<20 || p.MemoryBytes > 16<<30 || p.PidsLimit < 4 || p.PidsLimit > 256 || p.Network {
		return errors.New("invalid personal guest provisioning bundle")
	}
	return nil
}
