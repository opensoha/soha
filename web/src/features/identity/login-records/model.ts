import type { AuditLog } from '@/features/system'

export const APPLICATION_LOGIN_ACTIONS: Record<string, string> = {
  'oidc.authorize': 'oidcAuthorize',
  'oidc.token': 'oidcToken',
  'identity.saml.sso': 'saml',
  'proxy.allow': 'proxyAllow',
  'proxy.deny': 'proxyDeny',
  'proxy.login': 'proxyLogin',
}
const OUTPOST_LOGIN_ACTIONS: Record<string, string> = {
  proxy_allow: 'proxyAllow',
  proxy_deny: 'proxyDeny',
  proxy_login: 'proxyLogin',
}

export const APPLICATION_LOGIN_ACTION_PREFIXES = [
  ...Object.keys(APPLICATION_LOGIN_ACTIONS),
  'outpost.event',
].join(',')
// ponytail: reuse the audit endpoint's 500-row limit; narrow dates until cursor pagination is needed.
export const APPLICATION_LOGIN_LIMIT = 500

function text(value: unknown) {
  return typeof value === 'string' ? value.trim() : ''
}

export function applicationLoginRecord(record: AuditLog) {
  const metadata = record.metadata ?? {}
  const event = metadata.event
  const details =
    record.action === 'outpost.event' && event && typeof event === 'object' && !Array.isArray(event)
      ? (event as Record<string, unknown>)
      : metadata
  const stage =
    record.action === 'outpost.event'
      ? OUTPOST_LOGIN_ACTIONS[text(details.eventType)]
      : APPLICATION_LOGIN_ACTIONS[record.action]
  // Anonymous bypasses, refreshes and configuration changes are not application logins.
  if (!stage || details.skipped === true) return null
  const result = record.result.toLowerCase()
  return {
    ...record,
    stage,
    applicationId: text(details.applicationId) || text(metadata.applicationId),
    applicationName: text(details.applicationName) || text(metadata.applicationName),
    reason: text(details.reason),
    outcome: ['success', 'allow', 'allowed'].includes(result)
      ? 'success'
      : ['failure', 'failed', 'error', 'deny', 'denied'].includes(result)
        ? 'failure'
        : result,
  }
}

export type ApplicationLoginRecord = NonNullable<ReturnType<typeof applicationLoginRecord>>
