import { lazy, Suspense, useEffect, useState } from 'react'
import {
  Alert, Button, Card, Descriptions, Modal, Space,
  Spin, Switch, Tabs, Tag, Typography, message,
} from 'antd'
import { CopyOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ManagementDetailHeader, ManagementState } from '@/components/management-list'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import { buildClusterScopedPath } from '@/features/platform/platform-scope-query'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { formatRelativeTime } from '@/utils/time'
import type { ApiResponse, ResourceYAMLView } from '@/types'
import './platform-pages.css'

const { Text, Paragraph } = Typography

const K8sYamlEditor = lazy(async () => {
  const mod = await import('@/components/k8s-yaml-editor')
  return { default: mod.K8sYamlEditor }
})

interface ConfigMapDetail {
  name: string
  namespace: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  data?: Record<string, string>
  binaryData?: Record<string, string>
  immutable: boolean
  createdAt?: string
  ageSeconds: number
}

interface SecretDetail {
  name: string
  namespace: string
  type: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  data?: Record<string, string>
  immutable: boolean
  createdAt?: string
  ageSeconds: number
}

const DEFAULT_CONFIGMAP_TEMPLATE = `apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
data:
  key: value
`

const DEFAULT_SECRET_TEMPLATE = `apiVersion: v1
kind: Secret
metadata:
  name: example-secret
type: Opaque
stringData:
  key: value
`

export function CreateResourceModal({
  visible,
  onClose,
  kind,
  resourcePath,
  defaultTemplate,
  invalidationKeys,
  namespaceScope = 'required',
}: {
  visible: boolean
  onClose: () => void
  kind: string
  resourcePath: string
  defaultTemplate: string
  invalidationKeys: unknown[][]
  namespaceScope?: 'cluster' | 'required'
}) {
  const { t, localeCode } = useI18n()
  const { clusterId, namespace } = usePlatformScopeStore()
  const queryClient = useQueryClient()
  const [draft, setDraft] = useState(defaultTemplate)
  const effectiveNamespace = namespaceScope === 'cluster' ? null : (namespace || 'default')

  useEffect(() => {
    if (visible) setDraft(defaultTemplate)
  }, [visible, defaultTemplate])

  const createMutation = useMutation({
    mutationFn: () =>
      api.post<ApiResponse<ResourceYAMLView>>(
        buildClusterScopedPath(clusterId!, resourcePath, effectiveNamespace),
        { content: draft, ...(effectiveNamespace ? { namespace: effectiveNamespace } : {}) },
      ),
    onSuccess: () => {
      void message.success(localeCode === 'zh_CN' ? `${kind} 已创建` : `${kind} created`)
      invalidationKeys.forEach((key) => queryClient.invalidateQueries({ queryKey: key }))
      onClose()
    },
    onError: (err: Error) => void message.error(err.message),
  })

  return (
    <Modal
      title={localeCode === 'zh_CN' ? `新建 ${kind}` : `Create ${kind}`}
      open={visible}
      onCancel={onClose}
      footer={null}
      width={960}
      mask={{ closable: false }}
    >
      {!clusterId ? (
        <ManagementState compact kind="select-scope" description={localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first'} />
      ) : (
        <Suspense fallback={<div style={{ height: 520, display: 'flex', alignItems: 'center', justifyContent: 'center' }}><Spin size="large" /></div>}>
          <Alert
            banner
            showIcon
            type="info"
            description={localeCode === 'zh_CN'
              ? (namespaceScope === 'cluster'
                ? '当前资源为 cluster scope，metadata.namespace 必须留空。'
                : `命名空间：${effectiveNamespace || 'default'}。可在 YAML 内指定 metadata.namespace 覆盖。`)
              : (namespaceScope === 'cluster'
                ? 'This resource is cluster-scoped. Keep metadata.namespace empty.'
                : `Namespace: ${effectiveNamespace || 'default'}. You can override via metadata.namespace in YAML.`)}
          />
          <div style={{ marginTop: 12 }}>
            <K8sYamlEditor
              value={draft}
              onChange={setDraft}
              onReset={() => setDraft(defaultTemplate)}
              onSave={() => void message.info(localeCode === 'zh_CN' ? '新建模式不支持本地草稿' : 'Draft saving disabled in create mode')}
              onApply={() => createMutation.mutate()}
              saveDisabled
              applyDisabled={!draft.trim() || createMutation.isPending}
              applying={createMutation.isPending}
              editorHeight="min(46vh, 440px)"
            />
          </div>
          <div style={{ marginTop: 12, textAlign: 'right' }}>
            <Button onClick={onClose} style={{ marginRight: 8 }}>{t('common.cancel', 'Cancel')}</Button>
          </div>
        </Suspense>
      )}
    </Modal>
  )
}

function useResolvedNamespace() {
  const [searchParams] = useSearchParams()
  const { namespace } = usePlatformScopeStore()
  return (namespace && namespace !== '') ? namespace : (searchParams.get('namespace') || '')
}

function copyToClipboard(value: string, localeCode: string) {
  if (!navigator.clipboard) return
  navigator.clipboard.writeText(value).then(
    () => void message.success(localeCode === 'zh_CN' ? '已复制' : 'Copied'),
    () => void message.error(localeCode === 'zh_CN' ? '复制失败' : 'Copy failed'),
  )
}

function renderEntries(
  entries: Record<string, string> | undefined,
  localeCode: string,
  transform?: (value: string) => string,
  emptyHint?: string,
) {
  const keys = Object.keys(entries ?? {})
  if (keys.length === 0) {
    return <ManagementState bordered={false} compact description={emptyHint ?? (localeCode === 'zh_CN' ? '暂无数据' : 'No data')} />
  }
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      {keys.sort().map((key) => {
        const raw = entries?.[key] ?? ''
        const display = transform ? transform(raw) : raw
        return (
          <Card key={key} className="soha-detail-card" bodyStyle={{ padding: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 6 }}>
              <Text strong>{key}</Text>
              <Button size="small" type="text" icon={<CopyOutlined />} onClick={() => copyToClipboard(display, localeCode)}>
                {localeCode === 'zh_CN' ? '复制' : 'Copy'}
              </Button>
            </div>
            <Paragraph style={{ margin: 0 }}>
              <pre className="soha-json-block" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all', margin: 0 }}>{display}</pre>
            </Paragraph>
          </Card>
        )
      })}
    </div>
  )
}

