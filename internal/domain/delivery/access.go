package delivery

// AccessCandidate is derived from an authorized deployed resource. DialAddress
// pins the resource address while URL preserves HTTP Host and TLS identity.
// Neither field is accepted as an arbitrary caller-provided network target.
type AccessCandidate struct {
	URL             string
	DialAddress     string
	ProtocolUnknown bool
}
