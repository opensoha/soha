---
name: soha-backend-testing
description: Run Soha Core isolated PostgreSQL integration and internal lifecycle acceptance with strict no-skip evidence. Production implementation follows soha-backend; browser acceptance belongs to soha-web-testing.
---

# Soha Backend Testing

Read [test execution](../../../docs/testing.md), the affected tests and actual CI. Reuse
`deletion-integration.yml` and the existing Go fixtures rather than creating another framework.
Run database tests only against this run's disposable instance. Use unique data and exact cleanup;
never reuse development or production databases. Ordinary Go tests do not depend on Web or AI.

For required integration, run uncached `go test -json`, retain the process exit code and validate
all required parent/child tests with `scripts/check-go-test-evidence.py`. A package PASS, cached
result, zero matches or skipped child is not real acceptance. Exercise offline evidence counterexamples.

Preserve authorization, scope, ownership, side effects and terminal/attempt semantics. Reuse
existing cancellation, timeout, retry, token rotation, duplicate/late callback and race tests for
affected modules; do not expand to unrelated modules. Report package, actual PG, fake/real runner,
fixed/latest version combinations and missing environments separately.
