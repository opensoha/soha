import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeOperationFeedback, ComputeMutationFeedback } from '@/features/compute'
import { useMemo, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { hasAllowedAction } from '@/features/auth'
import { tableColumnPresets } from '@/utils/table-columns'
import { scrollableModalBodyStyle } from '@/components/modal-styles'
import { BooleanTag } from '@/components/status-tag'
import { ManagementDataPage } from '@/components/management-data-page'
import { localeText, useI18n } from '@/i18n'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  useManagementTextFilter,
} from '@/components/management-list'
import {
  virtualizationMutations,
  withVirtualizationMutationSuccess,
} from '@/features/virtualization/mutations'
import { virtualizationQueries } from '@/features/virtualization/queries'
import { useVirtualizationPermissions } from '@/features/virtualization/shared/use-virtualization-permissions'
import { VirtualizationAdminTable } from '@/features/virtualization/shared/ui'
import { TableCellText } from '@/components/table-cell-content'
import {
  ENABLED_FILTER_OPTIONS,
  localTableSummary,
} from '@/features/virtualization/virtualization-model'
import type { EnabledFilter } from '@/features/virtualization/virtualization-model'
import '@/features/virtualization/virtualization-workbench.css'
import type {
  VirtualizationFlavor,
  VirtualizationFlavorInput,
} from '@/features/virtualization/virtualization-types'

const tableEllipsis = { showTitle: false } as const

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

