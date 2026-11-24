# Storage driver RPC v1

`github.com/opensoha/soha/contracts/plugins/storagedriver` defines the protocol and Go driver interface for a Soha Core host and its storage driver subprocess. The protocol major is independent of the installed plugin version.

## Transport and startup

- Use direct HTTP/1.1 over a permission-restricted Unix domain socket or HTTPS with TLS 1.3 on a literal IPv4 or IPv6 loopback address. For loopback, the host provisions a fresh server certificate and key for each launch, conveys them to the driver through a protected inherited bootstrap channel, and pins the exact leaf certificate's DER SHA-256 before sending HTTP. For Unix, both peers use the private socket path chosen by the supervisor. The socket and parent directory must prevent other processes from connecting to, substituting or relaying that endpoint.
- Use an independent cryptographically random 32-byte launch secret per process. The host provisions it through a protected inherited channel, such as an anonymous pipe. Keep it out of command-line arguments, environment variables, URLs, storage and logs. The driver uses its own actual identity from build metadata. The host gets the expected identity from the verified release and its backend declaration.
- To start, the host sends `POST /v1/storage/handshake` with a fresh 32-byte nonce and proof. The driver verifies the host proof in constant time and checks all four expected identity fields against its own values. A successful handshake is accepted at most once per launch, including concurrent attempts. The driver returns its identity and proof only after recording the derived bearer for that launch. The host verifies the proof, nonce binding and exact identity before it sends credentials. Both peers then discard the launch secret. If acceptance is ambiguous or the host rejects the response, close the transport and terminate that launch.
- Use `Authorization: Bearer <token>` for **every** non-handshake request, including on reused connections. Reject operations before a successful handshake and compare the token in constant time before reading or processing operation metadata. The bearer expires with that launch. Never include it in a response. Do not follow redirects, use a proxy, or enable HTTP cache or compression for this transport. Both peers send `Cache-Control: no-store`. These RPCs assume OS-enforced subprocess and filesystem isolation provided by the supervisor.
- Calls may run concurrently over separate connections to the same endpoint. HTTP connection reuse is allowed. The client enforces a deadline for the entire call and body stream. Closing an open body or cancelling the context aborts the corresponding exchange. The server propagates client disconnects and local deadlines to the driver and closes its streams. One cancelled call must not cancel unrelated calls. Callers must explicitly decide whether to retry a failed operation.

The handshake uses the following JSON envelopes. The values below are field descriptions. The `capabilities` integer uses bits 0 to 6 for test, put, open, delete, list, conditional open and range open, respectively. V1 requires the full bitmap `127`. IDs use the manifest plugin ID pattern, at most 255 bytes. The version uses 1–128 bytes from `[A-Za-z0-9.+_-]` with a leading alphanumeric byte. Compare versions byte for byte.

```text
Request  {"expected":{"pluginId":"<id>","version":"<version>","protocolMajor":1,"capabilities":127},"nonce":"<nonce>","proof":"<host proof>"}
Response {"actual":{"pluginId":"<id>","version":"<version>","protocolMajor":1,"capabilities":127},"proof":"<driver proof>"}
```

The nonce and all proofs use unpadded base64url. A proof is HMAC-SHA256 with the raw launch secret over these exact UTF-8 lines joined by LF, with no trailing LF. Substitute `host` for the request proof, `driver` for the response proof and `session` for the bearer derived independently by both peers. Numbers use their decimal representation. The nonce in the transcript is the encoded request nonce for all three calculations.

```text
soha-storage-driver/v1
<role>
<pluginId>
<version>
<protocolMajor>
<capabilities>
<nonce>
```

## Operations

Paths are fixed and accept no query string. JSON bodies use `Content-Type: application/json`. Header envelopes are one `X-Soha-Storage-Metadata` field with unpadded base64url JSON and no line breaks. The `Binding` in each envelope includes `id` and `container` and may include `prefix`, `settings` and `credentials`.

| Method and path | Request | Success |
| --- | --- | --- |
| `POST /v1/storage/handshake` | `HandshakeRequest` JSON body | `200` and `HandshakeResponse` JSON |
| `POST /v1/storage/test-connection` | `{"binding": …}` JSON body | `204` |
| `POST /v1/storage/put` | `{"binding": …, "key": …, "contentType": …}` metadata header and raw `application/octet-stream` body | `200` and `{"size": …, "sha256": …, "etag": …}` JSON |
| `GET /v1/storage/open` | `{"binding": …, "key": …}` metadata header, optional `If-None-Match` and `Range` headers, no request body | `200` full stream, `206` range stream or `304` with `ETag` and no body |
| `POST /v1/storage/delete-keys` | `{"binding": …, "keys": […]}` JSON body | `204` |
| `POST /v1/storage/list-prefix` | `{"binding": …, "prefix": …, "limit": …, "cursor": …}` JSON body (`cursor` optional) | `200` and `{"keys": […], "nextCursor": …}` JSON (`nextCursor` omitted at end) |

### Binding scope

The host authorizes each call and constructs `Binding` from the selected connection and the credentials resolved for that operation. `id` identifies the connection. `container` selects the single provider bucket or container. `settings` holds driver-specific non-secret JSON configuration. `credentials` contains only values named by the approved connection and needed for this call. The driver may use them during the call, including the lifetime of an open stream. Do not retain credentials after the call, fall back to process or provider ambient credentials, echo the binding, or dump request headers or bodies. This contract does not grant filesystem or network access.

