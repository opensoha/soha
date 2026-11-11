package network

import _ "embed"

var (
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
