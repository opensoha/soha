package copilot

import (
	"errors"
	"regexp"

	"github.com/google/uuid"
)

type PersonalVMPolicy struct {
	OwnerID        string
	ComputeRef     string
	Provider       string
	DataDiskSerial string
	DataDiskBytes  int64
}

// Provider observations freeze identity, never data initialization or readiness.
type PersonalVMIdentity struct {
	ProviderIdentityHash string `json:"providerIdentityHash"`
	DataIdentityHash     string `json:"dataIdentityHash"`
	DataDiskSerial       string `json:"dataDiskSerial"`
	DataDiskBytes        int64  `json:"dataDiskBytes"`
}

var personalVMSerial = regexp.MustCompile(`^[A-Za-z0-9_-]{1,20}$`)
var personalVMHash = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func ValidPersonalVMPolicyRevision(value string) bool {
	return personalVMHash.MatchString(value)
}

func (p PersonalVMPolicy) Validate() error {
	for _, value := range []string{p.OwnerID, p.ComputeRef} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return errors.New("canonical personal VM identity required")
		}
	}
	if p.Provider != "pve" || !validPersonalVMDisk(p.DataDiskSerial, p.DataDiskBytes) {
		return errors.New("supported independent personal VM disk required")
	}
	return nil
}

func (p PersonalVMIdentity) Validate() error {
	if !personalVMHash.MatchString(p.ProviderIdentityHash) || !personalVMHash.MatchString(p.DataIdentityHash) || !validPersonalVMDisk(p.DataDiskSerial, p.DataDiskBytes) {
		return errors.New("frozen personal VM disk identity required")
	}
	return nil
}

func validPersonalVMDisk(serial string, bytes int64) bool {
	return personalVMSerial.MatchString(serial) && bytes >= 1<<30 && bytes <= 1<<40 && bytes%(1<<30) == 0
}
