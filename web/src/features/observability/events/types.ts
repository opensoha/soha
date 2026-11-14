import type { ObservabilityPayloadMap } from '../shared/types'

export interface EventStreamEntry {
  id: string
  source: string
  category: string
  severity?: string
  clusterId?: string
  namespace?: string
  summary: string
  payload?: ObservabilityPayloadMap
  occurredAt?: string
}
