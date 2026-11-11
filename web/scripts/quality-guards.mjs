import { createHash } from 'node:crypto'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'

export function sourceEvidence(repo) {
  const git = (...args) =>
    execFileSync('git', ['-C', repo, ...args], { maxBuffer: 64 * 1024 * 1024 })
  const digest = createHash('sha256').update(git('diff', 'HEAD', '--binary'))
  const untracked = git('ls-files', '--others', '--exclude-standard', '-z')
    .toString()
    .split('\0')
    .filter(Boolean)
    .sort()
  for (const path of untracked) digest.update(path).update(readFileSync(`${repo}/${path}`))
  return {
    commit: git('rev-parse', 'HEAD').toString().trim(),
    dirtyPatchDigest: digest.digest('hex'),
  }
}

export function validateVersions(sources, mode) {
  const required = ['web', 'core', 'contracts', ...(mode === 'e2e-agent' ? ['agent'] : [])]
  for (const name of required) {
    if (!/^[a-f0-9]{40}$/.test(sources?.[name]?.commit ?? ''))
      throw new Error(`BLOCKED: exact ${name} SHA required`)
  }
}

export function validateTarget(target, inspected) {
  validateVersions(target?.sources, target?.mode)
  for (const name of [
    'web',
    'core',
    'contracts',
    ...(target.mode === 'e2e-agent' ? ['agent'] : []),
  ]) {
    if (
      !/^[a-f0-9]{64}$/.test(target.sources[name].artifactDigest ?? '') ||
      inspected?.Config?.Labels?.[`soha.test.${name}-sha`] !== target.sources[name].commit ||
      inspected?.Config?.Labels?.[`soha.test.${name}-artifact`] !==
        target.sources[name].artifactDigest
    )
      throw new Error(`BLOCKED: unattested ${name} artifact/version`)
  }
  const url = new URL(target.baseURL)
  if (
    !['http:', 'https:'].includes(url.protocol) ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.pathname !== '/'
  )
    throw new Error('BLOCKED: invalid target URL')
  if (!target.disposable || !target.runId || !/^[a-f0-9]{64}$/.test(target.containerId ?? ''))
    throw new Error('BLOCKED: disposable target identity required')
  if (
    inspected?.Id !== target.containerId ||
    inspected?.Config?.Labels?.['soha.test.run'] !== target.runId ||
    inspected?.Image !== target.imageDigest
  )
    throw new Error('BLOCKED: unknown/shared container identity')
  const bindings = Object.values(inspected.NetworkSettings?.Ports ?? {})
    .flat()
    .filter(Boolean)
  if (
    !['127.0.0.1', 'localhost'].includes(url.hostname) ||
    !bindings.some((item) => item.HostIp === '127.0.0.1' && item.HostPort === url.port)
  )
    throw new Error('BLOCKED: target does not match owned loopback port')
  if (!['readonly', 'test-writer'].includes(target.role))
    throw new Error('BLOCKED: dedicated test role required')
  return target
}

export function validateCleanup(lease, observed, runId) {
  if (
    !lease?.id ||
    !lease?.uid ||
    lease.runId !== runId ||
    observed?.runId !== runId ||
    observed?.id !== lease.id ||
    observed?.uid !== lease.uid
  )
    throw new Error('Cleanup rejected: exact run/id/UID ownership mismatch')
  return { id: lease.id, uid: lease.uid }
}

export function aiPreflight(env) {
  if (env.SOHA_AI_ENABLED !== '1') return { result: 'NOT_RUN' }
  for (const key of [
    'MIDSCENE_MODEL_BASE_URL',
    'MIDSCENE_MODEL_API_KEY',
    'MIDSCENE_MODEL_NAME',
    'MIDSCENE_MODEL_FAMILY',
    'SOHA_AI_ALLOWED_HOST',
    'SOHA_AI_APPROVAL_ID',
    'SOHA_AI_MAX_CALLS',
  ]) {
    if (!env[key]) throw new Error(`BLOCKED: missing ${key}`)
  }
  const url = new URL(env.MIDSCENE_MODEL_BASE_URL)
  if (
    url.protocol !== 'https:' ||
    url.username ||
    url.password ||
    url.search ||
    url.hash ||
    url.hostname !== env.SOHA_AI_ALLOWED_HOST
  )
    throw new Error('BLOCKED: unapproved model endpoint')
  if (!/^(?:[1-9]|1[0-2])$/.test(env.SOHA_AI_MAX_CALLS))
    throw new Error('BLOCKED: request budget must be 1..12')
  return { result: 'ENABLED', host: url.hostname, maxCalls: Number(env.SOHA_AI_MAX_CALLS) }
}

export function assertTrustedDispatch(ref, sha, approved) {
  if (ref !== 'refs/heads/main' || !/^[a-f0-9]{40}$/.test(sha ?? '') || sha !== approved)
    throw new Error('BLOCKED: trusted main SHA and explicit approval required')
}

export function redact(value, sensitiveValues = []) {
  if (Array.isArray(value)) return value.map((item) => redact(item, sensitiveValues))
  if (value && typeof value === 'object')
    return Object.fromEntries(
      Object.entries(value).map(([key, item]) => [
        key,
        /authorization|cookie|token|password|secret|kubeconfig|yaml|content/i.test(key)
          ? '[REDACTED]'
          : redact(item, sensitiveValues),
      ]),
    )
  if (typeof value === 'string') {
    let text = value.replace(/Bearer\s+\S+/gi, 'Bearer [REDACTED]')
    for (const secret of sensitiveValues.filter(Boolean))
      text = text.split(secret).join('[REDACTED]')
    return text
  }
  return value
}
