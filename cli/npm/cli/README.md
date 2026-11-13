# @opensoha/cli

This package is a thin, verified launcher for the official native `soha` CLI.
It does not reimplement Soha or MCP in JavaScript.

```bash
npx -y @opensoha/cli@latest mcp
npx -y @opensoha/cli@latest mcp --base-url https://soha.example.com
npx -y @opensoha/cli@latest setup --client codex --mode both
npx -y @opensoha/cli@latest setup --client codex --scope project
npx -y @opensoha/cli@latest skill update
```

The launcher downloads the platform-specific binary from the matching GitHub
release, verifies it against `checksums.txt`, caches it by CLI version, and
checks the cached binary before every execution. Supported targets are Linux,
macOS, and Windows on amd64 or arm64.

Use `@latest` for an interactive one-shot setup or update. Pin the npm version
in CI for reproducible automation. `setup` records the verified cached native
binary in the generated MCP configuration, so the agent does not need to run
an npm download path on every MCP startup. Re-run the latest `setup` command
when upgrading the configured MCP binary.
