#!/usr/bin/env bash
set -euo pipefail
case "${NPM_PUBLISH_MODE}" in
  stage|recover) ;;
  *) echo "Unsupported npm publish mode" >&2; exit 1 ;;
esac
version="${CONTRACTS_NPM_VERSION}"
tarball="${CONTRACTS_NPM_TARBALL}"
local_integrity="sha512-$(openssl dgst -sha512 -binary "${tarball}" | openssl base64 -A)"
if published_integrity=$(npm view "@opensoha/contracts@${version}" dist.integrity 2> dist/release/npm-view.stderr); then
  test "${published_integrity}" = "${local_integrity}"
  echo "published=true" >> "$GITHUB_OUTPUT"
  echo "Verified identical public @opensoha/contracts@${version}."
elif [ "${NPM_PUBLISH_MODE}" = "recover" ]; then
  echo "The requested npm version is not public; approve its stage before recovery." >&2
  exit 1
else
  if ! grep -q '^npm error code E404$' dist/release/npm-view.stderr; then
    cat dist/release/npm-view.stderr >&2
    echo "Registry lookup failed; refusing to create another stage." >&2
    exit 1
  fi
  npm stage publish "${tarball}" --provenance --access public --json > dist/release/npm-stage.json
  node --input-type=module <<'NODE'
import { readFileSync, appendFileSync } from 'node:fs';
import { createHash } from 'node:crypto';
const raw = JSON.parse(readFileSync('dist/release/npm-stage.json', 'utf8'));
const items = Array.isArray(raw) ? raw : [raw, ...Object.values(raw)];
const stage = items.find((item) => item && typeof item === 'object' && typeof item.stageId === 'string');
const integrity = `sha512-${createHash('sha512').update(readFileSync(process.env.CONTRACTS_NPM_TARBALL)).digest('base64')}`;
if (!stage || stage.name !== '@opensoha/contracts' || stage.version !== process.env.CONTRACTS_NPM_VERSION || stage.integrity !== integrity || stage.stageId.length !== 36 || !/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i.test(stage.stageId)) {
  throw new Error('Stage receipt does not match the package, version, integrity or stage ID');
}
appendFileSync(process.env.GITHUB_OUTPUT, `published=false\nstage_id=${stage.stageId}\n`);
appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### npm stage pending approval\n\n@opensoha/contracts@${stage.version} is staged, not public.\n\nStage ID: ${stage.stageId}\n\nA maintainer must approve it with 2FA on npmjs.com. Then dispatch this workflow with the same tag and publish_mode=recover.\n`);
NODE
fi