function decodeBase64Safe(value: string) {
  try {
    if (typeof atob === 'function') {
      const binary = atob(value)
      const bytes = Uint8Array.from(binary, (c) => c.charCodeAt(0))
      return new TextDecoder('utf-8', { fatal: false }).decode(bytes)
    }
    return value
  } catch {
    return value
  }
}

export function ResourceMetaOverview({
  name, namespace, createdAt, labels, annotations, extra,
}: {
  name: string
  namespace: string
  createdAt?: string
  labels?: Record<string, string>
  annotations?: Record<string, string>
  extra?: Array<{ key: string; value: React.ReactNode }>
}) {
  const { t, localeCode } = useI18n()
  return (
    <Card className="soha-detail-card">
      <Descriptions
        items={[
          { key: t('common.name', 'Name'), label: t('common.name', 'Name'), children: name },
          { key: t('common.namespace', 'Namespace'), label: t('common.namespace', 'Namespace'), children: namespace },
          { key: t('common.createdAt', 'Created At'), label: t('common.createdAt', 'Created At'), children: createdAt ? formatRelativeTime(createdAt) : '-' },
          ...(extra ?? []).map((item) => ({ key: item.key, label: item.key, children: item.value })),
        ]}
      />
      {labels && Object.keys(labels).length > 0 && (
        <div className="soha-detail-meta">
          <Text strong>{`${t('common.labels', 'Labels')}:`}</Text>
          <div className="soha-tag-list">
            {Object.entries(labels).map(([k, v]) => (
              <Tag key={k}>{k}={v}</Tag>
            ))}
          </div>
        </div>
      )}
      {annotations && Object.keys(annotations).length > 0 && (
        <div className="soha-detail-meta">
          <Text strong>{`${localeCode === 'zh_CN' ? '注解' : 'Annotations'}:`}</Text>
          <pre className="soha-json-block">{JSON.stringify(annotations, null, 2)}</pre>
        </div>
      )}
    </Card>
  )
}

