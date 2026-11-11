import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { ComputeOperationFeedback, ComputeMutationFeedback } from '@/features/compute'
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  App,
  Button,
  Checkbox,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Typography,
} from 'antd'
import type { ColumnsType } from 'antd/es/table'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import { hasAllowedAction } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { tableColumnPresets } from '@/utils/table-columns'
import { scrollableModalBodyStyle } from '@/components/modal-styles'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { ManagementDataPage } from '@/components/management-data-page'
import { localeText, useI18n } from '@/i18n'
import {
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
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
  VIRTUALIZATION_PROVIDER_OPTIONS,
  buildImagePayload,
  normalizePage,
  providerLabel,
  virtualizationPageSummary,
} from '@/features/virtualization/virtualization-model'
import '@/features/virtualization/virtualization-workbench.css'
import type {
  VirtualizationImageCategory,
  VirtualizationImage,
  VirtualizationImageInput,
  VirtualizationListParams,
  VirtualizationPage,
} from '@/features/virtualization/virtualization-types'

const { Text } = Typography

const tableEllipsis = { showTitle: false } as const

function statusTag(value?: string) {
  if (!value) return <Text type="secondary">-</Text>
  return <StatusTag value={value} />
}

function tableTooltipText(value: unknown) {
  const text = String(value ?? '').trim() || '-'
  return <TableCellText className="soha-vrt-table-tooltip-text" value={text} />
}

function imageResourceType(record: VirtualizationImage, localeCode: 'zh_CN' | 'en_US') {
  const sourceKind = String(record.assetKind || record.sourceKind || record.source || '')
    .trim()
    .toLowerCase()
  const contentType = String(record.config?.contentType || '')
    .trim()
    .toLowerCase()
  if (sourceKind === 'template') return localeText(localeCode, 'VM 模板', 'VM template')
  if (sourceKind === 'cloud_image') return 'Cloud image'
  if (sourceKind === 'lxc_template' || contentType === 'vztmpl')
    return localeText(localeCode, 'CT 模板', 'CT template')
  if (sourceKind === 'iso' || contentType === 'iso')
    return localeText(localeCode, 'ISO 镜像', 'ISO image')
  if (contentType === 'images') return localeText(localeCode, 'VM 磁盘', 'VM disk')
  if (contentType === 'rootdir') return localeText(localeCode, 'CT 根卷', 'CT root volume')
  return (
    {
      datasource: 'DataSource',
      pvc: localeText(localeCode, 'PVC 镜像源', 'PVC image source'),
      storage: localeText(localeCode, '存储池', 'Storage pool'),
      storage_content: localeText(localeCode, '存储内容', 'Storage content'),
      image: localeText(localeCode, '磁盘/卷', 'Disk / Volume'),
      images: localeText(localeCode, 'VM 磁盘', 'VM disk'),
      rootdir: localeText(localeCode, 'CT 根卷', 'CT root volume'),
      datavolume: 'DataVolume',
      persistentvolumeclaim: localeText(localeCode, 'PVC 卷', 'PVC volume'),
    }[sourceKind] ||
    sourceKind ||
    '-'
  )
}

function pageTablePagination<T>(
  page: VirtualizationPage<T>,
  setFilters: React.Dispatch<React.SetStateAction<VirtualizationListParams>>,
) {
  return {
    current: page.page,
    pageSize: page.pageSize,
    total: page.total,
    onPageChange: (pageNumber: number) =>
      setFilters((current) => ({ ...current, page: pageNumber })),
    onPageSizeChange: (pageSize: number) =>
      setFilters((current) => ({ ...current, page: 1, pageSize })),
  }
}

interface VirtualizationImagesPageProps {
  category?: VirtualizationImageCategory
}

function imageFormValues(record?: VirtualizationImage | null) {
  return record
    ? {
        name: record.name,
        provider: record.provider ?? 'kubevirt',
        connectionId: record.connectionId,
        namespace: record.namespace,
        sourceKind: record.sourceKind ?? record.source,
        sourceRef: record.sourceRef,
        source: record.source,
        osType: record.osType,
        sizeGiB: record.sizeGiB,
        config: record.config,
        description: record.description,
      }
    : { provider: 'kubevirt', sourceKind: 'datasource' }
}

