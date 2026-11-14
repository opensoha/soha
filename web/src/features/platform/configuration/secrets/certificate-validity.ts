import type { SecretCertificate } from './types'

export function certificateValidity(certificate: SecretCertificate | undefined, now = Date.now()) {
  const start = Date.parse(certificate?.notBefore || '')
  const end = Date.parse(certificate?.notAfter || '')
  if (
    certificate?.status !== 'available' ||
    !Number.isFinite(start) ||
    !Number.isFinite(end) ||
    end <= start
  )
    return { state: 'unknown' as const }
  if (now < start) return { state: 'future' as const }
  if (now >= end) return { state: 'expired' as const }
  return {
    state: end - now <= 30 * 86400000 ? ('expiring' as const) : ('valid' as const),
    days: Math.ceil((end - now) / 86400000),
  }
}
