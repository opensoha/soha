import { mkdtemp, readFile, rm, stat } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { basename, join } from 'node:path'
import { tmpdir } from 'node:os'
import { spawnSync } from 'node:child_process'

const artifact = valueArg('--artifact')
if (!artifact) {
  throw new Error('--artifact is required')
}

const checksumFile = valueArg('--checksum') ?? `${artifact}.sha256`
const artifactName = basename(artifact)
if (!/^soha-web-dist-v?[0-9A-Za-z._-]+\.tar\.gz$/.test(artifactName)) {
  throw new Error(`unexpected web dist artifact name: ${artifactName}`)
}

await requireFile(artifact)
await requireFile(checksumFile)

const checksumText = (await readFile(checksumFile, 'utf8')).trim()
const checksumMatch = checksumText.match(/^([a-f0-9]{64})\s+\*?(.+)$/)
if (!checksumMatch) {
  throw new Error(`${checksumFile} must contain '<sha256>  <artifact>'`)
}

const [, expectedSha256, checksumArtifactName] = checksumMatch
if (checksumArtifactName !== artifactName) {
  throw new Error(`${checksumFile} names ${checksumArtifactName}, expected ${artifactName}`)
}

const actualSha256 = createHash('sha256').update(await readFile(artifact)).digest('hex')
if (actualSha256 !== expectedSha256) {
  throw new Error(`${artifactName} sha256 mismatch: expected ${expectedSha256}, got ${actualSha256}`)
}

const members = run('tar', ['-tzf', artifact]).stdout
  .split('\n')
  .map((line) => line.trim())
  .filter(Boolean)

if (!members.includes('./index.html') && !members.includes('index.html')) {
  throw new Error(`${artifactName} does not contain index.html`)
}
if (!members.some((member) => member.startsWith('./assets/') || member.startsWith('assets/'))) {
  throw new Error(`${artifactName} does not contain built assets`)
}
for (const member of members) {
  if (member.startsWith('/') || member.includes('..')) {
    throw new Error(`${artifactName} contains unsafe tar member ${member}`)
  }
}

const extractDir = await mkdtemp(join(tmpdir(), 'soha-web-dist-'))
try {
  run('tar', ['-xzf', artifact, '-C', extractDir])
  const indexHtml = await readFile(join(extractDir, 'index.html'), 'utf8')
  if (!indexHtml.includes('<div id="root">')) {
    throw new Error(`${artifactName} index.html is missing the React root element`)
  }
  if (!indexHtml.includes('/assets/')) {
    throw new Error(`${artifactName} index.html does not reference built assets`)
  }
} finally {
  await rm(extractDir, { recursive: true, force: true })
}

console.log(`web release artifact verified: ${artifact}`)
console.log(`sha256: ${actualSha256}`)
console.log(`members: ${members.length}`)

function valueArg(name) {
  const prefix = `${name}=`
  const value = process.argv.find((arg) => arg.startsWith(prefix))
  if (value) {
    return value.slice(prefix.length)
  }
  const index = process.argv.indexOf(name)
  return index >= 0 ? process.argv[index + 1] : undefined
}

async function requireFile(path) {
  const info = await stat(path)
  if (!info.isFile() || info.size === 0) {
    throw new Error(`${path} is missing or empty`)
  }
}

function run(command, args) {
  const result = spawnSync(command, args, {
    encoding: 'utf8',
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  if (result.status !== 0) {
    throw new Error(`${command} ${args.join(' ')} failed\n${result.stdout}${result.stderr}`)
  }
  return result
}
