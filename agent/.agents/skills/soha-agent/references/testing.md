# Testing and security evidence

Use the actual CI and Go engineering reference for package/race/runner gates. Fake control-plane,
Docker/Buildpacks/Hermes runtime and real cluster modes are different evidence. Existing runner
fixtures cover timeout, cancellation, callback retries and exact cleanup. Add denial/unsupported,
wrong workspace and late callback cases at the affected owner; do not create soha-agent-testing.
Real runners require disposable resources and their exact ownership. A successful fake or skipped
runtime test cannot establish real acceptance. Public protocol changes go through contracts first.
For cross-layer security/OCR in a known workspace, load Core soha-security by its real path;
missing shared guidance does not disable ordinary tests. No model egress or tool installation is implied.
