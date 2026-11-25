import { ContextFeedback } from '@/components/context-feedback'
import { ManagementState } from '@/components/management-list'
import { ReadFeedback, useInlineApiError } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { systemIntegrationQueries } from '@/features/settings'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  DeleteOutlined,
  PauseCircleOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RollbackOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Card, Descriptions, Space } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { pluginMutations } from '../mutations'
import type { InstalledPlugin } from '../plugin-model'
import { pluginSourceKindLabel, pluginSourceLabel } from '../plugin-model'
import { pluginQueries } from '../queries'
import { RegisteredExtensionList } from '../shared/extension-list'
import { compactPluginTags, formatPluginDateTime, pluginStatusBadge } from '../shared/formatters'
import { PluginManifestSections } from '../shared/manifest-sections'
import { PluginPageShell } from '../shared/page-shell'
import {
  canChangePluginLifecycle,
  canConfigurePlugins,
  canConfigurePluginSecrets,
  canInstallPlugin,
  canRemovePlugins,
  canUpgradePlugins,
} from '../shared/permissions'
import { PluginNameCell } from '../shared/plugin-name-cell'
import { pluginPublisherSignatureLabel } from '../shared/repository-preview'
import { useInstalledPluginActions } from './actions'
import { PluginConfigModal } from './config-modal'
import { useRepositoryUpgradeAction } from './repository-upgrade'

