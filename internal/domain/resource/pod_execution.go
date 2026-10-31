package resource

import contractresource "github.com/opensoha/soha-contracts/resource"

// PodExecutionRecord is private persistence state. Input and output must be
// encrypted; only the receipt is exposed by the application capability.
type PodExecutionRecord struct {
	ActorID        string
	KeyHash        string
	InputHash      string
	ConnectionHash string
	Input          contractresource.PodArgvInput
	Receipt        contractresource.PodExecutionReceipt
}
