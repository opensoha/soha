import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { ApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { CheckCircleOutlined, PoweroffOutlined, RadarChartOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Space, Typography } from 'antd'
import { settingsMutations } from '../mutations'
import { settingsQueries } from '../queries'
import { SettingsAdminTable, SettingsCard } from '../shared/components'
import type { WorkbenchRuntimeAsset } from '../types'

const kindLabels = { mcp: 'MCP', skill: 'Skill', hook: 'Hook' }
const scopeLabels = { chat: 'Chat', heya: 'Heya' }

export function RuntimeAssetsSettings({ owner, canManage }: { owner: string; canManage: boolean }) {
  const { modal } = App.useApp()
  const queryClient = useQueryClient()
  const catalog = useQuery(settingsQueries.ai.runtimeAssets(owner))
  const assets = readableQueryData(catalog)
  const setEnabled = useOperationMutation(
    settingsMutations.ai.setRuntimeAssetEnabled(queryClient, owner),
    {
      label: ['保存资产状态', 'Save asset availability'],
      target: (asset) => `${kindLabels[asset.kind]} · ${asset.id} @ ${asset.version}`,
      receipt: (asset) => ({
        message: asset.enabled ? '此资产版本已启用' : '此资产版本已停用',
        evidence: '后续运行按当前启用状态装配。已发布配置及历史运行仍保留原版本引用。',
      }),
    },
  )
  const probe = useOperationMutation(settingsMutations.ai.probeRuntimeMCP(), {
    label: ['探测 MCP', 'Probe MCP'],
    target: ({ id, version, scope }) => `${id} @ ${version} · ${scopeLabels[scope]}`,
    resultError: (result) =>
      result.status === 'cleanup_unknown'
        ? new ApiError(502, '探测进程清理未确认', { method: 'POST' })
        : undefined,
    receipt: (result) => ({
      message: result.status === 'ready' ? '探测就绪' : '探测不可用',
      evidence:
        result.status === 'ready' && result.capabilities?.length
          ? `发现能力 · ${result.capabilities.join('、')}`
          : undefined,
    }),
  })
  const actionsBlocked = !catalog.isSuccess || setEnabled.isPending || probe.isPending
  const enableBlocked = actionsBlocked || setEnabled.blocked || queryAccessDenied(setEnabled)
  const probeBlocked = actionsBlocked || probe.blocked || queryAccessDenied(probe)

  return (
    <SettingsCard title="Workbench 运行资产">
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Text type="secondary">
          目录由运营注册表提供。探测结果对应所选作用域，本轮会话是否加载以运行装配结果为准。
        </Typography.Text>
        {canManage && setEnabled.feedback}
        {canManage && probe.feedback}
        <SettingsAdminTable
          rowKey={(asset: WorkbenchRuntimeAsset) => `${asset.kind}:${asset.id}:${asset.version}`}
          dataSource={assets ?? []}
          pagination={false}
          enableColumnSelection={false}
          enableDensity={false}
          loading={catalog.isLoading}
          refreshing={catalog.isRefetching}
          error={catalog.error}
          hasReadData={assets !== undefined}
          onRefresh={
            owner && !queryAccessDenied(catalog) ? () => void catalog.refetch() : undefined
          }
          onRetry={() => catalog.refetch()}
          empty="运营注册表中暂无运行资产。"
          columns={[
            {
              title: '类型',
              key: 'kind',
              render: (_: unknown, asset: WorkbenchRuntimeAsset) => (
                <MetadataTag label={kindLabels[asset.kind]} />
              ),
            },
            { title: '资产 ID', dataIndex: 'id' },
            { title: '版本', dataIndex: 'version' },
            {
              title: '传输',
              dataIndex: 'transport',
              render: (transport?: string) => transport || '—',
            },
            {
              title: '执行位置',
              key: 'executionPosition',
              render: (_: unknown, asset: WorkbenchRuntimeAsset) =>
                asset.executionPosition === 'personal-docker' ? (
                  <Space orientation="vertical" size={0}>
                    <span>个人 Docker</span>
                    <Typography.Text type="secondary">
                      需绑定个人环境，由 Heya 运行验证
                    </Typography.Text>
                  </Space>
                ) : (
                  asset.executionPosition || '—'
                ),
            },
            {
              title: '状态',
              key: 'enabled',
              render: (_: unknown, asset: WorkbenchRuntimeAsset) => (
                <BooleanTag value={asset.enabled} trueLabel="已启用" falseLabel="已停用" />
              ),
            },
            ...(canManage
              ? [
                  {
                    ...tableColumnPresets.action,
                    title: '操作',
                    render: (_: unknown, asset: WorkbenchRuntimeAsset) => {
                      const { kind, id, version, enabled } = asset
                      const target = `${id} @ ${version}`
                      const probeScopes =
                        kind === 'mcp' && asset.executionPosition !== 'personal-docker'
                          ? asset.transport === 'stdio'
                            ? (['heya'] as const)
                            : (['chat', 'heya'] as const)
                          : []
                      return (
                        <TableRowActionMenu
                          ariaLabel={`更多操作 ${target}`}
                          items={[
                            ...probeScopes.map((scope) => ({
                              key: `probe:${scope}`,
                              label: `探测 ${scopeLabels[scope]}`,
                              icon: <RadarChartOutlined />,
                              disabled: probeBlocked,
                              onClick: () => probe.mutate({ id, version, scope }),
                            })),
                            ...(enabled
                              ? [
                                  ...(probeScopes.length ? [{ type: 'divider' as const }] : []),
                                  {
                                    key: 'disable',
                                    label: '停用',
                                    icon: <PoweroffOutlined />,
                                    danger: true,
                                    disabled: enableBlocked,
                                    onClick: () =>
                                      modal.confirm({
                                        title: `停用 ${target}？`,
                                        content: '停用后，仍引用此版本的配置无法启动新的运行。',
                                        okText: '停用',
                                        cancelText: '取消',
                                        okButtonProps: { danger: true },
                                        onOk: () =>
                                          setEnabled.mutateAsync({
                                            kind,
                                            id,
                                            version,
                                            enabled: false,
                                          }),
                                      }),
                                  },
                                ]
                              : [
                                  {
                                    key: 'enable',
                                    label: '启用',
                                    icon: <CheckCircleOutlined />,
                                    disabled: enableBlocked,
                                    onClick: () =>
                                      setEnabled.mutate({ kind, id, version, enabled: true }),
                                  },
                                ]),
                          ]}
                        />
                      )
                    },
                  },
                ]
              : []),
          ]}
        />
      </Space>
    </SettingsCard>
  )
}
