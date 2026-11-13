# Changelog

## Unreleased

- Added local release artifact generation for GitHub release packaging: npm
  tarball, `.sha256`, and release manifest verification.
- Added Feishu runtime operations runbook covering configuration, health,
  action dispatch, retry/dead-letter handling, upgrades, and rollback.
- Clarified that `wechat` and `wecom` remain `v0.1.x deferred`; they are not
  beta blockers until their manifests and sandbox evidence exist.

## 0.1.x Beta

- Feishu remains the only experimental connector in the 0.1.x beta scope.
- WeChat and WeCom are planned connectors with documentation placeholders only.
- Public release still requires a real tag, downloadable GitHub release assets,
  and post-upload checksum verification.
