import { useEffect, useMemo, useState } from 'react'
import {
  App,
  Alert,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Select,
  Space,
  Tabs,
  Typography,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  DeleteOutlined,
  EditOutlined,
  FileAddOutlined,
  HistoryOutlined,
  PlusOutlined,
  RocketOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementIconButton,
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementQueryField,
  ManagementRefreshButton,
  ManagementTableToolbar,
} from '@/components/management-list'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { deliveryQueries } from '../queries'
import type { ApplicationServiceComponent } from '../types'
import { manifestMutations } from './mutations'
import { manifestQueries } from './queries'
import { ManifestOperationsPanel } from './operations-panel'
import type { ManifestFilter, ManifestPackage, ManifestPackageInput } from './types'
import './styles.css'

const { Text } = Typography

const DEFAULT_INGRESS = `apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: example
spec:
  ingressClassName: nginx
  rules:
    - host: app.dev.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: example
                port:
                  number: 80
`

function emptyInput(applicationId = '', serviceId?: string): ManifestPackageInput {
  return {
    name: '',
    applicationId,
    serviceId,
    renderer: 'raw_yaml',
    files: [{ path: 'base/ingress.yaml', content: DEFAULT_INGRESS }],
    bindings: [],
  }
}

export function ManifestLibraryPage() {
  return <ManifestLibraryWorkspace readOnly />
}