export function VirtualizationFlavorsPage() {
  const [editing, setEditing] = useState<VirtualizationFlavor | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [flavorFilters, setFlavorFilters] = useState<{ enabled?: EnabledFilter; search?: string }>({
    enabled: 'all',
  })
  const [filterForm] = Form.useForm<{ enabled?: EnabledFilter; search?: string }>()
  const [form] = Form.useForm<VirtualizationFlavorInput>()
  const { virtualizationModuleEnabled, canCreateFlavors, canUpdateFlavors, canDeleteFlavors } =
    useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const { localeCode } = useI18n()
  const flavorsQuery = useQuery(virtualizationQueries.flavors(virtualizationModuleEnabled))
  const flavors = readableQueryData(flavorsQuery) ?? []
  const textFilteredFlavors = useManagementTextFilter(
    flavors,
    flavorFilters.search ?? '',
    (record) => [record.name, record.description],
  )
  const flavorRows = useMemo(() => {
    return textFilteredFlavors.filter((record) => {
      return flavorFilters.enabled === 'disabled'
        ? record.enabled === false
        : flavorFilters.enabled === 'enabled'
          ? record.enabled !== false
          : true
    })
  }, [flavorFilters.enabled, textFilteredFlavors])
  const afterSave = () => {
    message.success(localeText(localeCode, '规格已保存', 'Flavor saved'))
    setDrawerOpen(false)
    setEditing(null)
    form.resetFields()
  }
  const createMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.createFlavor(queryClient), afterSave),
  )
  const updateMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.updateFlavor(queryClient), afterSave),
  )
  const deleteMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.deleteFlavor(queryClient), () =>
      message.success(localeText(localeCode, '规格已删除', 'Flavor deleted')),
    ),
  )
  const savePending = createMutation.isPending || updateMutation.isPending
  function openEditor(record?: VirtualizationFlavor) {
    if (savePending) return
    if (!writeOutcomeUnknown(createMutation.error)) createMutation.reset()
    if (!writeOutcomeUnknown(updateMutation.error)) updateMutation.reset()
    setEditing(record ?? null)
    form.resetFields()
    form.setFieldsValue(record ?? { enabled: true })
    setDrawerOpen(true)
  }
  const columns: ColumnsType<VirtualizationFlavor> = [
    {
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      fixed: 'left',
      render: tableTooltipText,
      ellipsis: tableEllipsis,
      width: 180,
    },
    { title: 'CPU', dataIndex: 'cpu', width: 90 },
    { title: localeText(localeCode, '内存 MiB', 'Memory MiB'), dataIndex: 'memoryMiB', width: 120 },
    { title: localeText(localeCode, '磁盘 GiB', 'Disk GiB'), dataIndex: 'diskGiB', width: 120 },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'enabled',
      render: (value) => (
        <BooleanTag
          value={value !== false}
          trueLabel={localeText(localeCode, '启用', 'Enabled')}
          falseLabel={localeText(localeCode, '禁用', 'Disabled')}
        />
      ),
      width: 100,
    },
    {
      title: localeText(localeCode, '描述', 'Description'),
      dataIndex: 'description',
      render: (value) => tableTooltipText(value || '-'),
      ellipsis: tableEllipsis,
      width: 320,
    },
    {
      ...tableColumnPresets.action,
      title: localeText(localeCode, '操作', 'Actions'),
      render: (_value, record) => {
        const canUpdate = canUpdateFlavors && hasAllowedAction(record.allowedActions, 'update')
        const canDelete = canDeleteFlavors && hasAllowedAction(record.allowedActions, 'delete')
        if (!canUpdate && !canDelete) return null
        return (
          <Space className="soha-row-action-icons">
            {canUpdate ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '编辑规格', 'Edit flavor')}
                size="small"
                tooltip={localeText(localeCode, '编辑', 'Edit')}
                icon={<EditOutlined />}
                onClick={() => openEditor(record)}
              />
            ) : null}
            {canDelete ? (
              <Popconfirm
                title={localeText(localeCode, '确认删除规格？', 'Delete this flavor?')}
                onConfirm={() => deleteMutation.mutate(record.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '删除规格', 'Delete flavor')}
                  size="small"
                  tooltip={localeText(localeCode, '删除', 'Delete')}
                  danger
                  icon={<DeleteOutlined />}
                />
              </Popconfirm>
            ) : null}
          </Space>
        )
      },
    },
  ]
  return (
    <ManagementDataPage
      className="soha-virtualization-page"
      beforeQuery={
        <>
          <ReadFeedback query={flavorsQuery} label={localeText(localeCode, '规格', 'Flavors')} />
          <ComputeMutationFeedback
            operations={[
              {
                label: localeText(localeCode, '新增规格', 'Create flavor'),
                mutation: createMutation,
                target: createMutation.variables?.name,
                receipt: {
                  message: localeText(localeCode, '规格记录已保存', 'Flavor record saved'),
                },
              },
              {
                label: localeText(localeCode, '更新规格', 'Update flavor'),
                mutation: updateMutation,
                target: updateMutation.variables?.id,
                receipt: {
                  message: localeText(localeCode, '规格记录已保存', 'Flavor record saved'),
                },
              },
              {
                label: localeText(localeCode, '删除规格', 'Delete flavor'),
                mutation: deleteMutation,
                target: deleteMutation.variables,
                receipt: {
                  message: localeText(localeCode, '规格记录已删除', 'Flavor record deleted'),
                },
              },
            ]}
          />
        </>
      }
      query={{
        actions: (
          <ManagementQueryActions
            loading={flavorsQuery.isFetching}
            onReset={() => {
              filterForm.resetFields()
              setFlavorFilters({ enabled: 'all' })
            }}
          />
        ),
        children: (
          <>
            <ManagementKeywordField
              label={localeText(localeCode, '关键字', 'Keyword')}
              placeholder={localeText(
                localeCode,
                '搜索规格名称或描述',
                'Search flavor name or description',
              )}
            />
            <ManagementQueryField
              minWidth={180}
              name="enabled"
              label={localeText(localeCode, '启用状态', 'Enabled')}
              width={180}
            >
              <Select
                options={ENABLED_FILTER_OPTIONS.map((option) => ({
                  ...option,
                  label:
                    option.value === 'all'
                      ? localeText(localeCode, '全部', 'All')
                      : option.value === 'enabled'
                        ? localeText(localeCode, '仅启用', 'Enabled only')
                        : localeText(localeCode, '仅禁用', 'Disabled only'),
                }))}
              />
            </ManagementQueryField>
          </>
        ),
        collapsible: true,
        form: filterForm,
        initialValues: { enabled: 'all' },
        onFinish: (values) =>
          setFlavorFilters({ enabled: values.enabled ?? 'all', search: values.search }),
        wrapperClassName: 'soha-vrt-query',
      }}
      tableNode={
        <VirtualizationAdminTable
          rowKey="id"
          empty={
            flavorsQuery.isError
              ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
              : undefined
          }
          actions={
            canCreateFlavors ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>
                {localeText(localeCode, '新增规格', 'Add flavor')}
              </Button>
            ) : null
          }
          refreshing={flavorsQuery.isFetching}
          onRefresh={() => void flavorsQuery.refetch()}
          loading={flavorsQuery.isLoading}
          dataSource={flavorRows}
          columns={columns}
          paginationSummary={localeText(
            localeCode,
            localTableSummary(flavorRows.length, flavors.length),
            `${flavorRows.length} of ${flavors.length}`,
          )}
          scroll={{ x: 1070 }}
        />
      }
      afterTable={
        <Modal
          title={
            editing
              ? localeText(localeCode, '编辑规格', 'Edit flavor')
              : localeText(localeCode, '新增规格', 'Add flavor')
          }
          open={drawerOpen}
          onCancel={() => {
            if (!savePending) setDrawerOpen(false)
          }}
          onOk={() => form.submit()}
          confirmLoading={savePending}
          okText={localeText(localeCode, '保存', 'Save')}
          cancelText={localeText(localeCode, '取消', 'Cancel')}
          width={720}
          destroyOnHidden
          mask={{ closable: false }}
          style={{ top: 32 }}
          styles={{ body: { ...scrollableModalBodyStyle, maxHeight: 'calc(100dvh - 180px)' } }}
        >
          <ComputeOperationFeedback
            label={localeText(localeCode, '保存规格', 'Save record')}
            pending={createMutation.isPending || updateMutation.isPending}
            error={updateMutation.error || createMutation.error}
            target={
              updateMutation.error ? updateMutation.variables?.id : createMutation.variables?.name
            }
          />
          <Form<VirtualizationFlavorInput>
            form={form}
            layout="vertical"
            preserve={false}
            onFinish={(values) =>
              editing
                ? updateMutation.mutate({ id: editing.id, payload: values })
                : createMutation.mutate(values)
            }
            initialValues={editing ?? { cpu: 2, memoryMiB: 4096, diskGiB: 40, enabled: true }}
          >
            <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
              <Form.Item
                name="name"
                label={localeText(localeCode, '名称', 'Name')}
                rules={[{ required: true }]}
              >
                <Input />
              </Form.Item>
              <Form.Item
                name="enabled"
                label={localeText(localeCode, '启用', 'Enabled')}
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
            </div>

            <div className="soha-vrt-form-grid soha-vrt-form-grid--3">
              <Form.Item name="cpu" label="CPU" rules={[{ required: true }]}>
                <InputNumber min={1} className="soha-vrt-fill" />
              </Form.Item>
              <Form.Item
                name="memoryMiB"
                label={localeText(localeCode, '内存 MiB', 'Memory MiB')}
                rules={[{ required: true }]}
              >
                <InputNumber min={128} className="soha-vrt-fill" />
              </Form.Item>
              <Form.Item
                name="diskGiB"
                label={localeText(localeCode, '磁盘 GiB', 'Disk GiB')}
                rules={[{ required: true }]}
              >
                <InputNumber min={1} className="soha-vrt-fill" />
              </Form.Item>
            </div>
            <Form.Item name="description" label={localeText(localeCode, '描述', 'Description')}>
              <Input.TextArea rows={3} />
            </Form.Item>
          </Form>
        </Modal>
      }
    />
  )
}
