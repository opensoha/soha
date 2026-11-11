import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { useClusterCapability } from '@/features/platform/cluster-capabilities'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { resolveWorkloadNamespace } from '@/features/platform/workloads-model'
import '@/features/platform/workloads/styles.css'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { toScopeKey } from '@/types'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { App, Card, Spin, Tabs } from 'antd'
import { lazy, Suspense } from 'react'
import { useParams, useSearchParams } from 'react-router-dom'
import { updateWorkloadYAML } from './api'
import { workloadQueries } from './queries'
import type { WorkloadKind } from './types'

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

export function WorkloadYAMLOnlyDetailPage({
  paramKey,
  resource,
  title,
}: {
  paramKey: string
  resource: WorkloadKind
  title: string
}) {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const params = useParams()
  const [searchParams] = useSearchParams()
  const name = params[paramKey] as string
  const { clusterId, namespace } = usePlatformScopeStore()
  const detailNamespace = resolveWorkloadNamespace(namespace, searchParams.get('namespace'))
  const scope = toScopeKey(clusterId, detailNamespace)
  const yamlQueryOptions = workloadQueries.yaml(resource, scope, name)
  const yamlQuery = useQuery(yamlQueryOptions)
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const yamlState = useYamlDraft(
    JSON.stringify([clusterId, resource, detailNamespace, name]),
    visibleYamlQueryData?.content,
  )
  const { value: yamlDraft, setValue: setYamlDraft } = yamlState
  const yamlApplyCapability = useClusterCapability('resource.yaml.apply', localeCode)

  const applyYamlMutation = usePlatformMutation(
    {
      mutationFn: () => updateWorkloadYAML(resource, scope, name, { content: yamlDraft }),
      onSuccess: (resourceYAML) => {
        yamlState.applied(resourceYAML.content)

        void yamlQuery.refetch()
      },
    },
    { label: ['应用 YAML', 'Apply YAML'], target: () => platformResourceTarget({ scope, name }) },
  )
  const platformWriteBlocked = applyYamlMutation.blocked

  return (
    <div className="soha-page soha-workload-detail-page">
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {applyYamlMutation.feedback}

      <Tabs
        className="soha-resource-tabs soha-workload-detail-tabs"
        defaultActiveKey="yaml"
        indicator={{ size: (origin) => Math.max(16, origin - 16), align: 'center' }}
        size="small"
        tabBarGutter={32}
        items={[
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: yamlQuery.isLoading ? (
              <Card className="soha-detail-card">
                <Spin size="large" />
              </Card>
            ) : yamlQuery.isError ? (
              <ManagementState
                compact
                kind="not-found"
                title={localeCode === 'zh_CN' ? `${title}未找到` : `${title} not found`}
              />
            ) : (
              <Suspense
                fallback={
                  <Card className="soha-detail-card">
                    <Spin size="large" />
                  </Card>
                }
              >
                {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
                  <K8sYamlEditor
                    key={yamlState.resourceKey}
                    value={yamlDraft}
                    original={yamlState.original}
                    serverChanged={yamlState.serverChanged}
                    onCompareLatest={yamlState.compareLatest}
                    onChange={setYamlDraft}
                    onReset={() => {
                      yamlState.reset()
                      void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
                    }}
                    onSave={() =>
                      void message.info(
                        localeCode === 'zh_CN'
                          ? '此页面不保存本地草稿'
                          : 'Local draft save disabled here',
                      )
                    }
                    onApply={() => applyYamlMutation.mutate()}
                    saveDisabled
                    applyDisabled={
                      platformWriteBlocked ||
                      !yamlQueryOptions.enabled ||
                      !yamlDraft.trim() ||
                      yamlApplyCapability.disabled
                    }
                    applyDisabledReason={
                      yamlApplyCapability.disabled ? yamlApplyCapability.reason : undefined
                    }
                    applying={applyYamlMutation.isPending}
                  />
                )}
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}
