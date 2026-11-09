package networkprotocol

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"slices"
)

const NASAuthenticationTTLS = "eap-ttls-pap"
const CapabilityTTLSVerifier = "radius-ttls-pap-verifier-v1"
const CapabilityTTLS = "radius-eap-ttls-pap-v1"
const CapabilityEAPLifecycle = "eap-certificate-lifecycle-v1"

type RadiusPasswordRequest struct {
	RuntimeProof   *ManagedNASProof `json:"runtimeProof,omitempty"`
	RequestID      string           `json:"requestId"`
	NASID          string           `json:"nasId"`
	StationID      string           `json:"stationId"`
	Login          string           `json:"login"`
	PasswordBase64 string           `json:"passwordBase64"`
	EAPMethod      string           `json:"eapMethod"`
	InnerMethod    string           `json:"innerMethod"`
}

type RadiusPasswordResult struct {
	SubjectID             string
	AuthenticationReceipt string
}

type EAPCertificateRevokeRequest struct {
	RequestID    string `json:"requestId"`
	CredentialID string `json:"credentialId"`
	Fingerprint  string `json:"fingerprint"`
	ReasonCode   string `json:"reasonCode"`
}

type EAPCertificateRevokeResult struct {
	RequestID    string `json:"requestId"`
	CredentialID string `json:"credentialId"`
	Fingerprint  string `json:"fingerprint"`
	Status       string `json:"status"`
}

// AdmissionProfileRevision excludes the rotating credential fingerprint so the
// same installed profile can atomically activate a replacement certificate.
func AdmissionProfileRevision(profile AdmissionProfile) string {
	profile.ProfileRevision = ""
	profile.ClientCertificateFingerprint = ""
	profile.ServerNames = slices.Clone(profile.ServerNames)
	profile.CACertificates = slices.Clone(profile.CACertificates)
	slices.Sort(profile.ServerNames)
	slices.Sort(profile.CACertificates)
	encoded, _ := json.Marshal(profile)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256:%x", digest)
}

// ManagedNASProof is injected only by the approved private RADIUS supervisor.
type ManagedNASProof struct {
	PoolID                string `json:"poolId"`
	MemberID              string `json:"memberId"`
	CredentialID          string `json:"credentialId"`
	ProcessEpoch          string `json:"processEpoch"`
	OwnerEpoch            int64  `json:"ownerEpoch"`
	ControlRevision       int64  `json:"controlRevision"`
	ConfigurationRevision int64  `json:"configurationRevision"`
	ConfigurationHash     string `json:"configurationHash"`
}
