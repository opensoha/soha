package network

import _ "embed"

var (
	//go:embed radius-runtime.schema.json
	radiusRuntimeSchema []byte

	//go:embed radius-password-verifier.schema.json
	radiusPasswordVerifierSchema []byte

	//go:embed network-runtime-protocol.schema.json
	runtimeProtocolSchema []byte

	//go:embed network-ingest-event.schema.json
	ingestEventSchema []byte

	//go:embed network-radius-accounting.schema.json
	radiusAccountingSchema []byte

	//go:embed proxy-runtime.schema.json
	proxyRuntimeSchema []byte
)

func RuntimeProtocolSchema() []byte {
	return append([]byte(nil), runtimeProtocolSchema...)
}

func IngestEventSchema() []byte {
	return append([]byte(nil), ingestEventSchema...)
}

func RadiusAccountingSchema() []byte {
	return append([]byte(nil), radiusAccountingSchema...)
}

func ProxyRuntimeSchema() []byte {
	return append([]byte(nil), proxyRuntimeSchema...)
}

func RadiusPasswordVerifierSchema() []byte {
	return append([]byte(nil), radiusPasswordVerifierSchema...)
}

func RadiusRuntimeSchema() []byte { return append([]byte(nil), radiusRuntimeSchema...) }
