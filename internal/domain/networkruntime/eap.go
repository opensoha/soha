package networkruntime

import (
	"github.com/opensoha/soha/internal/networkprotocol"
	"time"
)

const EAPCertificatePendingTTL = 15 * time.Minute

type EAPCredential struct {
	networkprotocol.EAPCertificateResult
	ParentCredentialID        string
	RuntimeID                 string
	DeviceID                  string
	SubjectID                 string
	RequestID                 string
	CSRHash                   string
	Generation                int
	CertificateSerial         string
	CertificateAuthorityKeyID string
	NotBefore                 time.Time
	CreatedAt                 time.Time
}
