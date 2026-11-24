import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { localeText, useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, EditOutlined, PlusOutlined, ThunderboltOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Flex, Popconfirm, Space, Typography, type TableColumnsType } from 'antd'
import { useState } from 'react'
import { systemIntegrationMutations } from './mutations'
import {
  PluginStorageConnectionModal,
  type PluginStorageFormValues,
} from './plugin-storage-connection-modal'
import {
  parsePluginStorageDriver,
  pluginStorageConfiguration,
  type PluginStorageDriver,
} from './plugin-storage-schema'
import { systemIntegrationQueries } from './queries'
import type { SystemIntegration } from './types'

const healthLabels: Record<SystemIntegration['healthStatus'], [string, string]> = {
  unknown: ['等待测试', 'Awaiting test'],
  healthy: ['连接正常', 'Connection healthy'],
  unhealthy: ['连接失败', 'Connection failed'],
  degraded: ['连接降级', 'Connection degraded'],
}

export function PluginStorageConnections({
  canView,
  canCreate,
  canUpdate,
  canDelete,
  canTest,
}: {
  canView: boolean
  canCreate: boolean
  canUpdate: boolean
  canDelete: boolean
  canTest: boolean
}) {
  const { localeCode } = useI18n()
  const label = (zh: string, en: string) => localeText(localeCode, zh, en)
  const client = useQueryClient()
  const driversQuery = useQuery(systemIntegrationQueries.storageDrivers(canView))
  const listQuery = useQuery(systemIntegrationQueries.list({ category: 'storage' }, canView))
  const descriptors = canView ? readableQueryData(driversQuery) : undefined
  const connections = canView ? readableQueryData(listQuery) : undefined
  const drivers = (descriptors ?? [])
    .map(parsePluginStorageDriver)
    .filter((driver) => driver !== undefined)
  const rows = (connections ?? []).filter(({ providerType }) => providerType !== 's3')
  const [editor, setEditor] = useState<'create' | SystemIntegration>()
  const [testingAfterSave, setTestingAfterSave] = useState(false)
  const createMutation = useOperationMutation(systemIntegrationMutations.create(client), {
    label: ['创建插件存储连接', 'Create plugin storage connection'],
    target: (values) => values.name,
  })
  const updateMutation = useOperationMutation(systemIntegrationMutations.update(client), {
    label: ['保存插件存储连接', 'Save plugin storage connection'],
    target: ({ id }) => id,
  })
  const deleteMutation = useOperationMutation(systemIntegrationMutations.remove(client), {
    label: ['删除插件存储连接', 'Delete plugin storage connection'],
    target: (id) => id,
  })
  const testMutation = useOperationMutation(systemIntegrationMutations.test(client), {
    label: ['测试插件存储连接', 'Test plugin storage connection'],
    target: (id) => id,
    readOnly: true,
    resultError: (result) =>
      result.status !== 'succeeded'
        ? new Error(
            label(
              '连接测试失败。请检查存储端点、网络访问及 Secret 授权。',
              'Connection test failed. Check the storage endpoint, network access, and secret authorization.',
            ),
          )
        : undefined,
    receipt: () => ({
      message: label('连接测试成功', 'Connection test succeeded'),
    }),
  })
  const denied = !canView || queryAccessDenied(listQuery) || queryAccessDenied(driversQuery)
  const blocked =
    denied ||
    createMutation.blocked ||
    updateMutation.blocked ||
    deleteMutation.blocked ||
    testMutation.blocked
  const canSave = editor === 'create' ? canCreate : canUpdate
  const editing = editor === 'create' ? undefined : editor
  const closeEditor = () => {
    setTestingAfterSave(false)
    setEditor(undefined)
  }

  const save = (driver: PluginStorageDriver, values: PluginStorageFormValues, test: boolean) => {
    if (!editor || blocked || !canSave) return
    const { name, enabled } = values
    const runTest = canTest && test && enabled
    setTestingAfterSave(runTest)
    const onSuccess = (saved: SystemIntegration) => {
      if (runTest) {
        testMutation.mutate(saved.id, { onSettled: closeEditor })
      } else {
        closeEditor()
      }
    }
    const onError = () => setTestingAfterSave(false)
    if (editing) {
      updateMutation.mutate(
        {
          id: editing.id,
          values: { name: name.trim(), enabled, expectedVersion: editing.version },
        },
        { onSuccess, onError },
      )
    } else {
      createMutation.mutate(
        {
          name: name.trim(),
          category: 'storage',
          providerType: driver.id,
          enabled,
          configuration: pluginStorageConfiguration(driver.fields, values.configuration ?? {}),
        },
        { onSuccess, onError },
      )
    }
  }

  const columns: TableColumnsType<SystemIntegration> = [
    { title: label('连接名称', 'Connection name'), dataIndex: 'name', key: 'name' },
    {
      title: label('存储驱动', 'Storage driver'),
      key: 'driver',
      render: (_, row) =>
        drivers.find(({ id }) => id === row.providerType)?.name ?? row.providerType,
    },
    {
      title: label('连接状态', 'Connection status'),
      key: 'status',
      render: (_, row) => {
        if (!row.enabled) return <StatusTag value="disabled" label={label('已停用', 'Disabled')} />
        if (descriptors && !drivers.some(({ id }) => id === row.providerType)) {
          return <StatusTag value="unknown" label={label('驱动不可用', 'Driver unavailable')} />
        }
        return (
          <StatusTag value={row.healthStatus} label={label(...healthLabels[row.healthStatus])} />
        )
      },
    },
    {
      title: label('操作', 'Actions'),
      key: 'actions',
      render: (_, row) => {
        const driverAvailable = drivers.some(({ id }) => id === row.providerType)
        return (
          <Space size={0}>
            {canTest && (
              <Button
                aria-label={label(`测试 ${row.name} 连接`, `Test ${row.name} connection`)}
                disabled={blocked || !row.enabled || !driverAvailable}
                icon={<ThunderboltOutlined />}
                loading={testMutation.isPending && testMutation.variables === row.id}
                size="small"
                type="text"
                onClick={() => testMutation.mutate(row.id)}
              />
            )}
            {canUpdate && (
              <Button
                aria-label={label(`编辑 ${row.name}`, `Edit ${row.name}`)}
                disabled={blocked || !driverAvailable}
                icon={<EditOutlined />}
                size="small"
                type="text"
                onClick={() => setEditor(row)}
              />
            )}
            {canDelete && (
              <Popconfirm
                title={label(`删除连接 ${row.name}？`, `Delete connection ${row.name}?`)}
                okText={label('删除', 'Delete')}
                cancelText={label('取消', 'Cancel')}
                okButtonProps={{ danger: true, disabled: blocked }}
                onConfirm={() => deleteMutation.mutate(row.id)}
              >
                <Button
                  aria-label={label(`删除 ${row.name}`, `Delete ${row.name}`)}
                  danger
                  disabled={blocked}
                  icon={<DeleteOutlined />}
                  size="small"
                  type="text"
                />
              </Popconfirm>
            )}
          </Space>
        )
      },
    },
  ]

  const feedback = (
    <>
      {createMutation.feedback}
      {updateMutation.feedback}
      {deleteMutation.feedback}
      {testMutation.feedback}
    </>
  )
  const showEditor = Boolean(editor && !denied)
  if (!canView) return null
  return (
    <section aria-label={label('插件存储连接', 'Plugin storage connections')}>
      <Flex vertical gap={8}>
        <Flex wrap="wrap" gap={8} justify="space-between" align="center">
          <Typography.Title level={5} style={{ margin: 0 }}>
            {label('插件存储连接', 'Plugin storage connections')}
          </Typography.Title>
          {canCreate && (
            <Button
              disabled={blocked || !connections || drivers.length === 0}
              icon={<PlusOutlined />}
              onClick={() => setEditor('create')}
            >
              {label('创建连接', 'Create connection')}
            </Button>
          )}
        </Flex>
        <Typography.Text type="secondary">
          {label(
            '供支持插件驱动的功能使用。软件包存储使用上方 S3 连接。',
            'For features that support plugin drivers. Software packages use the S3 connections above.',
          )}
        </Typography.Text>
        <ReadFeedback
          canRead={canView}
          query={driversQuery}
          label={label('存储驱动', 'Storage drivers')}
        />
        <ReadFeedback
          canRead={canView}
          query={listQuery}
          label={label('插件存储连接', 'Plugin storage connections')}
        />
        {!showEditor && feedback}
        {!denied && descriptors && drivers.length === 0 && (
          <Typography.Text type="secondary">
            {label('暂无就绪的插件存储驱动。', 'No plugin storage drivers are ready.')}
          </Typography.Text>
        )}
        {!denied && (connections || listQuery.isLoading) && (
          <AdminTable
            columnSettingPlacement="hidden"
            columns={canTest || canUpdate || canDelete ? columns : columns.slice(0, -1)}
            dataSource={rows}
            empty={label('尚未配置插件存储连接', 'No plugin storage connections configured')}
            enableDensity={false}
            loading={listQuery.isLoading || driversQuery.isLoading}
            pagination={false}
            rowKey="id"
            tableSize="small"
          />
        )}
      </Flex>
      {showEditor && (
        <PluginStorageConnectionModal
          drivers={drivers}
          editing={editing}
          blocked={blocked}
          canSave={canSave}
          canTest={canTest}
          busy={createMutation.isPending || updateMutation.isPending || testingAfterSave}
          saving={!testingAfterSave && (createMutation.isPending || updateMutation.isPending)}
          testing={testingAfterSave}
          feedback={feedback}
          onClose={closeEditor}
          onSave={save}
        />
      )}
    </section>
  )
}
