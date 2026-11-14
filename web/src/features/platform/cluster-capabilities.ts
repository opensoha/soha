import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import type {
  ClusterCapabilityMatrixEntry,
  ClusterCapabilityModeSupport,
  ClusterCapabilityRiskLevel,
  ClusterCapabilityStatus,
} from '@/types'
import { clusterQueries } from './clusters/queries'

type CapabilityMode = 'direct' | 'agent'
type LocaleCode = 'zh_CN' | 'en_US'

const AGENT_CAPABILITY_REASONS_ZH: Partial<Record<string, string>> = {
  'namespace.lifecycle': '升级后的 Agent 支持命名空间查询、创建、元数据编辑和删除。',
  'configuration.inventory':
    '升级后的 Agent 支持 ConfigMap 和 Secret 详情、数据编辑与引用查询；Secret 数据需要独立查看权限。',
  'storage.inventory': 'Agent 支持 PVC、PV 和 StorageClass 的查询与详情，创建由资源创建能力控制。',
  'rbac.inventory': 'Agent 支持 RBAC 查询与权限检查，创建由资源创建能力控制。',
  'custom.resources':
    'Agent 模式支持 CRD 发现；自定义资源读取和变更需要为目标 API 组与资源配置明确的 Kubernetes RBAC。',
  'helm.releases': 'Agent 模式支持 Helm Release 的查询、安装、更新和删除。',
  'pod.exec': 'Agent 模式支持非交互命令执行和交互式终端。',
  'pod.logs': 'Agent 模式支持 Pod 日志快照和流式日志。',
  'port.forward': 'Agent 模式支持实时端口转发。',
  'resource.yaml.apply':
    'Agent 模式支持内置资源和自定义资源的 YAML 应用与删除；需要升级 Agent 并配置对应动作与 Kubernetes RBAC。',
  'resource.yaml.view':
    'Agent 模式支持读取内置资源和自定义资源 YAML；需要升级 Agent 并配置对应动作与 Kubernetes RBAC。',
  'workload.mutations': '升级后的 Agent 支持工作负载快捷操作、Pod 删除以及 CronJob 暂停和恢复。',
}

export interface ClusterCapabilityDecision {
  disabled: boolean
  entry?: ClusterCapabilityMatrixEntry
  isLoading: boolean
  mode?: CapabilityMode
  notes: string[]
  requiredScopes: string[]
  requiresApproval: boolean
  reason: string
  riskLevel?: ClusterCapabilityRiskLevel
  docsUrl?: string
  status: ClusterCapabilityStatus | 'unknown'
}

function capabilityModeFor(connectionMode?: string): CapabilityMode | undefined {
  if (!connectionMode) return undefined
  if (connectionMode === 'agent') return 'agent'
  if (connectionMode === 'direct' || connectionMode === 'direct_kubeconfig') return 'direct'
  return undefined
}

function fallbackUnsupportedReason(localeCode: LocaleCode) {
  return localeCode === 'zh_CN'
    ? '当前集群连接模式暂不支持该操作。'
    : 'The current cluster connection mode does not support this operation.'
}

function fallbackUnknownReason(localeCode: LocaleCode, isLoading: boolean) {
  if (isLoading) {
    return localeCode === 'zh_CN'
      ? '正在确认当前集群的运行能力。'
      : 'Checking the current cluster capabilities.'
  }
  return localeCode === 'zh_CN'
    ? '暂时无法确认当前集群是否支持该操作。'
    : 'Unable to confirm whether the current cluster supports this operation.'
}

function notesFromSupport(support: ClusterCapabilityModeSupport | undefined) {
  return (support?.notes ?? []).map((item) => item.trim()).filter(Boolean)
}

function capabilityReason(
  key: string,
  localeCode: LocaleCode,
  mode: CapabilityMode,
  support: ClusterCapabilityModeSupport,
  notes: string[],
) {
  const sourceReason = [...new Set([support.reason?.trim(), ...notes].filter(Boolean))].join(' / ')
  if (sourceReason) return sourceReason
  if (support.status === 'unsupported') return fallbackUnsupportedReason(localeCode)
  if (support.status === 'partial') {
    return localeCode === 'zh_CN'
      ? '当前集群连接模式仅部分支持该能力。'
      : 'The current cluster connection mode only partially supports this capability.'
  }
  return localeCode === 'zh_CN' && mode === 'agent' ? AGENT_CAPABILITY_REASONS_ZH[key] || '' : ''
}

export function evaluateClusterCapability({
  connectionMode,
  key,
  localeCode,
  matrix,
  isError = false,
  isLoading = false,
}: {
  connectionMode?: string
  key: string
  localeCode: LocaleCode
  matrix?: ClusterCapabilityMatrixEntry[]
  isError?: boolean
  isLoading?: boolean
}): ClusterCapabilityDecision {
  const mode = capabilityModeFor(connectionMode)
  const entry = (matrix ?? []).find((item) => item.key === key)
  if (isLoading || isError || !mode || !entry) {
    return {
      disabled: true,
      entry,
      isLoading,
      mode,
      notes: [],
      requiredScopes: [],
      requiresApproval: false,
      reason: fallbackUnknownReason(localeCode, isLoading),
      status: 'unknown',
    }
  }

  const support = entry[mode]
  const notes = notesFromSupport(support)
  const disabled = support.status === 'unsupported'
  const reason = capabilityReason(key, localeCode, mode, support, notes)

  return {
    disabled,
    entry,
    isLoading: false,
    mode,
    notes,
    requiredScopes: entry.requiredScopes ?? [],
    requiresApproval: entry.requiresApproval,
    reason,
    riskLevel: entry.riskLevel,
    docsUrl: entry.docsUrl,
    status: support.status,
  }
}

export function useClusterCapability(
  key: string,
  localeCode: LocaleCode,
): ClusterCapabilityDecision {
  const { clusterId } = usePlatformScopeStore()
  return useClusterCapabilityForCluster(key, localeCode, clusterId)
}

export function useClusterCapabilityForCluster(
  key: string,
  localeCode: LocaleCode,
  clusterId?: string | null,
): ClusterCapabilityDecision {
  const clustersQuery = useQuery({ ...clusterQueries.list(), enabled: !!clusterId })
  const capabilitiesQuery = useQuery({ ...clusterQueries.capabilities(), enabled: !!clusterId })

  const connectionMode = useMemo(
    () => (clustersQuery.data ?? []).find((item) => item.id === clusterId)?.connectionMode,
    [clusterId, clustersQuery.data],
  )

  return useMemo(
    () => ({
      ...evaluateClusterCapability({
        connectionMode,
        key,
        localeCode,
        matrix: capabilitiesQuery.data,
        isError: clustersQuery.isError || capabilitiesQuery.isError,
        isLoading: clustersQuery.isLoading || capabilitiesQuery.isLoading,
      }),
    }),
    [
      capabilitiesQuery.data,
      capabilitiesQuery.isError,
      capabilitiesQuery.isLoading,
      clustersQuery.isError,
      clustersQuery.isLoading,
      connectionMode,
      key,
      localeCode,
    ],
  )
}

export function capabilityActionTooltip(label: string, capability: ClusterCapabilityDecision) {
  return capability.reason ? `${label}: ${capability.reason}` : label
}
