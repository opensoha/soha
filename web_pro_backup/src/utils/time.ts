import { usePreferencesStore } from '@/stores/preferences-store'

function getLocaleCode() {
  return usePreferencesStore.getState().localeCode === 'en_US' ? 'en-US' : 'zh-CN'
}

export function formatDateTime(value?: string | null) {
  if (!value) return '-'

  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value

  return new Intl.DateTimeFormat(getLocaleCode(), {
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  }).format(date)
}

export function formatRelativeTime(value?: string | null) {
  if (!value) return '-'

  const timestamp = new Date(value).getTime()
  if (Number.isNaN(timestamp)) return value

  const diff = Date.now() - timestamp
  const days = Math.floor(diff / 86400000)
  const hours = Math.floor((diff % 86400000) / 3600000)
  if (getLocaleCode() === 'en-US') {
    if (days > 0) return `${days}d ${hours}h`
    const mins = Math.floor((diff % 3600000) / 60000)
    return hours > 0 ? `${hours}h ${mins}m` : `${mins}m`
  }
  if (days > 0) return `${days}d${hours}h`
  const mins = Math.floor((diff % 3600000) / 60000)
  return hours > 0 ? `${hours}h${mins}m` : `${mins}m`
}

export function formatAgeSeconds(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '-'
  const total = Math.max(0, Math.floor(value))
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  if (getLocaleCode() === 'en-US') {
    if (days > 0) return `${days}d ${hours}h`
    const mins = Math.floor((total % 3600) / 60)
    return hours > 0 ? `${hours}h ${mins}m` : `${mins}m`
  }
  if (days > 0) return `${days}d${hours}h`
  const mins = Math.floor((total % 3600) / 60)
  return hours > 0 ? `${hours}h${mins}m` : `${mins}m`
}