export function InstalledPluginDetailPage() {
  const { pluginId = '' } = useParams()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const snapshot = usePermissionSnapshot().data?.data
  const [configTarget, setConfigTarget] = useState<InstalledPlugin | null>(null)
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
  const canRollback = canUpgradePlugins(snapshot)
  const actions = useInstalledPluginActions(canRollback, plugin)
  const repositoryUpgrade = useRepositoryUpgradeAction()
  const hasReferences = (plugin?.referenceCount ?? 0) > 0
  const driverId = plugin?.manifest.components?.backend?.driverId
  const canReadReferences = Boolean(
    driverId && hasReferences && hasPermission(snapshot, 'settings.system-integrations.view'),
  )
  const referencesQuery = useQuery(
    systemIntegrationQueries.list(
      { category: 'storage', providerType: driverId },
      canReadReferences,
    ),
  )
  const references = canReadReferences ? readableQueryData(referencesQuery) : undefined
  const canOpenStorageManagement =
    canReadReferences &&
    !queryAccessDenied(referencesQuery) &&
    hasPermission(snapshot, 'software.package.view')
  const repositoryBound = Boolean(plugin?.sourceInfo?.repositoryUrl)
  const directRepository = plugin?.sourceInfo?.kind === 'github-repo'
  const canReadUpdates =
    (repositoryBound || directRepository) && hasPermission(snapshot, 'plugin.view')
  const updatesQuery = useQuery(pluginQueries.updates([pluginId], canReadUpdates))
  const update = canReadUpdates
    ? readableQueryData(updatesQuery)?.find(
        (item) => item.pluginId === plugin?.id && item.installedVersion === plugin?.version,
      )
    : undefined
  const manifest = queryAccessDenied(manifestQuery)
    ? undefined
    : (readableQueryData(manifestQuery) ?? plugin?.manifest)
  const extensions = readableQueryData(extensionsQuery)
  const blocked = actions.blocked || configMutation.blocked || repositoryUpgrade.blocked
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
      {canReadUpdates ? <ReadFeedback query={updatesQuery} label="插件更新" /> : null}
      {actions.feedback}
      {actions.dialog}
      {repositoryUpgrade.feedback}
      {repositoryUpgrade.dialog}
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
                detailQuery.isFetching ||
                manifestQuery.isFetching ||
                extensionsQuery.isFetching ||
                (canReadReferences && referencesQuery.isFetching) ||
                (canReadUpdates && updatesQuery.isFetching)
              }
              onClick={() =>
                void Promise.all([
                  detailQuery.refetch(),
                  manifestQuery.refetch(),
                  extensionsQuery.refetch(),
                  ...(canReadUpdates ? [updatesQuery.refetch()] : []),
                  ...(canReadReferences ? [referencesQuery.refetch()] : []),
                ])
              }
            />
            {plugin.status === 'enabled' ? (
              <Button
                disabled={blocked || hasReferences || !canChangePluginLifecycle(snapshot)}
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
            {directRepository || repositoryBound || update ? (
              <Button
                title={
                  canUpgradePlugins(snapshot) && canInstallPlugin(snapshot)
                    ? '从同一仓库来源预览并升级'
                    : '仓库升级预览需要 plugin.install，确认需要 plugin.upgrade'
                }
                disabled={
                  blocked ||
                  repositoryUpgrade.blocked ||
                  !canUpgradePlugins(snapshot) ||
                  !canInstallPlugin(snapshot) ||
                  (!directRepository && !update)
                }
                icon={<ReloadOutlined />}
                onClick={() => repositoryUpgrade.open(plugin, update)}
              >
                升级
              </Button>
            ) : (
              <Button
                disabled={blocked || !canUpgradePlugins(snapshot)}
                icon={<ReloadOutlined />}
                onClick={() => actions.upgrade(plugin.id)}
              >
                升级
              </Button>
            )}
            {plugin.rollbackVersion ? (
              <Button
                disabled={blocked || !canRollback}
                icon={<RollbackOutlined />}
                onClick={() => actions.confirmRollback(plugin)}
              >
                回退版本
              </Button>
            ) : null}
            <Button
              danger
              disabled={blocked || hasReferences || !canRemovePlugins(snapshot)}
              icon={<DeleteOutlined />}
              onClick={() => actions.confirmRemove(plugin)}
            >
              移除
            </Button>
          </Space>
        </div>
        {hasReferences ? (
          <ContextFeedback
            type="warning"
            showIcon
            title={`仍有 ${plugin.referenceCount} 个存储连接引用此驱动，停用和移除需要先解除引用。`}
            description={
              canReadReferences ? (
                <Space orientation="vertical" size={6}>
                  <ReadFeedback query={referencesQuery} label="引用此驱动的存储连接" />
                  {references?.length ? (
                    <ul>
                      {references.map((item) => (
                        <li key={item.id}>{item.name}</li>
                      ))}
                    </ul>
                  ) : null}
                  {canOpenStorageManagement ? (
                    <Link to="/internal-workbench/software">前往软件库管理对象存储</Link>
                  ) : null}
                </Space>
              ) : undefined
            }
          />
        ) : null}
        <dl className="soha-plugin-facts">
          <div>
            <dt>运行状态</dt>
            <dd>{pluginStatusBadge(plugin.status)}</dd>
          </div>
          {plugin.runtimeReady !== undefined ||
          plugin.manifest.runtime?.mode === 'executable-package-v1' ? (
            <div>
              <dt>组件运行就绪</dt>
              <dd>
                {plugin.runtimeReady === undefined
                  ? '状态未知'
                  : plugin.runtimeReady
                    ? '已就绪'
                    : '未就绪'}
              </dd>
            </div>
          ) : null}
          {plugin.referenceCount !== undefined ? (
            <div>
              <dt>存储连接引用</dt>
              <dd>{plugin.referenceCount}</dd>
            </div>
          ) : null}
          {plugin.rollbackVersion ? (
            <div>
              <dt>本地可回退版本</dt>
              <dd>{plugin.rollbackVersion}</dd>
            </div>
          ) : null}
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
            {
              key: 'source',
              label: '来源',
              children: pluginSourceLabel(plugin.sourceInfo, plugin.source),
            },
            ...(plugin.sourceInfo
              ? [
                  {
                    key: 'sourceKind',
                    label: '来源类型',
                    children: pluginSourceKindLabel(plugin.sourceInfo.kind),
                  },
                  {
                    key: 'canonicalSource',
                    label: '规范化来源',
                    span: { xs: 1, sm: 2, lg: 3 },
                    children: (
                      <span className="soha-plugin-digest">
                        {plugin.sourceInfo.canonicalSource}
                      </span>
                    ),
                  },
                  ...(plugin.sourceInfo.pinnedRef
                    ? [
                        {
                          key: 'pinnedRef',
                          label: '请求修订',
                          children: plugin.sourceInfo.pinnedRef,
                        },
                      ]
                    : []),
                  ...(plugin.sourceInfo.commitSha
                    ? [
                        {
                          key: 'commitSha',
                          label: '固定提交',
                          span: { xs: 1, sm: 2, lg: 3 },
                          children: (
                            <span className="soha-plugin-digest">
                              {plugin.sourceInfo.commitSha}
                            </span>
                          ),
                        },
                      ]
                    : []),
                  ...(plugin.sourceInfo.rawManifestChecksum
                    ? [
                        {
                          key: 'rawManifestChecksum',
                          label: '原始摘要',
                          span: { xs: 1, sm: 2, lg: 3 },
                          children: (
                            <span className="soha-plugin-digest">
                              {plugin.sourceInfo.rawManifestChecksum}
                            </span>
                          ),
                        },
                      ]
                    : []),
                  {
                    key: 'official',
                    label: '官方收录',
                    children: <BooleanTag value={plugin.sourceInfo.official} />,
                  },
                ]
              : []),
            { key: 'installedBy', label: '安装人', children: plugin.installedBy },
            {
              key: 'signature',
              label: '签名状态',
              children: pluginPublisherSignatureLabel(
                plugin.signatureStatus,
                plugin.sourceInfo?.publisherKeyFingerprint,
              ),
            },
            ...(plugin.sourceInfo?.publisherKeyFingerprint
              ? [
                  {
                    key: 'publisherFingerprint',
                    label: 'Ed25519 公钥指纹',
                    span: { xs: 1, sm: 2, lg: 3 },
                    children: (
                      <span className="soha-plugin-digest">
                        {plugin.sourceInfo.publisherKeyFingerprint}
                      </span>
                    ),
                  },
                ]
              : []),
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
