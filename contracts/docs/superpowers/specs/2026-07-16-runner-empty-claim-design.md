# Runner Empty Claim Response Design

## Problem

The Delivery and Docker runners poll the Soha control plane for queued work. An empty queue currently reaches the repositories as `ErrNotFound`, is mapped by the shared handler error path to HTTP 404, and is then logged by request middleware as a warning. This makes normal idle polling look like a startup or routing failure every five seconds.

The published OpenAPI contract is also out of sync with the implementation. It currently documents HTTP 200 with optional empty data, while the server returns HTTP 202 for a successful claim and HTTP 404 when no work is available.

## Decision

Both claim endpoints will use the same response semantics:

- `202 Accepted` with the existing item envelope when a task or operation is claimed.
- `204 No Content` when no matching queued work is available.
- Existing `400`, `401`, and `500` behavior remains unchanged for malformed input, failed authentication, and unexpected failures.

Affected endpoints:

- `POST /api/v1/delivery/execution-tasks/claim`
- `POST /api/v1/docker/operations/claim`

The 204 response is an expected polling outcome. It must not be added to the Gin error list and therefore must not be logged as a rejected request.

## Repository Changes

### soha-contracts

Update both OpenAPI operations to declare `202` with their existing response envelopes and `204` with no response body. Regenerate the TypeScript and Go DTO outputs and update the compatibility baseline through the repository's standard generation and compatibility commands.

The handwritten Go client already treats every successful response with an empty body as a nil error and returns the zero-value DTO. Add focused client tests proving that both claim methods accept 204 without attempting JSON decoding.

### soha

In each claim handler, detect `apperrors.ErrNotFound` returned by the claim service and respond with `c.Status(http.StatusNoContent)`. Do not call the shared `writeError` helper for this expected outcome. Continue using `writeError` for all other errors.

Repository and application semantics remain unchanged: their not-found result still represents the absence of a matching claimable record. Only the HTTP boundary translates that result into queue polling semantics.

Add focused handler tests for both endpoints:

- empty queue produces 204 and no response body;
- successful claim remains 202 with the existing envelope;
- non-not-found failures still use the normal error mapping.

### soha-agent

No production code change is required. The contracts client returns a zero-value task or operation for 204, and the existing runner checks an empty ID and records a claim miss.

Add runner regression coverage showing that a 204 response is treated as an idle miss rather than a claimed task or execution failure.

## Compatibility

This is a backward-compatible success-path extension for current Agent behavior: 204 is a 2xx response and the existing client accepts empty bodies. Consumers that assumed every successful claim response contains JSON must be updated to recognize 204, which is now explicitly documented in OpenAPI.

The API paths, request schemas, authentication, callback contracts, queue filters, and task state transitions do not change.

## Verification

- `soha-contracts`: generation freshness, OpenAPI validation and compatibility checks, Go and TypeScript compilation, focused client tests.
- `soha`: Delivery and Docker handler tests, route tests, relevant application/repository tests, then `GOWORK=off go test ./...`.
- `soha-agent`: runner-focused tests and `GOWORK=off go test ./...`.
- Runtime check: with both queues empty, repeated claim requests return 204 and no WARN entry is emitted by the Soha request logger.

## Out Of Scope

- Changing polling intervals or replacing polling with long polling.
- Changing queue selection, provider-kind filters, host filters, or operation-kind filters.
- Changing Agent Runtime provider claim endpoints.
- Globally downgrading all HTTP 404 request logs.
