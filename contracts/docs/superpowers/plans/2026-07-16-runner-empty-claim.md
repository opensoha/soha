# Runner Empty Claim Responses Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Treat an empty runner claim queue as an idle polling result instead of an HTTP 404 error for Delivery and Docker claims.

**Architecture:** Keep repository and application `ErrNotFound` behavior unchanged. Translate only claim-handler `ErrNotFound` results to HTTP 204, retain HTTP 202 with the existing envelope for successful claims, and document both outcomes in the shared OpenAPI contract. The existing SDK and agent already accept bodyless successful responses; regression tests will lock that behavior down.

**Tech Stack:** OpenAPI 3.1, Go 1.25, Gin, generated TypeScript contracts, handwritten Go SDK client.

---

### Task 1: Update the public runner claim contract

**Files:**
- Modify: `openapi/soha-api.yaml`
- Modify: `scripts/validate.mjs`
- Modify: `gen/go/sohaapi/client_test.go`
- Regenerate: `gen/ts/sohaapi/schema.ts`
- Modify if required by compatibility policy: `compat/openapi-0.1.0.snapshot.json`

- [x] Add structural validation requiring `202` and `204` responses for both claim operations.
- [x] Run `npm run validate` and confirm the new assertion fails against the old contract.
- [x] Add `202` and bodyless `204` responses for both endpoints while retaining legacy `200` compatibility.
- [x] Add Go client tests proving both claim methods return zero-value items without error on `204`.
- [x] Run `npm run generate`, focused Go tests, generated checks, OpenAPI lint, and compatibility checks.
- [x] Commit the contracts change.

### Task 2: Map empty backend claims to HTTP 204

**Files:**
- Create: `internal/api/handlers/runner_claim_test.go`
- Modify: `internal/api/handlers/delivery.go`
- Modify: `internal/api/handlers/docker.go`

- [x] Add handler tests covering empty queue, successful claim, and unexpected error for Delivery and Docker.
- [x] Run the focused tests and confirm empty-queue cases fail with `404`.
- [x] Special-case `errors.Is(err, apperrors.ErrNotFound)` in each claim handler and return `204` without registering a Gin error.
- [x] Keep successful claims at `202` and other errors on the shared error path.
- [x] Run focused handler tests, the handler package tests, formatting, complexity, and repository diff checks.
- [x] Commit the backend change.

### Task 3: Lock down Agent idle-poll compatibility

**Files:**
- Modify: `internal/agent/runner/fake_control_plane_test.go`

- [x] Add execution and Docker claim tests whose fake control plane returns `204` with an empty body.
- [x] Assert each call reports a normal claim miss, returns no task, and records no successful claim.
- [x] Run focused runner tests and the runner package race test.
- [x] Commit the Agent regression tests.

### Task 4: Cross-repository verification

- [x] Run the full contracts test gate.
- [x] Run backend full tests, vet, complexity checks, and build checks appropriate to this transport-only change.
- [x] Run Agent full tests, race tests, vet, and build checks.
- [x] Run `git diff --check` and inspect status/log in all three repositories.
- [x] Summarize commits, behavior, and any verification limitations before integration.
