import type { components } from '@opensoha/contracts/gen/ts/sohaapi'
export type MonitoringView = components['schemas']['ResourceMonitoringView']
export type MonitoringNode = components['schemas']['ResourceMonitoringNode']
export type MonitoringPanel = components['schemas']['ResourceMonitoringPanel']
export type MonitoringSeries = components['schemas']['ResourceMonitoringSeries']
export type MonitoringViewName = components['schemas']['ResourceMonitoringViewName']
export interface MonitoringQuery {
  view: MonitoringViewName
  node?: string
  from?: string
  to?: string
  rangeMinutes: number
  stepSeconds: number
}
