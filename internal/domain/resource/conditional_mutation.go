package resource

import (
	"time"

	domainai "github.com/opensoha/soha/internal/domain/aigateway"
)

// ConditionalMutationRecord is an original-call receipt, never a retry queue.
// InputHash, authorization and output must be encrypted by its repository. It
// deliberately has no YAML field; the protected workflow owns original input.
type ConditionalMutationRecord struct {
	ID, ActorID, CredentialID, KeyHash, InputHash, ConnectionHash string
	Status                                                        string
	CreatedAt, DeadlineAt, ExpiresAt                              time.Time
	FinishedAt                                                    *time.Time
	Authorization                                                 domainai.ExecutionAuthorization
	Output                                                        map[string]any
}