export function useResourceYAMLState(yamlPath: string | null, resource: string, name: string, namespace: string) {
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const yamlQuery = useQuery({
    queryKey: ['resource-yaml', resource, name, namespace],
    queryFn: () => api.get<ApiResponse<ResourceYAMLView>>(yamlPath!),
    enabled: !!yamlPath,
  })
  const [draft, setDraft] = useState('')
  const serverValue = yamlQuery.data?.data?.content ?? ''

  useEffect(() => {
    if (!yamlPath) return
    setDraft(serverValue)
  }, [yamlPath, serverValue])

  const applyMutation = useMutation({
    mutationFn: () => api.put<ApiResponse<ResourceYAMLView>>(yamlPath!, { content: draft }),
    onSuccess: (response) => {
      setDraft(response.data?.content ?? draft)
      void message.success(t('yamlEditor.applySuccess', 'YAML applied'))
      yamlQuery.refetch()
      queryClient.invalidateQueries({ queryKey: [resource, 'detail', name, namespace] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  return { draft, setDraft, serverValue, applyMutation, yamlQuery }
}

export function ConfigMapDetailPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const params = useParams()
  const name = params.configMapName as string
  const detailNamespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()

  const detailPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/configuration/configmaps/${name}/detail?namespace=${encodeURIComponent(detailNamespace)}`
    : null
  const yamlPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/configuration/configmaps/${name}/yaml?namespace=${encodeURIComponent(detailNamespace)}`
    : null

  const detailQuery = useQuery({
    queryKey: ['configmaps', 'detail', name, detailNamespace],
    queryFn: () => api.get<ApiResponse<ConfigMapDetail>>(detailPath!),
    enabled: !!detailPath,
  })

  const yamlState = useResourceYAMLState(yamlPath, 'configmaps', name, detailNamespace)
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }
  if (!detail) {
    return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? 'ConfigMap 未找到' : 'ConfigMap not found'} /></div>
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={`ConfigMap: ${name}`}
        description={localeCode === 'zh_CN' ? '查看 ConfigMap 的键值数据与 YAML。' : 'Inspect ConfigMap data entries and YAML.'}
        actions={<Button onClick={() => navigate('/configuration/configmaps')}>{localeCode === 'zh_CN' ? '返回列表' : 'Back to list'}</Button>}
      />
      <Tabs
        defaultActiveKey="overview"
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <>
                <ResourceMetaOverview
                  name={detail.name}
                  namespace={detail.namespace}
                  createdAt={detail.createdAt}
                  labels={detail.labels}
                  annotations={detail.annotations}
                  extra={[{ key: 'Immutable', value: detail.immutable ? 'Yes' : 'No' }]}
                />
                <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? `数据 (${Object.keys(detail.data ?? {}).length})` : `Data (${Object.keys(detail.data ?? {}).length})`}>
                  {renderEntries(detail.data, localeCode, undefined, localeCode === 'zh_CN' ? '暂无 data 键' : 'No data keys')}
                </Card>
                {detail.binaryData && Object.keys(detail.binaryData).length > 0 ? (
                  <Card className="soha-detail-card" title={localeCode === 'zh_CN' ? `二进制数据 (base64, ${Object.keys(detail.binaryData).length})` : `Binary Data (base64, ${Object.keys(detail.binaryData).length})`}>
                    {renderEntries(detail.binaryData, localeCode)}
                  </Card>
                ) : null}
              </>
            ),
          },
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: (
              <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
                <div style={{ height: 620 }}>
                  <K8sYamlEditor
                    value={yamlState.draft}
                    onChange={yamlState.setDraft}
                    onReset={() => yamlState.setDraft(yamlState.serverValue)}
                    onSave={() => void message.info(localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here')}
                    onApply={() => yamlState.applyMutation.mutate()}
                    saveDisabled
                    applyDisabled={!yamlPath || !yamlState.draft.trim()}
                    applying={yamlState.applyMutation.isPending}
                  />
                </div>
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}

export function SecretDetailPage() {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const params = useParams()
  const name = params.secretName as string
  const detailNamespace = useResolvedNamespace()
  const { clusterId } = usePlatformScopeStore()
  const [decoded, setDecoded] = useState(false)

  const detailPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/configuration/secrets/${name}/detail?namespace=${encodeURIComponent(detailNamespace)}`
    : null
  const yamlPath = clusterId && detailNamespace
    ? `/clusters/${clusterId}/configuration/secrets/${name}/yaml?namespace=${encodeURIComponent(detailNamespace)}`
    : null

  const detailQuery = useQuery({
    queryKey: ['secrets', 'detail', name, detailNamespace],
    queryFn: () => api.get<ApiResponse<SecretDetail>>(detailPath!),
    enabled: !!detailPath,
  })

  const yamlState = useResourceYAMLState(yamlPath, 'secrets', name, detailNamespace)
  const detail = detailQuery.data?.data

  if (detailQuery.isLoading) {
    return <div className="flex items-center justify-center h-64"><Spin size="large" /></div>
  }
  if (!detail) {
    return <div className="soha-page"><ManagementState kind="not-found" description={localeCode === 'zh_CN' ? 'Secret 未找到' : 'Secret not found'} /></div>
  }

  return (
    <div className="soha-page">
      <ManagementDetailHeader
        title={`Secret: ${name}`}
        description={localeCode === 'zh_CN' ? '查看 Secret 的数据(默认 base64)与 YAML。敏感信息仅限授权查看。' : 'Inspect Secret data (base64 by default) and YAML.'}
        actions={<Button onClick={() => navigate('/configuration/secrets')}>{localeCode === 'zh_CN' ? '返回列表' : 'Back to list'}</Button>}
      />
      <Tabs
        defaultActiveKey="overview"
        items={[
          {
            key: 'overview',
            label: t('common.overview', 'Overview'),
            children: (
              <>
                <ResourceMetaOverview
                  name={detail.name}
                  namespace={detail.namespace}
                  createdAt={detail.createdAt}
                  labels={detail.labels}
                  annotations={detail.annotations}
                  extra={[
                    { key: 'Type', value: detail.type || '-' },
                    { key: 'Immutable', value: detail.immutable ? 'Yes' : 'No' },
                  ]}
                />
                <Card
                  className="soha-detail-card"
                  title={localeCode === 'zh_CN' ? `数据 (${Object.keys(detail.data ?? {}).length})` : `Data (${Object.keys(detail.data ?? {}).length})`}
                  extra={(
                    <Space>
                      <Text type="secondary" className="text-xs">{localeCode === 'zh_CN' ? '解码显示' : 'Decode'}</Text>
                      <Switch checked={decoded} onChange={setDecoded} />
                    </Space>
                  )}
                >
                  {renderEntries(
                    detail.data,
                    localeCode,
                    decoded ? decodeBase64Safe : undefined,
                    localeCode === 'zh_CN' ? '暂无 data 键' : 'No data keys',
                  )}
                </Card>
              </>
            ),
          },
          {
            key: 'yaml',
            label: t('common.yaml', 'YAML'),
            children: (
              <Suspense fallback={<Card className="soha-detail-card"><Spin size="large" /></Card>}>
                <div style={{ height: 620 }}>
                  <K8sYamlEditor
                    value={yamlState.draft}
                    onChange={yamlState.setDraft}
                    onReset={() => yamlState.setDraft(yamlState.serverValue)}
                    onSave={() => void message.info(localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here')}
                    onApply={() => yamlState.applyMutation.mutate()}
                    saveDisabled
                    applyDisabled={!yamlPath || !yamlState.draft.trim()}
                    applying={yamlState.applyMutation.isPending}
                  />
                </div>
              </Suspense>
            ),
          },
        ]}
      />
    </div>
  )
}

export const CONFIGMAP_DEFAULT_TEMPLATE = DEFAULT_CONFIGMAP_TEMPLATE
export const SECRET_DEFAULT_TEMPLATE = DEFAULT_SECRET_TEMPLATE
