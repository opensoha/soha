export interface MonitoringSummary {
  channelCount: number
  criticalCount: number
  firingCount: number
  infoCount: number
  lastReceivedAt?: string
  resolvedCount: number
  totalCount: number
  warningCount: number
}
