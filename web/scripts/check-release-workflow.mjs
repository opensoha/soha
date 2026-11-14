import { readFile } from 'node:fs/promises'

const workflow = await readFile(
  new URL('../../.github/workflows/release.yml', import.meta.url),
  'utf8',
)
const errors = []

for (const required of [
  'contracts_ref:',
  'CONTRACTS_REF:',
  'npm ci',
  'npm run build:web',
  'python3 scripts/source-workspace.py check-go',
  'Build Web and SDK from the same release commit',
  "startsWith('https://registry.npmjs.org/')",
  '!dist.integrity',
  'integrity !== dist.integrity',
  'GOWORK=off go mod verify',
  'soha-web-source-manifest.json',
  '"webRef": "${GITHUB_SHA}"',
  'gh release download',
]) {
  if (!workflow.includes(required)) {
    errors.push(`release workflow is missing ${required}`)
  }
}

for (const forbidden of [
  'repository: opensoha/soha-contracts',
  '--repo opensoha/soha-web',
  'inputs.web_ref',
  'git describe --tags',
  'npm pkg set',
  'npm install',
  'inputs.contracts_version ||',
]) {
  if (workflow.includes(forbidden)) {
    errors.push(`release workflow must not contain ${forbidden}`)
  }
}

if (errors.length > 0) {
  for (const error of errors) {
    console.error(`release-workflow: ${error}`)
  }
  process.exit(1)
}

console.log('release builds same-source Web/SDK and requires public SDK integrity proof')
