import type { ReactNode } from 'react'

export type OverviewTone = 'default' | 'success' | 'warning' | 'danger'

export type OverviewMiniChartConfig =
  | { kind: 'line'; data: number[] }
  | { kind: 'area'; data: number[] }
  | { kind: 'column'; data: number[] }
  | { kind: 'progress'; percent: number }
  | { kind: 'ring'; percent: number }

export interface OverviewMetricItem {
  key: string
  label: ReactNode
  value: ReactNode
  helper?: ReactNode
  icon?: ReactNode
  tone?: OverviewTone
  chart?: OverviewMiniChartConfig
}

export interface OverviewChipItem {
  key: string
  label: ReactNode
  value: ReactNode
  helper?: ReactNode
  icon?: ReactNode
  tone?: OverviewTone
  chart?: OverviewMiniChartConfig
}

export interface OverviewMetricCardProps {
  label: ReactNode
  value: ReactNode
  helper?: ReactNode
  icon?: ReactNode
  tone?: OverviewTone
  className?: string
  chart?: OverviewMiniChartConfig
  loading?: boolean
  variant?: 'metric' | 'pod'
}

export interface OverviewChipProps {
  label: ReactNode
  value: ReactNode
  helper?: ReactNode
  icon?: ReactNode
  tone?: OverviewTone
  className?: string
  chart?: OverviewMiniChartConfig
}

export interface OverviewSectionBarProps {
  title: ReactNode
  description?: ReactNode
  kicker?: ReactNode
  extra?: ReactNode
  className?: string
}
