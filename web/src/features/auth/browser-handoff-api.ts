import type {
  BrowserHandoffCompletionEnvelope,
  BrowserHandoffEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { api } from '@/services/api-client'

function browserHandoffPath(handoffId: string) {
  return `/auth/browser-handoffs/${encodeURIComponent(handoffId.trim())}`
}

export async function inspectBrowserHandoff(handoffId: string) {
  const response = await api.getEnvelope<BrowserHandoffEnvelope>(browserHandoffPath(handoffId))
  return response.data
}

export async function completeBrowserHandoff(handoffId: string) {
  const response = await api.post<BrowserHandoffCompletionEnvelope>(
    `${browserHandoffPath(handoffId)}/complete`,
  )
  return response.data
}