Keys and list prefixes are relative to the binding. A non-empty binding prefix ends in `/`. Form the provider key or prefix by concatenating `binding.prefix` and the relative value verbatim. Adapters must preserve bytes when escaping provider paths and must constrain every provider action to this container and prefix. Strip the binding prefix on list results and reject any key outside it. Empty list prefixes cover the whole binding. Object keys are case-sensitive UTF-8 and may contain nested `/`-separated components. Empty, `.` and `..` components, backslashes and control characters are invalid. The list prefix may end in `/` or be a partial final component. Do not treat any key as a local path or decode it as a URI.

### Operation behavior

- **TestConnection** verifies that the binding can reach the configured container with the required read and write permissions. If a write probe is necessary, the driver uses a fresh temporary key within the binding and cleans it up without touching existing objects.
- **PutObject** replaces the exact key. Stream request bytes to storage without buffering the entire object. The HTTP request body can have a known `Content-Length` or use chunked transfer. The `contentType` metadata describes the stored object. Report success only after the full body was consumed and the provider committed it. Return the byte count and lowercase 64-digit hex SHA-256 of those exact bytes plus the quoted HTTP entity tag of the committed representation. Report read errors or cancellation and abort unfinished writes. A lost response can leave a committed object, so replay requires an explicit caller decision.
- **OpenObject** checks the requested key's current representation. If found, evaluate `If-None-Match` before applying `Range`, using weak comparison for a quoted tag, comma-delimited list or `*`. A match yields `304` with the current `ETag`, without `Content-Length`, `Content-Type`, `Content-Range` or a body. A missing key yields `404`. Support one `Range` in the form `bytes=N-M`, `bytes=N-` or `bytes=-N` with unsigned decimal values in the signed 64-bit range. Bound the end by the current size. A syntactically malformed or multi-range request yields `400`. A valid range with no matching bytes yields `416`. Return `206` for a satisfiable range even if it covers the whole object. On `200` and `206`, require `ETag`, `Content-Type` and an exact `Content-Length` for the returned bytes. On `206`, also require `Content-Range: bytes <start>-<end>/<full size>` with inclusive offsets and use the same representation for all metadata and bytes. `200` has no `Content-Range`. A stream failure before the advertised length aborts the exchange. An early EOF is an error even after a success status.
- **DeleteKeys** removes only the exact listed keys in the binding. Duplicate keys and missing keys are harmless. Return `204` when all keys are absent. If an error follows partial progress, the caller may retry the same batch. This operation never means delete a prefix or a container.
- **ListPrefix** returns at most `limit` distinct keys matching the relative prefix. An empty or omitted request cursor starts enumeration. The driver binds cursors to the binding and prefix and rejects a stale or mismatched cursor with `invalid_cursor`. Treat a non-empty response cursor as opaque and pass it unchanged with the same binding and prefix. A non-empty next cursor must advance and may accompany an empty page. At end, return no next cursor. Ordering and snapshot consistency during concurrent mutations are provider-dependent.

## Limits and errors

| Value | Limit |
| --- | --- |
| Total request or response headers | 32 KiB |
| Decoded metadata header | 16 KiB |
| JSON control body | 2 MiB |
| Binding ID and container | 255 bytes each |
| Binding configuration and credentials | 16 KiB encoded JSON for the complete binding, including at most 32 credentials with 1–128 byte names matching `[A-Za-z0-9_.-]+` and at most 8 KiB UTF-8 per value |
| Provider key or prefix including binding prefix | 1024 bytes |
| Cursor | 4096 UTF-8 bytes without control characters |
| ETag, `Content-Type` | 1024 bytes each |
| `If-None-Match`, `Range` | 4096 and 64 bytes respectively |
| Delete keys or page limit | 1–256 |

Apply limits before parsing or buffering untrusted values. Requests with settings or credentials near the binding limit still need room for metadata envelope fields within the decoded header limit. The JSON decoder must read a single object. An entity tag contains a quoted ASCII opaque value, optionally prefixed with `W/`. Raw object streams have no protocol-imposed byte cap beyond the signed 64-bit size representation. Host policy can cap individual streams and concurrent work.

An RPC error has JSON `{"code":"<code>"}`. Use fixed codes, without provider error text, connection metadata, URLs or credentials. Missing or invalid authentication is `401`. A transport-level error without a valid RPC envelope, an unexpected status/code combination or a truncated stream is an RPC failure to the caller. Cancellation or loss of the channel may prevent a response.

| HTTP status | Code |
| --- | --- |
| `400` | `invalid_request`, `invalid_cursor` |
| `401` | `unauthenticated` |
| `403` | `access_denied` |
| `404` | `not_found` |
| `409` | `handshake_mismatch` |
| `413` | `too_large` |
| `416` | `range_not_satisfiable` |
| `429` | `busy` |
| `500` | `internal` |
| `503` | `unavailable` |
| `504` | `deadline_exceeded` |

An accepted handshake can be followed by a known-ready check using the supervisor's launch state or an authenticated TestConnection with an actual binding. A new process requires a new handshake. Handshake success establishes protocol readiness. It does not establish that any storage connection works.
