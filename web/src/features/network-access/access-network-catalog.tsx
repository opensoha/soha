import { AdminTable } from '@/components/admin-table'
import { ContextFeedback } from '@/components/context-feedback'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { ReadFeedback, requestErrorMessage } from '@/components/request-feedback'
import { BooleanTag } from '@/components/status-tag'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { DeleteOutlined, EditOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import type {
  NetworkAccessNetwork,
  NetworkAccessNetworkInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { App, Button, Typography, type TableColumnsType } from 'antd'
import { useState } from 'react'
import { AccessNetworkEditor, networkKindLabels } from './access-network-editor'
import { AccessNetworkPreview } from './access-network-preview'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'

export function AccessNetworkCatalog({
  siteId,
  canManage,
  canViewUsers,
  canViewDevices,
}: {
  siteId: string
  canManage: boolean
  canViewUsers: boolean
  canViewDevices: boolean
}) {
  const { localeCode } = useI18n()
  const { modal } = App.useApp()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const queryClient = useQueryClient()
  const catalog = useQuery(networkAccessQueries.accessNetworks(siteId))
  const [editing, setEditing] = useState<NetworkAccessNetwork | null>()
  const [preview, setPreview] = useState<{ draft?: NetworkAccessNetworkInput }>()
  const nas = useQuery(
    networkAccessQueries.nasBindings({ siteId, limit: 200 }, canManage && editing !== undefined),
  )
  const save = useOperationMutation(networkAccessMutations.accessNetworks.save(queryClient), {
    label: ['保存接入网络', 'Save access network'],
    target: (variables) => variables.input.name,
  })
  const remove = useOperationMutation(networkAccessMutations.accessNetworks.remove(queryClient), {
    label: ['删除接入网络', 'Delete access network'],
    target: (variables) => variables.name,
  })
  const denied = queryAccessDenied(catalog)
  const blocked = save.blocked || remove.blocked || denied
  const records = readableQueryData(catalog)
  const rows = [...(records ?? [])].sort(
    (left, right) =>
      left.configuration.priority - right.configuration.priority ||
      left.configuration.name.localeCompare(right.configuration.name),
  )
  const kindLabels = networkKindLabels(text)
  const previewLabel = text('预览授权', 'Preview authorization')
  const editLabel = text('编辑接入网络', 'Edit access network')
  const deleteLabel = text('删除接入网络', 'Delete access network')
  const refreshLabel = text('刷新接入网络', 'Refresh access networks')
  const nameCell = (value: string) => (
    <Typography.Text style={{ overflowWrap: 'anywhere' }}>{value}</Typography.Text>
  )

  function openEditor(item: NetworkAccessNetwork | null) {
    save.reset()
    setEditing(item)
  }

  const columns: TableColumnsType<NetworkAccessNetwork> = [
    {
      title: text('名称', 'Name'),
      dataIndex: ['configuration', 'name'],
      width: 180,
      render: (value: string, item) =>
        canManage && !blocked ? (
          <Button
            className="soha-table-primary-link"
            size="small"
            type="link"
            onClick={() => openEditor(item)}
          >
            {value}
          </Button>
        ) : (
          nameCell(value)
        ),
    },
    {
      title: text('SSID / 端口', 'SSID / Ports'),
      key: 'selectors',
      width: 180,
      render: (_, { configuration }) =>
        nameCell(
          configuration.accessMedium === 'wifi'
            ? (configuration.ssid ?? '—')
            : [
                ...(configuration.portIds ?? []),
                ...(configuration.portStart != null && configuration.portEnd != null
                  ? [`${configuration.portStart}–${configuration.portEnd}`]
                  : []),
              ].join(', ') || '—',
        ),
    },
    {
      title: text('网络类型', 'Network type'),
      dataIndex: ['configuration', 'kind'],
      width: 160,
      render: (kind: NetworkAccessNetworkInput['kind']) => kindLabels[kind],
    },
    {
      title: text('启用状态', 'Status'),
      dataIndex: ['configuration', 'enabled'],
      width: 90,
      render: (enabled: boolean) => (
        <BooleanTag
          value={enabled}
          trueLabel={text('已启用', 'Enabled')}
          falseLabel={text('已停用', 'Disabled')}
        />
      ),
    },
    {
      title: text('目标 VLAN', 'Target VLAN'),
      key: 'vlan',
      width: 110,
      render: (_, { configuration }) => {
        const vlans = new Set(
          configuration.rules
            .filter((rule) => rule.effect === 'allow')
            .map((rule) => rule.vlanId || configuration.vlanId)
            .filter(Boolean),
        )
        if (configuration.vlanId) vlans.add(configuration.vlanId)
        return Array.from(vlans).join(', ') || '—'
      },
    },
    {
      title: text('允许人群', 'Allowed subjects'),
      key: 'subjects',
      width: 160,
      render: (_, { configuration }) => {
        if (configuration.kind === 'open_guest_bootstrap') return text('开放引导', 'Open bootstrap')
        const allowRules = configuration.rules.filter((rule) => rule.effect === 'allow')
        const subjects = new Set(
          allowRules.flatMap(({ subjects }) => [
            ...subjects.users,
            ...subjects.teams,
            ...subjects.tags,
            ...(subjects.roles ?? []),
          ]),
        )
        if (!allowRules.length) return text('无允许规则', 'No allow rules')
        if (
          allowRules.some(
            ({ subjects }) =>
              !subjects.users.length &&
              !subjects.teams.length &&
              !subjects.tags.length &&
              !subjects.roles?.length,
          )
        )
          return text('含不限人群规则', 'Includes unrestricted subjects')
        return nameCell([...subjects].join(', '))
      },
    },
    {
      title: text('推荐优先级', 'Priority'),
      dataIndex: ['configuration', 'priority'],
      width: 120,
    },
    ...(canManage
      ? [
          {
            ...tableColumnPresets.action,
            title: text('操作', 'Actions'),
            key: 'actions',
            render: (_: unknown, item: NetworkAccessNetwork) => (
              <TableRowActionMenu
                targetLabel={item.configuration.name}
                items={[
                  {
                    key: 'edit',
                    icon: <EditOutlined />,
                    label: editLabel,
                    disabled: blocked,
                    onClick: () => openEditor(item),
                  },
                  { type: 'divider' },
                  {
                    key: 'delete',
                    icon: <DeleteOutlined />,
                    label: deleteLabel,
                    danger: true,
                    disabled: blocked,
                    onClick: () => {
                      modal.confirm({
                        title: text('删除此接入网络？', 'Delete this access network?'),
                        content: item.configuration.name,
                        okText: text('删除', 'Delete'),
                        cancelText: text('取消', 'Cancel'),
                        okButtonProps: { danger: true, disabled: blocked },
                        onOk: () =>
                          remove
                            .mutateAsync({ id: item.id, siteId, name: item.configuration.name })
                            .catch(() => undefined),
                      })
                    },
                  },
                ]}
              />
            ),
          },
        ]
      : []),
  ]

  return (
    <>
      <ReadFeedback query={catalog} label={text('接入网络', 'Access networks')} />
      {remove.feedback}
      {editing === undefined && save.feedback}
      {!denied && (
        <AdminTable
          rowKey="id"
          columns={columns}
          dataSource={rows}
          hasReadData={records !== undefined}
          error={catalog.error}
          onRetry={catalog.refetch}
          loading={catalog.isLoading}
          empty={text('此站点暂无接入网络。', 'This site has no access networks.')}
          scroll={{ x: 1090 }}
          columnSettingPlacement="header"
          columnSettingIconOnly
          headerExtra={
            <ManagementTableToolbar>
              {canManage && (
                <Button
                  type="primary"
                  size="small"
                  icon={<PlusOutlined />}
                  disabled={blocked}
                  onClick={() => openEditor(null)}
                >
                  {text('新增接入网络', 'Add access network')}
                </Button>
              )}
              <Button size="small" disabled={catalog.isLoading} onClick={() => setPreview({})}>
                {previewLabel}
              </Button>
              <ManagementIconButton
                aria-label={refreshLabel}
                icon={<ReloadOutlined />}
                tooltip={refreshLabel}
                loading={catalog.isFetching}
                onClick={() => void catalog.refetch()}
              />
            </ManagementTableToolbar>
          }
        />
      )}
      {canManage && !denied && editing !== undefined && (
        <AccessNetworkEditor
          siteId={siteId}
          value={editing?.configuration}
          bindings={readableQueryData(nas) ?? []}
          bindingsLoading={nas.isLoading}
          blocked={blocked}
          pending={save.isPending}
          feedback={
            <>
              {save.feedback}
              {save.error && (
                <ContextFeedback
                  type="error"
                  showIcon
                  title={
                    queryAccessDenied(save)
                      ? text(
                          '无权修改此站点的接入网络',
                          'You do not have permission to edit this site',
                        )
                      : text('保存接入网络失败', 'Could not save access network')
                  }
                  description={requestErrorMessage(save.error, localeCode)}
                />
              )}
              <ReadFeedback query={nas} label={text('站点 NAS', 'Site NAS')} />
            </>
          }
          onClose={() => setEditing(undefined)}
          onSave={(input) => {
            if (!blocked)
              save.mutate({ id: editing?.id, input }, { onSuccess: () => setEditing(undefined) })
          }}
          onPreview={(draft) => setPreview({ draft })}
        />
      )}
      {!denied && preview && (!preview.draft || canManage) && (
        <AccessNetworkPreview
          siteId={siteId}
          networks={rows}
          draft={preview.draft}
          canViewUsers={canViewUsers}
          canViewDevices={canViewDevices}
          onClose={() => setPreview(undefined)}
        />
      )}
    </>
  )
}