export function ManifestLibraryWorkspace({
  applicationId,
  serviceId,
  services = [],
  readOnly = false,
}: {
  applicationId?: string
  serviceId?: string
  services?: ApplicationServiceComponent[]
  readOnly?: boolean
}) {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const [queryForm] = Form.useForm()
  const [editorForm] = Form.useForm<ManifestPackageInput>()
  const [searchParams, setSearchParams] = useSearchParams()
  const urlFilter = useMemo<ManifestFilter>(() => {
    const page = Number(searchParams.get('page'))
    const pageSize = Number(searchParams.get('pageSize'))
    return {
      search: searchParams.get('search')?.trim() || undefined,
      applicationId: searchParams.get('applicationId')?.trim() || undefined,
      page: Number.isInteger(page) && page > 0 ? page : 1,
      pageSize: Number.isInteger(pageSize) && pageSize > 0 ? pageSize : 20,
    }
  }, [searchParams])
  const [scopedFilter, setScopedFilter] = useState<ManifestFilter>({
    applicationId,
    serviceId,
    page: 1,
    pageSize: 20,
  })
  const filter = applicationId ? { ...scopedFilter, applicationId, serviceId } : urlFilter
  const [editing, setEditing] = useState<ManifestPackage | null | undefined>(undefined)
  const [revisionTarget, setRevisionTarget] = useState<ManifestPackage | null>(null)
  const [publishTarget, setPublishTarget] = useState<ManifestPackage | null>(null)
  const [publishNote, setPublishNote] = useState('')
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')
  const snapshot = usePermissionSnapshot().data?.data
  const canEdit = !readOnly && hasPermission(snapshot, 'delivery.application.update')
  const canDelete = !readOnly && hasPermission(snapshot, 'delivery.application.delete')
  const canPublish = !readOnly && hasPermission(snapshot, 'delivery.releases.trigger')
  const selectedApplicationId = Form.useWatch('applicationId', editorForm)
  const packagesQuery = useQuery(manifestQueries.list(filter))
  const applicationsQuery = useQuery(deliveryQueries.applications.list())
  const environmentsQuery = useQuery(deliveryQueries.environments.list())
  const revisionsQuery = useQuery(
    manifestQueries.revisions(revisionTarget?.id ?? '', Boolean(revisionTarget)),
  )
  const editingSourceQuery = useQuery(manifestQueries.source(editing?.id ?? '', Boolean(editing)))
  const gitFilesReadOnly = editingSourceQuery.data?.mode === 'git_synced'
  const createMutation = useMutation(manifestMutations.create(queryClient))
  const updateMutation = useMutation(manifestMutations.update(queryClient))
  const removeMutation = useMutation(manifestMutations.remove(queryClient))
  const publishMutation = useMutation(manifestMutations.publish(queryClient))

  const applicationOptions = useMemo(
    () =>
      (applicationsQuery.data ?? []).map((item) => ({
        value: item.id,
        label: item.name || item.key || item.id,
      })),
    [applicationsQuery.data],
  )
  const applicationNames = useMemo(
    () => new Map(applicationOptions.map((item) => [item.value, item.label])),
    [applicationOptions],
  )
  const serviceOptions = useMemo(
    () => services.map((item) => ({ value: item.id, label: item.name || item.key || item.id })),
    [services],
  )
  const serviceNames = useMemo(
    () => new Map(serviceOptions.map((item) => [item.value, item.label])),
    [serviceOptions],
  )
  const environmentOptions = useMemo(
    () =>
      (environmentsQuery.data ?? [])
        .filter((item) => item.applicationId === selectedApplicationId)
        .map((item) => ({
          value: item.id,
          label: item.environmentKey || item.environmentId,
          item,
        })),
    [environmentsQuery.data, selectedApplicationId],
  )

  useEffect(() => {
    queryForm.setFieldsValue({
      search: filter.search ?? '',
      applicationId: filter.applicationId ?? '',
    })
  }, [filter.applicationId, filter.search, queryForm])

  useEffect(() => {
    if (!applicationId) return
    setScopedFilter({ applicationId, serviceId, page: 1, pageSize: 20 })
  }, [applicationId, serviceId])

  const updateFilter = (next: ManifestFilter) => {
    if (applicationId) {
      setScopedFilter({ ...next, applicationId, serviceId })
      return
    }
    const params = new URLSearchParams()
    if (next.search) params.set('search', next.search)
    if (next.applicationId) params.set('applicationId', next.applicationId)
    if ((next.page ?? 1) > 1) params.set('page', String(next.page))
    if ((next.pageSize ?? 20) !== 20) params.set('pageSize', String(next.pageSize))
    setSearchParams(params, { replace: true })
  }

  const openEditor = (item: ManifestPackage | null) => {
    setEditing(item)
    editorForm.setFieldsValue(
      item
        ? {
            name: item.name,
            description: item.description,
            applicationId: item.applicationId,
            serviceId: item.serviceId,
            businessLineId: item.businessLineId,
            renderer: item.renderer,
            files: item.files,
            bindings: item.bindings,
          }
        : emptyInput(applicationId, serviceId),
    )
  }

  const save = async () => {
    const input = await editorForm.validateFields()
    try {
      if (editing) await updateMutation.mutateAsync({ id: editing.id, input })
      else await createMutation.mutateAsync(input)
      message.success(editing ? '清单草稿已更新' : '清单包已创建')
      setEditing(undefined)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  const columns: TableColumnsType<ManifestPackage> = [
    {
      title: '清单包',
      dataIndex: 'name',
      width: 250,
      render: (name: string, item) => {
        const content = (
          <>
            <strong>{name}</strong>
            <span>
              {item.description || applicationNames.get(item.applicationId) || item.applicationId}
            </span>
          </>
        )
        return canEdit ? (
          <Button className="soha-manifest-name" type="text" onClick={() => openEditor(item)}>
            {content}
          </Button>
        ) : (
          <div className="soha-manifest-name is-readonly">{content}</div>
        )
      },
    },
    ...(applicationId
      ? []
      : [
          {
            title: '应用',
            dataIndex: 'applicationId',
            render: (value: string) => applicationNames.get(value) || value,
          },
        ]),
    {
      title: '归属',
      dataIndex: 'serviceId',
      width: 160,
      render: (value?: string) => (value ? serviceNames.get(value) || value : '应用级'),
    },
    {
      title: '渲染方式',
      dataIndex: 'renderer',
      width: 130,
      render: (value: string) => (
        <MetadataTag label={value === 'kustomize' ? 'Kustomize' : '原生 YAML'} />
      ),
    },
    {
      title: '文件 / 环境',
      key: 'counts',
      width: 130,
      render: (_, item) => `${item.files.length} / ${item.bindings.length}`,
    },
    {
      title: '版本',
      dataIndex: 'currentRevision',
      width: 100,
      render: (value: number) => (value > 0 ? `v${value}` : '-'),
    },
    {
      title: '状态',
      dataIndex: 'status',
      width: 110,
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '更新时间',
      dataIndex: 'updatedAt',
      width: 180,
      render: formatDateTime,
    },
    {
      title: '操作',
      key: 'actions',
      fixed: 'right',
      render: (_, item) => (
        <Space size={4}>
          {canEdit ? (
            <ManagementIconButton
              aria-label="编辑清单"
              icon={<EditOutlined />}
              tooltip="编辑"
              onClick={() => openEditor(item)}
            />
          ) : null}
          <ManagementIconButton
            aria-label="查看版本"
            icon={<HistoryOutlined />}
            tooltip="版本记录"
            onClick={() => setRevisionTarget(item)}
          />
          {canPublish ? (
            <ManagementIconButton
              aria-label="发布版本"
              icon={<RocketOutlined />}
              tooltip="发布版本"
              onClick={() => setPublishTarget(item)}
            />
          ) : null}
          {canDelete ? (
            <Popconfirm
              title={item.currentRevision > 0 ? '归档清单包？' : '删除清单草稿？'}
              description={
                item.currentRevision > 0
                  ? '清单将从工作台隐藏，已发布版本和审计历史会保留。'
                  : '未发布草稿将被永久删除，此操作不可恢复。'
              }
              onConfirm={() => removeMutation.mutateAsync(item.id)}
            >
              <ManagementIconButton
                aria-label="删除清单"
                danger
                icon={<DeleteOutlined />}
                tooltip="删除"
              />
            </Popconfirm>
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <>
      <ManagementDataPage
        className="soha-manifest-library"
        query={{
          form: queryForm,
          initialValues: { search: filter.search ?? '', applicationId: filter.applicationId ?? '' },
          onFinish: (values) =>
            updateFilter({
              search: values.search?.trim() || '',
              applicationId: values.applicationId || '',
              page: 1,
              pageSize: filter.pageSize,
            }),
          actions: (
            <ManagementQueryActions
              onReset={() => {
                queryForm.resetFields()
                updateFilter({ page: 1, pageSize: filter.pageSize })
              }}
            />
          ),
          children: (
            <>
              <ManagementKeywordField name="search" placeholder="搜索清单或应用 ID" />
              {!applicationId ? (
                <ManagementQueryField label="应用" name="applicationId">
                  <Select
                    allowClear
                    showSearch={{ optionFilterProp: 'label' }}
                    options={applicationOptions}
                    placeholder="全部应用"
                  />
                </ManagementQueryField>
              ) : null}
            </>
          ),
        }}
        table={{
          title: <Text strong>{applicationId ? '扩展资源' : '应用清单'}</Text>,
          headerExtra: (
            <ManagementTableToolbar>
              {canEdit ? (
                <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor(null)}>
                  新建清单包
                </Button>
              ) : null}
              <ManagementDensityButton
                aria-label="切换表格密度"
                tooltip={tableSize === 'small' ? '切换为舒展密度' : '切换为紧凑密度'}
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <ManagementRefreshButton
                aria-label="刷新清单"
                loading={packagesQuery.isFetching}
                tooltip="刷新"
                onClick={() => void packagesQuery.refetch()}
              />
            </ManagementTableToolbar>
          ),
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          columns,
          dataSource: packagesQuery.data?.items ?? [],
          loading: packagesQuery.isLoading,
          rowKey: 'id',
          tableSize,
          pageSize: filter.pageSize,
          pagination: {
            current: packagesQuery.data?.page ?? filter.page,
            pageSize: packagesQuery.data?.pageSize ?? filter.pageSize,
            total: packagesQuery.data?.total ?? 0,
            onPageChange: (page: number) => updateFilter({ ...filter, page }),
            onPageSizeChange: (pageSize: number) => updateFilter({ ...filter, page: 1, pageSize }),
          },
          paginationSummary: (total, range) => (
            <Text type="secondary">
              {total > 0 ? `当前 ${range[0]}-${range[1]} / ${total} 条` : '当前 0 / 0 条'}
            </Text>
          ),
          scroll: { x: 'max-content' },
        }}
      />

      <Drawer
        open={editing !== undefined}
        size={920}
        destroyOnHidden
        title={editing ? `编辑 ${editing.name}` : '新建清单包'}
        onClose={() => setEditing(undefined)}
        footer={
          canEdit ? (
            <div className="soha-manifest-drawer-footer">
              <Button onClick={() => setEditing(undefined)}>取消</Button>
              <Button
                type="primary"
                loading={createMutation.isPending || updateMutation.isPending}
                onClick={save}
              >
                保存草稿
              </Button>
            </div>
          ) : null
        }
      >
        <Form form={editorForm} layout="vertical" requiredMark="optional">
          <Tabs
            className="soha-resource-tabs"
            items={[
              {
                key: 'basic',
                label: '基本信息',
                children: (
                  <div className="soha-manifest-form-grid">
                    <Form.Item label="名称" name="name" rules={[{ required: true }]}>
                      <Input placeholder="例如：支付服务入口配置" />
                    </Form.Item>
                    <Form.Item label="所属应用" name="applicationId" rules={[{ required: true }]}>
                      <Select
                        disabled={Boolean(applicationId)}
                        showSearch={{ optionFilterProp: 'label' }}
                        options={applicationOptions}
                        onChange={() => {
                          editorForm.setFieldValue('serviceId', undefined)
                          editorForm.setFieldValue('bindings', [])
                        }}
                      />
                    </Form.Item>
                    <Form.Item label="所属服务" name="serviceId" extra="留空表示应用级共享资源">
                      <Select
                        allowClear
                        disabled={Boolean(serviceId) || !selectedApplicationId}
                        options={serviceOptions}
                        placeholder="应用级"
                      />
                    </Form.Item>
                    <Form.Item label="业务线 ID" name="businessLineId">
                      <Input />
                    </Form.Item>
                    <Form.Item label="渲染方式" name="renderer" rules={[{ required: true }]}>
                      <Select
                        options={[
                          { value: 'raw_yaml', label: '原生 YAML' },
                          { value: 'kustomize', label: 'Kustomize' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item className="soha-manifest-form-wide" label="描述" name="description">
                      <Input.TextArea rows={3} />
                    </Form.Item>
                  </div>
                ),
              },
              {
                key: 'files',
                label: 'YAML 文件',
                children: (
                  <Form.List name="files">
                    {(fields, { add, remove }) => (
                      <div className="soha-manifest-files">
                        {gitFilesReadOnly ? (
                          <Alert showIcon type="info" title="Git 是当前内容来源" />
                        ) : null}
                        <div className="soha-manifest-section-head">
                          <Text strong>清单文件</Text>
                          {!gitFilesReadOnly ? (
                            <Button
                              icon={<FileAddOutlined />}
                              onClick={() =>
                                add({
                                  path: `base/resource-${fields.length + 1}.yaml`,
                                  content: '',
                                })
                              }
                            >
                              添加文件
                            </Button>
                          ) : null}
                        </div>
                        {fields.map((field, index) => (
                          <div className="soha-manifest-file" key={field.key}>
                            <div className="soha-manifest-file-head">
                              <Form.Item
                                name={[field.name, 'path']}
                                rules={[{ required: true }]}
                                noStyle
                              >
                                <Input
                                  disabled={gitFilesReadOnly}
                                  aria-label={`文件 ${index + 1} 路径`}
                                  placeholder="base/ingress.yaml"
                                />
                              </Form.Item>
                              {!gitFilesReadOnly ? (
                                <Button
                                  danger
                                  type="text"
                                  icon={<DeleteOutlined />}
                                  aria-label="删除文件"
                                  onClick={() => remove(field.name)}
                                />
                              ) : null}
                            </div>
                            <Form.Item
                              name={[field.name, 'content']}
                              rules={[{ required: true, message: '请输入 YAML 内容' }]}
                              noStyle
                            >
                              <Input.TextArea
                                className="soha-manifest-editor"
                                readOnly={gitFilesReadOnly}
                                rows={18}
                                spellCheck={false}
                              />
                            </Form.Item>
                          </div>
                        ))}
                      </div>
                    )}
                  </Form.List>
                ),
              },
              {
                key: 'bindings',
                label: '环境绑定',
                children: (
                  <Form.List name="bindings">
                    {(fields, { add, remove }) => (
                      <div className="soha-manifest-bindings">
                        <div className="soha-manifest-section-head">
                          <Text strong>发布目标</Text>
                          <Button
                            icon={<PlusOutlined />}
                            onClick={() =>
                              add({
                                applicationEnvironmentId: '',
                                environmentKey: '',
                                clusterId: '',
                                namespace: 'default',
                              })
                            }
                          >
                            绑定环境
                          </Button>
                        </div>
                        {fields.map((field, index) => (
                          <div className="soha-manifest-binding" key={field.key}>
                            <Form.Item
                              label="应用环境"
                              name={[field.name, 'applicationEnvironmentId']}
                              rules={[{ required: true }]}
                            >
                              <Select
                                disabled={!selectedApplicationId}
                                options={environmentOptions}
                                placeholder={
                                  selectedApplicationId ? '选择应用环境' : '请先选择应用'
                                }
                                onChange={(value) => {
                                  const selected = environmentOptions.find(
                                    (option) => option.value === value,
                                  )?.item
                                  if (selected)
                                    editorForm.setFieldValue(
                                      ['bindings', field.name, 'environmentKey'],
                                      selected.environmentKey || selected.environmentId,
                                    )
                                }}
                              />
                            </Form.Item>
                            <Form.Item
                              label="环境标识"
                              name={[field.name, 'environmentKey']}
                              rules={[{ required: true }]}
                            >
                              <Input placeholder="dev / test / pre / prod" />
                            </Form.Item>
                            <Form.Item
                              label="集群 ID"
                              name={[field.name, 'clusterId']}
                              rules={[{ required: true }]}
                            >
                              <Input />
                            </Form.Item>
                            <Form.Item
                              label="Namespace"
                              name={[field.name, 'namespace']}
                              rules={[{ required: true }]}
                            >
                              <Input />
                            </Form.Item>
                            <Button
                              danger
                              type="text"
                              icon={<DeleteOutlined />}
                              aria-label={`删除环境绑定 ${index + 1}`}
                              onClick={() => remove(field.name)}
                            />
                          </div>
                        ))}
                      </div>
                    )}
                  </Form.List>
                ),
              },
              ...(editing
                ? [
                    {
                      key: 'operations',
                      label: '交付状态',
                      children: <ManifestOperationsPanel item={editing} />,
                    },
                  ]
                : []),
            ]}
          />
        </Form>
      </Drawer>

      <Drawer
        open={Boolean(revisionTarget)}
        size={680}
        title={`${revisionTarget?.name ?? ''} · 版本记录`}
        onClose={() => setRevisionTarget(null)}
      >
        {(revisionsQuery.data ?? []).map((revision) => (
          <Descriptions
            key={revision.id}
            className="soha-manifest-revision"
            size="small"
            column={2}
            bordered
          >
            <Descriptions.Item label="版本">v{revision.version}</Descriptions.Item>
            <Descriptions.Item label="创建时间">
              {formatDateTime(revision.createdAt)}
            </Descriptions.Item>
            <Descriptions.Item label="摘要" span={2}>
              <Text code copyable>
                {revision.digest.slice(0, 20)}
              </Text>
            </Descriptions.Item>
            <Descriptions.Item label="说明" span={2}>
              {revision.note || '-'}
            </Descriptions.Item>
          </Descriptions>
        ))}
        {!revisionsQuery.isLoading && (revisionsQuery.data?.length ?? 0) === 0 ? (
          <Text type="secondary">尚未发布版本</Text>
        ) : null}
      </Drawer>

      <Modal
        open={Boolean(publishTarget)}
        title="发布不可变版本"
        okText="发布版本"
        confirmLoading={publishMutation.isPending}
        onCancel={() => setPublishTarget(null)}
        onOk={async () => {
          if (!publishTarget) return
          await publishMutation.mutateAsync({ id: publishTarget.id, note: publishNote })
          message.success(`已发布 ${publishTarget.name} 的新版本`)
          setPublishTarget(null)
          setPublishNote('')
        }}
      >
        <Input.TextArea
          value={publishNote}
          onChange={(event) => setPublishNote(event.target.value)}
          rows={4}
          placeholder="记录本次变更内容"
        />
      </Modal>
    </>
  )
}
