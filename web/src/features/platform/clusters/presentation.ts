import type { Cluster } from './types'

export const clusterTypeOptions = [
  { value: 'standard_kubernetes', labelZh: '标准 Kubernetes', labelEn: 'Standard Kubernetes' },
  { value: 'k3s', labelZh: 'K3s', labelEn: 'K3s' },
  { value: 'gke', labelZh: 'GKE', labelEn: 'GKE' },
  { value: 'eks', labelZh: 'EKS', labelEn: 'EKS' },
  { value: 'ack', labelZh: 'ACK', labelEn: 'ACK' },
  { value: 'tke', labelZh: 'TKE', labelEn: 'TKE' },
  { value: 'aks', labelZh: 'AKS', labelEn: 'AKS' },
]

export function formatClusterType(value: string | undefined, localeCode: string) {
  const item = clusterTypeOptions.find((option) => option.value === value)
  if (!item) return value || '-'
  return localeCode === 'zh_CN' ? item.labelZh : item.labelEn
}

export function clusterTypeOf(cluster: Pick<Cluster, 'region' | 'labels'>) {
  const provider = cluster.labels?.provider
  return typeof provider === 'string' && provider.trim() !== '' ? provider.trim() : cluster.region
}

export function formatConnectionMode(value: string | undefined, localeCode: string) {
  if (value === 'direct_kubeconfig') return localeCode === 'zh_CN' ? '直连' : 'Direct'
  if (value === 'agent') return 'Agent'
  return value || '-'
}

const clusterRuntimeValueLabels: Record<string, [zh: string, en: string]> = {
  agent: ['Agent', 'Agent'],
  api: ['API', 'API'],
  bearer: ['Bearer Token', 'Bearer Token'],
  token: ['Token', 'Token'],
  kubeconfig: ['Kubeconfig', 'Kubeconfig'],
  agent_summary_and_remote_pull: ['Agent 摘要与远程拉取', 'Agent summary and remote pull'],
  informer_cache_then_live_fallback: [
    'Informer 缓存，实时读取回退',
    'Informer cache with live fallback',
  ],
  watch: ['持续监听', 'Watch'],
  ready: ['就绪', 'Ready'],
  warming: ['预热中', 'Warming'],
  disabled: ['已禁用', 'Disabled'],
  healthy: ['正常', 'Healthy'],
  connected: ['已连接', 'Connected'],
  disconnected: ['未连接', 'Disconnected'],
  failed: ['失败', 'Failed'],
  error: ['异常', 'Error'],
  unknown: ['未知', 'Unknown'],
}

export function formatClusterRuntimeValue(value: string | undefined, localeCode: string) {
  const normalized = value?.trim().toLowerCase()
  if (!normalized) return '-'
  const labels = clusterRuntimeValueLabels[normalized]
  if (labels) return labels[localeCode === 'zh_CN' ? 0 : 1]
  return normalized.replace(/[_-]+/g, ' ').replace(/\b\w/g, (character) => character.toUpperCase())
}

export function formatClusterHealth(value: string | undefined, localeCode: string) {
  const normalized = (value || '').trim().toLowerCase()
  if (localeCode !== 'zh_CN') return value || 'unknown'
  if (['healthy', 'connected', 'ready', 'available', 'running', 'normal'].includes(normalized)) {
    return '正常'
  }
  if (['error', 'failed', 'disconnected', 'critical', 'notready', 'lost'].includes(normalized)) {
    return '异常'
  }
  if (['pending', 'warning', 'queued', 'waiting'].includes(normalized)) return '等待'
  if (['syncing', 'checking', 'initializing'].includes(normalized)) return '同步中'
  return value || '未知'
}