export function VirtualizationImagesPage({
  category: imageCategory = 'catalog',
}: VirtualizationImagesPageProps = {}) {
  const { localeCode } = useI18n()
  const [editing, setEditing] = useState<VirtualizationImage | null>(null)
  const [drawerOpen, setDrawerOpen] = useState(false)
  const [filters, setFilters] = useState<VirtualizationListParams>({ page: 1, pageSize: 15 })
  const [filterForm] = Form.useForm<VirtualizationListParams>()
  const [form] = Form.useForm<VirtualizationImageInput>()
  const { virtualizationModuleEnabled, canCreateImages, canUpdateImages, canDeleteImages } =
    useVirtualizationPermissions()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const imageProvider = Form.useWatch('provider', form) ?? 'kubevirt'
  const imagesQuery = useQuery(
    virtualizationQueries.images(
      { ...filters, category: imageCategory },
      virtualizationModuleEnabled,
    ),
  )
  const clustersQuery = useQuery(virtualizationQueries.clusters(virtualizationModuleEnabled))
  const imagesPage = normalizePage(
    readableQueryData(imagesQuery),
    filters.page ?? 1,
    filters.pageSize ?? 15,
  )
  const clusters = readableQueryData(clustersQuery) ?? []
  const afterSave = () => {
    message.success(localeText(localeCode, '镜像入口已保存', 'Image entry saved'))
    setDrawerOpen(false)
    setEditing(null)
    form.resetFields()
  }
  const createMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.createImage(queryClient), afterSave),
  )
  const updateMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.updateImage(queryClient), afterSave),
  )
  const deleteMutation = useMutation(
    withVirtualizationMutationSuccess(virtualizationMutations.deleteImage(queryClient), () =>
      message.success(localeText(localeCode, '镜像入口已删除', 'Image entry deleted')),
    ),
  )
  const savePending = createMutation.isPending || updateMutation.isPending
  function openImageEditor(record?: VirtualizationImage) {
    if (savePending) return
    if (!writeOutcomeUnknown(createMutation.error)) createMutation.reset()
    if (!writeOutcomeUnknown(updateMutation.error)) updateMutation.reset()
    setEditing(record ?? null)
    form.resetFields()
    form.setFieldsValue(imageFormValues(record))
    setDrawerOpen(true)
  }
  const columns: ColumnsType<VirtualizationImage> = [
    {
      title: localeText(localeCode, '名称', 'Name'),
      dataIndex: 'name',
      fixed: 'left',
      render: tableTooltipText,
      ellipsis: tableEllipsis,
      width: 180,
    },
    {
      title: localeText(localeCode, '提供方', 'Provider'),
      dataIndex: 'provider',
      render: (value: string) => (
        <MetadataTag label={providerLabel(value)} tone={value === 'pve' ? 'gold' : 'blue'} />
      ),
      width: 120,
    },
    {
      title: localeText(localeCode, '连接', 'Connection'),
      dataIndex: 'connectionName',
      render: (value, record) => tableTooltipText(value || record.connectionId || '-'),
      ellipsis: tableEllipsis,
      width: 200,
    },
    {
      title: localeText(localeCode, '类型', 'Type'),
      render: (_value, record) => (
        <MetadataTag
          label={imageResourceType(record, localeCode)}
          tone={imageCategory === 'storage' ? 'gold' : 'blue'}
        />
      ),
      width: 160,
    },
    {
      title: localeText(localeCode, '引用', 'Reference'),
      dataIndex: 'sourceRef',
      render: (value) => tableTooltipText(value || '-'),
      ellipsis: tableEllipsis,
      width: 280,
    },
    {
      title: localeText(localeCode, '可用性', 'Availability'),
      render: (_value, record) => (
        <BooleanTag
          value={record.ready !== false}
          trueLabel={localeText(localeCode, '可用', 'Available')}
          falseLabel={localeText(localeCode, '不可用', 'Unavailable')}
          falseColor="error"
        />
      ),
      width: 110,
    },
    {
      title: localeText(localeCode, '大小', 'Size'),
      dataIndex: 'sizeGiB',
      render: (value) => (value ? `${value} GiB` : '-'),
      width: 100,
    },
    {
      title: localeText(localeCode, '状态', 'Status'),
      dataIndex: 'status',
      render: statusTag,
      width: 120,
    },
    {
      ...tableColumnPresets.datetime,
      title: localeText(localeCode, '更新时间', 'Updated at'),
      dataIndex: 'updatedAt',
      render: formatDateTime,
    },
    {
      ...tableColumnPresets.action,
      title: localeText(localeCode, '操作', 'Actions'),
      render: (_value, record) => {
        const canUpdate = canUpdateImages && hasAllowedAction(record.allowedActions, 'update')
        const canDelete = canDeleteImages && hasAllowedAction(record.allowedActions, 'delete')
        if (!canUpdate && !canDelete) return null
        return (
          <Space className="soha-row-action-icons">
            {canUpdate ? (
              <ManagementIconButton
                aria-label={localeText(localeCode, '编辑镜像', 'Edit image')}
                size="small"
                tooltip={localeText(localeCode, '编辑', 'Edit')}
                icon={<EditOutlined />}
                onClick={() => openImageEditor(record)}
              />
            ) : null}
            {canDelete ? (
              <Popconfirm
                title={localeText(localeCode, '确认删除镜像入口？', 'Delete this image entry?')}
                onConfirm={() => deleteMutation.mutate(record.id)}
              >
                <ManagementIconButton
                  aria-label={localeText(localeCode, '删除镜像', 'Delete image')}
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
  if (imageCategory === 'storage') columns.splice(-1)
  return (
    <ManagementDataPage
      className="soha-virtualization-page"
      beforeQuery={
        <>
          <ReadFeedback
            query={imagesQuery}
            label={localeText(
              localeCode,
              imageCategory === 'storage' ? '存储与卷' : '镜像与模板',
              imageCategory === 'storage' ? 'Storage' : 'Images',
            )}
          />
          <ComputeMutationFeedback
            operations={[
              {
                label: localeText(localeCode, '新增资源入口', 'Create resource entry'),
                mutation: createMutation,
                target: createMutation.variables?.name,
                receipt: {
                  message: localeText(localeCode, '资源入口记录已保存', 'Resource entry saved'),
                },
              },
              {
                label: localeText(localeCode, '更新资源入口', 'Update resource entry'),
                mutation: updateMutation,
                target: updateMutation.variables?.id,
                receipt: {
                  message: localeText(localeCode, '资源入口记录已保存', 'Resource entry saved'),
                },
              },
              {
                label: localeText(localeCode, '删除资源入口', 'Delete resource entry'),
                mutation: deleteMutation,
                target: deleteMutation.variables,
                receipt: {
                  message: localeText(localeCode, '资源入口记录已删除', 'Resource entry deleted'),
                },
              },
            ]}
          />
        </>
      }
      query={{
        actions: (
          <ManagementQueryActions
            loading={imagesQuery.isFetching}
            onReset={() => {
              filterForm.resetFields()
              setFilters((current) => ({ page: 1, pageSize: current.pageSize ?? 15 }))
            }}
          />
        ),
        children: (
          <>
            <ManagementKeywordField
              label={localeText(localeCode, '关键字', 'Keyword')}
              placeholder={
                imageCategory === 'catalog'
                  ? localeText(
                      localeCode,
                      '搜索镜像、模板或 ISO',
                      'Search images, templates, or ISO',
                    )
                  : localeText(
                      localeCode,
                      '搜索存储、磁盘或卷',
                      'Search storage, disks, or volumes',
                    )
              }
            />
            <ManagementQueryField
              minWidth={180}
              name="connectionId"
              label={localeText(localeCode, '连接', 'Connection')}
              width={180}
            >
              <Select
                allowClear
                showSearch={{ optionFilterProp: 'label' }}
                placeholder={localeText(localeCode, '全部连接', 'All connections')}
                options={clusters.map((item) => ({ value: item.id, label: item.name }))}
              />
            </ManagementQueryField>
            <ManagementQueryField
              minWidth={160}
              name="provider"
              label={localeText(localeCode, '提供方', 'Provider')}
              width={160}
            >
              <Select
                allowClear
                placeholder={localeText(localeCode, '全部提供方', 'All providers')}
                options={VIRTUALIZATION_PROVIDER_OPTIONS}
              />
            </ManagementQueryField>
          </>
        ),
        collapsible: true,
        form: filterForm,
        onFinish: (values) => setFilters((current) => ({ ...current, ...values, page: 1 })),
        wrapperClassName: 'soha-vrt-query',
      }}
      tableNode={
        <VirtualizationAdminTable
          rowKey="id"
          empty={
            imagesQuery.isError
              ? localeText(localeCode, '数据读取失败，请先重试', 'Read failed; retry first')
              : undefined
          }
          actions={
            canCreateImages && imageCategory === 'catalog' ? (
              <Button type="primary" icon={<PlusOutlined />} onClick={() => openImageEditor()}>
                {localeText(localeCode, '新增镜像入口', 'Add image entry')}
              </Button>
            ) : null
          }
          refreshing={imagesQuery.isFetching}
          onRefresh={() => void imagesQuery.refetch()}
          loading={imagesQuery.isLoading}
          dataSource={imagesPage.items}
          columns={columns}
          scroll={{ x: 1546 }}
          pagination={pageTablePagination(imagesPage, setFilters)}
          paginationSummary={(total, range) =>
            localeText(
              localeCode,
              virtualizationPageSummary(total, range),
              total > 0 ? `${range[0]}-${range[1]} of ${total}` : '0 of 0',
            )
          }
          expandable={{
            expandedRowRender: (record: VirtualizationImage) => (
              <Descriptions
                size="small"
                bordered
                column={{ xs: 1, sm: 1, md: 2, lg: 2, xl: 2, xxl: 2 }}
              >
                <Descriptions.Item label={localeText(localeCode, '命名空间', 'Namespace')}>
                  {record.namespace || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '系统', 'Operating system')}>
                  {record.osType || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '节点', 'Node')}>
                  {record.node || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '存储', 'Storage')}>
                  {record.storage || '-'}
                </Descriptions.Item>
                <Descriptions.Item label="StorageClass">
                  {record.storageClass || '-'}
                </Descriptions.Item>
                <Descriptions.Item label={localeText(localeCode, '来源类型', 'Source type')}>
                  {record.sourceKind || record.source || '-'}
                </Descriptions.Item>
                <Descriptions.Item
                  label={localeText(localeCode, '来源引用', 'Source reference')}
                  span="filled"
                >
                  {record.sourceRef || '-'}
                </Descriptions.Item>
                <Descriptions.Item
                  label={localeText(localeCode, '描述', 'Description')}
                  span="filled"
                >
                  {record.description || '-'}
                </Descriptions.Item>
              </Descriptions>
            ),
          }}
        />
      }
      afterTable={
        <Modal
          title={
            editing
              ? localeText(localeCode, '编辑镜像入口', 'Edit image entry')
              : localeText(localeCode, '新增镜像入口', 'Add image entry')
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
            label={localeText(localeCode, '保存资源入口', 'Save record')}
            pending={createMutation.isPending || updateMutation.isPending}
            error={updateMutation.error || createMutation.error}
            target={
              updateMutation.error ? updateMutation.variables?.id : createMutation.variables?.name
            }
          />
          <ReadFeedback
            query={clustersQuery}
            label={localeText(localeCode, '连接选项', 'Connection options')}
          />
          <Form<VirtualizationImageInput>
            form={form}
            layout="vertical"
            preserve={false}
            onFinish={(values) => {
              const payload = buildImagePayload(values)
              if (editing) updateMutation.mutate({ id: editing.id, payload })
              else createMutation.mutate(payload)
            }}
            initialValues={imageFormValues(editing)}
          >
            <Form.Item
              name="name"
              label={localeText(localeCode, '名称', 'Name')}
              rules={[{ required: true }]}
            >
              <Input />
            </Form.Item>
            <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
              <Form.Item
                name="provider"
                label={localeText(localeCode, '提供方', 'Provider')}
                rules={[{ required: true }]}
              >
                <Select
                  options={[
                    { value: 'kubevirt', label: 'KubeVirt' },
                    { value: 'pve', label: 'PVE' },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="connectionId"
                label={localeText(localeCode, '连接', 'Connection')}
                rules={[{ required: true }]}
              >
                <Select
                  showSearch={{ optionFilterProp: 'label' }}
                  loading={clustersQuery.isFetching}
                  notFoundContent={
                    clustersQuery.isError
                      ? localeText(
                          localeCode,
                          '连接选项读取失败，请重试',
                          'Connection options could not be read; retry',
                        )
                      : undefined
                  }
                  options={clusters
                    .filter((item) => !imageProvider || item.provider === imageProvider)
                    .map((item) => ({ value: item.id, label: item.name }))}
                />
              </Form.Item>
            </div>

            <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
              <Form.Item
                name="sourceKind"
                label={localeText(localeCode, '来源类型', 'Source type')}
                rules={[{ required: true }]}
              >
                <Select
                  options={
                    imageProvider === 'pve'
                      ? [
                          { value: 'cloud_image', label: 'PVE cloud image (QCOW2)' },
                          { value: 'template', label: 'PVE template' },
                          { value: 'iso', label: 'PVE ISO' },
                        ]
                      : [
                          { value: 'datasource', label: 'KubeVirt DataSource' },
                          { value: 'pvc', label: 'PVC' },
                        ]
                  }
                />
              </Form.Item>
              <Form.Item
                name="sourceRef"
                label={localeText(localeCode, '来源引用', 'Source reference')}
                rules={[{ required: true }]}
              >
                <Input
                  placeholder={
                    imageProvider === 'pve' ? 'VMID 或 storage:volume' : 'namespace/name'
                  }
                />
              </Form.Item>
            </div>
            {imageProvider === 'kubevirt' ? (
              <Form.Item name="namespace" label={localeText(localeCode, '命名空间', 'Namespace')}>
                <Input />
              </Form.Item>
            ) : null}
            <Form.Item name={['config', 'cloudInitSupported']} valuePropName="checked">
              <Checkbox>
                {localeText(localeCode, '已确认镜像支持 cloud-init', 'Cloud-init support verified')}
              </Checkbox>
            </Form.Item>
            <div className="soha-vrt-form-grid soha-vrt-form-grid--2">
              <Form.Item
                name="osType"
                label={localeText(localeCode, '操作系统', 'Operating system')}
              >
                <Input placeholder="alpine / ubuntu / windows" />
              </Form.Item>
              <Form.Item name="sizeGiB" label={localeText(localeCode, '大小 GiB', 'Size GiB')}>
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
