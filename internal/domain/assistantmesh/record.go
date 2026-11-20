package assistantmesh

import "encoding/json"

// Record stores an aggregate with a database-owned identity and an application-owned state.
type Record struct {
	ID      string
	OwnerID string
	Kind    string
	Payload json.RawMessage
}
