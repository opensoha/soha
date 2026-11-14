import { ManagementState } from '@/components/management-list'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { usePermissionSnapshot } from '@/features/auth'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  DeleteOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Descriptions, Space } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { pluginMutations } from '../mutations'
import type { InstalledPlugin } from '../plugin-model'
import { pluginQueries } from '../queries'
import { RegisteredExtensionList } from '../shared/extension-list'
import { compactPluginTags, formatPluginDateTime, pluginStatusBadge } from '../shared/formatters'
import { PluginManifestSections } from '../shared/manifest-sections'
import { PluginPageShell } from '../shared/page-shell'
import {
  canChangePluginLifecycle,
  canConfigurePlugins,
  canConfigurePluginSecrets,
  canRemovePlugins,
  canUpgradePlugins,
} from '../shared/permissions'
import { PluginNameCell } from '../shared/plugin-name-cell'
import { useInstalledPluginActions } from './actions'
import { PluginConfigModal } from './config-modal'

export function InstalledPluginDetailPage() {
  const { pluginId = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const snapshot = usePermissionSnapshot().data?.data
  const [configTarget, setConfigTarget] = useState<InstalledPlugin | null>(null)
  const actions = useInstalledPluginActions()
  const detailQuery = useQuery(pluginQueries.installedDetail(pluginId))
  const manifestQuery = useQuery(pluginQueries.manifest(pluginId))
  const extensionsQuery = useQuery(pluginQueries.extensions('runtime'))
  const configMutation = useOperationMutation(pluginMutations.configure(queryClient), {
    label: ['保存插件配置', 'Save plugin configuration'],
    target: (v) => v.pluginId,
    receipt: () => ({
      message: '插件配置记录已保存',
      evidence: '运行状态以插件实际观察记录为准。',
    }),
  })
  const plugin = readableQueryData(detailQuery)
  const manifest = queryAccessDenied(manifestQuery)
    ? undefined
    : (readableQueryData(manifestQuery) ?? plugin?.manifest)
  const extensions = readableQueryData(extensionsQuery)
  const blocked = actions.blocked || configMutation.blocked
  const notFound = isApiError(detailQuery.error) && detailQuery.error.status === 404
  useInlineApiError(notFound ? detailQuery.error : null)
  useEffect(() => {
    if (queryAccessDenied(detailQuery) || queryAccessDenied(configMutation)) setConfigTarget(null)
  }, [detailQuery.error, configMutation.error])
  const registeredExtensions = useMemo(
    () => (extensions ?? []).filter((record) => record.pluginId === plugin?.id),
    [extensions, plugin?.id],
  )

  if (detailQuery.isLoading) {
    return (
      <PluginPageShell>
        <ManagementState kind="loading" />
      </PluginPageShell>
    )
  }
  if (!plugin) {
    return (
      <PluginPageShell>
        {!queryAccessDenied(detailQuery) ? (
          <>
            {actions.feedback}
            {configMutation.feedback}
          </>
        ) : null}
        {notFound || !detailQuery.isError ? (
          <ManagementState kind="not-found" />
        ) : (
          <ReadFeedback query={detailQuery} label="插件安装记录" />
        )}
      </PluginPageShell>
    )
  }

  return (
    <PluginPageShell>
      <ReadFeedback query={detailQuery} label="插件安装记录" />
      {actions.feedback}
      {actions.dialog}
      {!configTarget ? configMutation.feedback : null}
      <Card size="small" className="soha-plugin-identity">
        <div className="soha-plugin-detail-heading">
          <PluginNameCell
            id={plugin.id}
            name={plugin.name}
            publisher={plugin.publisher}
            type={plugin.type}
            version={plugin.version}
            description={plugin.manifest.description}
          />
          <Space wrap className="soha-plugin-detail-actions">
            <Button onClick={() => navigate('/plugins/marketplace?installation=installed')}>
              返回插件市场
            </Button>
            <Button
              aria-label="刷新插件记录"
              icon={<ReloadOutlined />}
              loading={
                detailQuery.isFetching || manifestQuery.isFetching || extensionsQuery.isFetching
              }
              onClick={() =>
                void Promise.all([
                  detailQuery.refetch(),
                  manifestQuery.refetch(),
                  extensionsQuery.refetch(),
                ])
              }
            />
            {plugin.status === 'enabled' ? (
              <Button
                disabled={blocked || !canChangePluginLifecycle(snapshot)}
                icon={<PauseCircleOutlined />}
                onClick={() => actions.disable(plugin.id)}
              >
                停用
              </Button>
            ) : (
              <Button
                disabled={blocked || !canChangePluginLifecycle(snapshot)}
                icon={<PlayCircleOutlined />}
                onClick={() => actions.enable(plugin.id)}
              >
                启用
              </Button>
            )}
            <Button
              disabled={blocked || !canConfigurePlugins(snapshot)}
              icon={<SettingOutlined />}
              onClick={() => {
                if (!blocked) {
                  configMutation.reset()
                  setConfigTarget(plugin)
                }
              }}
            >
              配置
            </Button>
            <Button
              disabled={blocked || !canUpgradePlugins(snapshot)}
              icon={<ReloadOutlined />}
              onClick={() => actions.upgrade(plugin.id)}
            >
              升级
            </Button>
            <Button
              danger
              disabled={blocked || !canRemovePlugins(snapshot)}
              icon={<DeleteOutlined />}
              onClick={() => actions.confirmRemove(plugin)}
            >
              移除
            </Button>
          </Space>
        </div>
        <dl className="soha-plugin-facts">
          <div>
            <dt>运行状态</dt>
            <dd>{pluginStatusBadge(plugin.status)}</dd>
          </div>
          <div>
            <dt>校验状态</dt>
            <dd>
              <StatusTag value={plugin.checksumStatus} />
            </dd>
          </div>
          <div>
            <dt>安装时间</dt>
            <dd>{formatPluginDateTime(plugin.installedAt)}</dd>
          </div>
          <div>
            <dt>更新时间</dt>
            <dd>{formatPluginDateTime(plugin.updatedAt)}</dd>
          </div>
        </dl>
      </Card>
      <Card title={<h2 className="soha-plugin-section-title">安装记录</h2>} size="small">
        <Descriptions
          column={{ xs: 1, sm: 2, lg: 3 }}
          layout="vertical"
          size="small"
          items={[
            { key: 'source', label: '来源', children: plugin.source },
            { key: 'installedBy', label: '安装人', children: plugin.installedBy },
            {
              key: 'signature',
              label: '签名状态',
              children: plugin.signatureStatus || '-',
            },
            {
              key: 'enabledAt',
              label: '启用时间',
              children: formatPluginDateTime(plugin.enabledAt),
            },
            {
              key: 'disabledAt',
              label: '停用时间',
              children: formatPluginDateTime(plugin.disabledAt),
            },
            {
              key: 'secrets',
              label: '密钥引用',
              children: compactPluginTags(Object.keys(plugin.configuredSecretRefs ?? {}), 4),
            },
          ]}
        />
      </Card>
      <Card
        title={<h2 className="soha-plugin-section-title">已注册扩展</h2>}
        size="small"
        loading={extensionsQuery.isLoading}
      >
        <ReadFeedback query={extensionsQuery} label="已注册扩展" />
        {extensionsQuery.isError && !extensions ? null : (
          <RegisteredExtensionList records={registeredExtensions} />
        )}
      </Card>
      <ReadFeedback query={manifestQuery} label="插件 manifest" />
      {manifest ? <PluginManifestSections manifest={manifest} /> : null}
      <PluginConfigModal
        canEditSecrets={canConfigurePluginSecrets(snapshot)}
        open={Boolean(configTarget)}
        plugin={configTarget ?? undefined}
        blocked={blocked}
        saving={configMutation.isPending}
        feedback={configMutation.feedback}
        onCancel={() => {
          if (!blocked) setConfigTarget(null)
        }}
        onSubmit={(input) => configMutation.mutateAsync({ pluginId: configTarget!.id, input })}
      />
    </PluginPageShell>
  )
}
