import { useMemo } from 'react'
import { useSearchParams } from 'react-router-dom'
import { PlatformScopeToolbar } from '@/components/platform-scope-toolbar'
import { useAIPageContext } from '@/features/copilot'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { LogExplorer } from './log-explorer'
import type { LogTarget } from './api'
import { readLogExplorerPreset } from './model'

export function LogsPage() {
  const [searchParams] = useSearchParams()
  const preset = useMemo(() => readLogExplorerPreset(searchParams), [searchParams])
  const { clusterId, namespace } = usePlatformScopeStore()
  const target = useMemo<LogTarget | undefined>(() => {
    if (preset.source === 'docker') {
      return {
        kind: 'docker',
        projectId: preset.dockerProjectId ?? '',
        serviceName: preset.dockerService ?? '',
      }
    }
    if (preset.source === 'delivery') {
      return {
        kind: 'delivery',
        applicationId: preset.applicationId ?? '',
        environmentId: preset.environmentId ?? '',
        namespace: preset.namespace ?? undefined,
      }
    }
    return undefined
  }, [preset])
  useAIPageContext({
    sourceWorkbench: 'monitoring',
    sourceTitle: '日志调查',
    entityKind:
      preset.source === 'docker'
        ? 'monitoring.logs.docker'
        : preset.source === 'delivery'
          ? 'monitoring.logs.delivery'
          : 'monitoring.logs',
    entityName:
      preset.workloadName || preset.dockerService || preset.applicationId || clusterId || '日志',
    clusterId: preset.clusterId || clusterId || undefined,
    namespace: preset.namespace || namespace || undefined,
    workload: preset.workloadName,
    service: preset.service || preset.dockerService || undefined,
    applicationId: preset.applicationId || undefined,
    timeRangeMinutes: preset.sinceSeconds ? Math.ceil(preset.sinceSeconds / 60) : undefined,
    visibleFilters: { ...preset },
    pinnedData: { source: preset.source, targetKind: target?.kind ?? 'cluster' },
    promptHint: '分析当前日志范围的错误模式、时间关联、Trace 线索和影响对象，并给出可打开的证据。',
  })

  return (
    <div className="soha-page">
      <LogExplorer
        clusterId={preset.clusterId || clusterId}
        namespace={preset.namespace || namespace}
        preset={preset}
        scopeControl={
          <PlatformScopeToolbar
            clusterWidth={180}
            embedded
            namespaceWidth={180}
            showLabel={false}
          />
        }
        syncURL
        target={target}
      />
    </div>
  )
}
