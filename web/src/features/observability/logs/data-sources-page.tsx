import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { StepForm } from '@/components/step-form'
import { StepFormModal } from '@/components/step-form-modal'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  EditOutlined,
  ExperimentOutlined,
  PlusOutlined,
  PoweroffOutlined,
} from '@ant-design/icons'
import type {
  ObservabilityDataSource,
  ObservabilityDataSourceInput,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import {
  Button,
  Collapse,
  Form,
  Input,
  InputNumber,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tag,
  Typography,
} from 'antd'
import { useEffect, useState } from 'react'
import '../observability-pages.css'
import { observabilityProviderQueries } from '../provider-queries'
import { observabilityLogMutations } from './mutations'
import { observabilityLogQueries } from './queries'
import './styles.css'

const { Text } = Typography
const labelDefaults = {
  labelCluster: 'cluster',
  labelNamespace: 'namespace',
  labelService: 'service',
  labelWorkload: 'workload',
  labelSeverity: 'level',
  labelPod: 'pod',
  labelContainer: 'container',
}

interface DataSourceFormValues {
  name: string
  providerKey: string
  enabled: boolean
  endpoint: string
  tenantId?: string
  clusterIds?: string[]
  namespaces?: string[]
  maxEntries: number
  maxRangeSeconds: number
  timeoutSeconds: number
  dropAttributeKeys?: string[]
  bearerToken?: string
  credentials?: Array<{ key?: string; value?: string }>
  configuration?: Array<{ key?: string; value?: string }>
  clearCredentialKeys?: string[]
  labelCluster?: string
  labelNamespace?: string
  labelService?: string
  labelWorkload?: string
  labelSeverity?: string
  labelPod?: string
  labelContainer?: string
}

function clean(value?: string) {
  return value?.trim() || undefined
}

function formValues(item?: ObservabilityDataSource): DataSourceFormValues {
  const labels = item?.config.labelKeys
  return {
    name: item?.name ?? '',
    providerKey: item?.providerKey ?? item?.backendType ?? 'loki',
    enabled: item?.enabled ?? true,
    endpoint: item?.config.endpoint ?? '',
    tenantId: item?.config.tenantId,
    clusterIds: item?.scope.clusterIds,
    namespaces: item?.scope.namespaces,
    maxEntries: item?.queryBudget.maxEntries ?? 1000,
    maxRangeSeconds: item?.queryBudget.maxRangeSeconds ?? 86400,
    timeoutSeconds: item?.queryBudget.timeoutSeconds ?? 10,
    dropAttributeKeys: item?.redactionPolicy.dropAttributeKeys,
    ...labelDefaults,
    labelCluster: labels?.cluster ?? labelDefaults.labelCluster,
    labelNamespace: labels?.namespace ?? labelDefaults.labelNamespace,
    labelService: labels?.service ?? labelDefaults.labelService,
    labelWorkload: labels?.workload ?? labelDefaults.labelWorkload,
    labelSeverity: labels?.severity ?? labelDefaults.labelSeverity,
    labelPod: labels?.pod ?? labelDefaults.labelPod,
    labelContainer: labels?.container ?? labelDefaults.labelContainer,
    configuration: item?.config.configuration,
  }
}

export function buildDataSourceInput(values: DataSourceFormValues): ObservabilityDataSourceInput {
  const { providerKey } = values
  const builtIn = providerKey === 'loki'
  const config: ObservabilityDataSourceInput['config'] = {
    endpoint: values.endpoint.trim(),
    tenantId: clean(values.tenantId),
  }
  if (builtIn) {
    config.labelKeys = {
      cluster: clean(values.labelCluster),
      namespace: clean(values.labelNamespace),
      service: clean(values.labelService),
      workload: clean(values.labelWorkload),
      severity: clean(values.labelSeverity),
      pod: clean(values.labelPod),
      container: clean(values.labelContainer),
    }
  } else {
    config.configuration = values.configuration
      ?.map((item) => ({ key: item.key?.trim() ?? '', value: item.value?.trim() ?? '' }))
      .filter((item) => item.key && item.value)
  }
  const credentials = (
    builtIn
      ? [{ key: 'bearer_token', value: clean(values.bearerToken) }]
      : (values.credentials ?? []).map((item) => ({
          key: item.key?.trim() ?? '',
          value: item.value?.trim() || undefined,
        }))
  ).filter((item): item is { key: string; value: string } => Boolean(item.key && item.value))
  return {
    name: values.name.trim(),
    backendType: builtIn ? 'loki' : 'provider',
    providerKey: builtIn ? undefined : providerKey,
    enabled: values.enabled,
    scope: { clusterIds: values.clusterIds, namespaces: values.namespaces },
    queryBudget: {
      maxEntries: values.maxEntries,
      maxRangeSeconds: values.maxRangeSeconds,
      timeoutSeconds: values.timeoutSeconds,
    },
    redactionPolicy: { dropAttributeKeys: values.dropAttributeKeys },
    config,
    credentials,
    clearCredentialKeys: values.clearCredentialKeys,
  }
}

function isRetiredLogSource(item: ObservabilityDataSource) {
  return item.backendType === 'elasticsearch' || item.backendType === 'clickhouse'
}

export function LogDataSourcesPage() {
  const queryClient = useQueryClient()
  const snapshot = usePermissionSnapshot().data?.data
  const canCreate = hasPermission(snapshot, 'observe.log-data-sources.create')
  const canUpdate = hasPermission(snapshot, 'observe.log-data-sources.update')
  const canValidate = hasPermission(snapshot, 'observe.log-data-sources.validate')
  const [form] = Form.useForm<DataSourceFormValues>()
  const providerKey = Form.useWatch('providerKey', form)
  const [step, setStep] = useState(0)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<ObservabilityDataSource | null>(null)
  const sourcesQuery = useQuery(observabilityLogQueries.dataSources())
  const providersQuery = useQuery(observabilityProviderQueries.providers())
  const createMutation = useOperationMutation(
    observabilityLogMutations.createDataSource(queryClient),
    {
      label: ['创建日志数据源', 'Create log data source'],
      target: (input) => input.name,
      receipt: (result) => ({ message: '数据源记录已创建', evidence: result.id }),
    },
  )
  const updateMutation = useOperationMutation(
    observabilityLogMutations.updateDataSource(queryClient),
    {
      label: ['更新日志数据源', 'Update log data source'],
      target: ({ id }) => id,
      receipt: (result) => ({ message: '数据源记录已更新', evidence: result.id }),
    },
  )
  const validateMutation = useOperationMutation(
    observabilityLogMutations.validateDataSource(queryClient),
    {
      label: ['验证日志数据源', 'Validate log data source'],
      target: (id) => id,
      resultError: (result) =>
        result.validationStatus === 'healthy'
          ? undefined
          : new Error(result.validationMessage || '连接验证未通过'),
      receipt: (result) => ({ message: '连接验证记录已返回', evidence: result.validationStatus }),
    },
  )
  const denied = queryAccessDenied(sourcesQuery)
  const writeBlocked = createMutation.blocked || updateMutation.blocked || denied
  const blocked =
    writeBlocked || queryAccessDenied(providersQuery) || !readableQueryData(providersQuery)
  useEffect(() => {
    if (denied || queryAccessDenied(createMutation) || queryAccessDenied(updateMutation)) {
      setOpen(false)
      setEditing(null)
      form.resetFields()
    }
  }, [denied, createMutation.error, updateMutation.error, form])
  const providers = (readableQueryData(providersQuery) ?? []).filter(
    (provider) =>
      (!provider.builtIn || provider.providerKey === 'loki') &&
      provider.status !== 'unsupported' &&
      provider.signals.includes('logs') &&
      provider.capabilities.includes('logs.query'),
  )
  const providerOptions = providers.map((provider) => ({
    value: provider.providerKey,
    label: provider.displayName,
  }))
  const builtIn = providerKey === 'loki'

  function openEditor(item?: ObservabilityDataSource) {
    if (writeBlocked) return
    createMutation.reset()
    updateMutation.reset()
    setStep(0)
    setEditing(item ?? null)
    form.setFieldsValue(formValues(item))
    setOpen(true)
  }

  function closeEditor() {
    if (writeBlocked) return
    clearEditor()
  }

  function clearEditor() {
    setOpen(false)
    setEditing(null)
    form.resetFields()
  }

  function submit(values: DataSourceFormValues) {
    if (blocked || !providers.some((provider) => provider.providerKey === values.providerKey)) return
    const input = buildDataSourceInput(values)
    if (editing) {
      updateMutation.mutate(
        { id: editing.id, input },
        {
          onSuccess: () => {
            clearEditor()
          },
        },
      )
      return
    }
    createMutation.mutate(input, {
      onSuccess: () => {
        clearEditor()
      },
    })
  }

  function disableRetiredSource(item: ObservabilityDataSource) {
    const { name, backendType, providerKey, scope, queryBudget, redactionPolicy, config } = item
    updateMutation.mutate({
      id: item.id,
      input: {
        name,
        backendType,
        providerKey,
        scope,
        queryBudget,
        redactionPolicy,
        config,
        enabled: false,
      },
    })
  }
  const columns: TableColumnsType<ObservabilityDataSource> = [
    {
      title: '数据源',
      dataIndex: 'name',
      width: 260,
      render: (name: string, item) => (
        <Space orientation="vertical" size={2}>
          <Text strong>{name}</Text>
          <Text type="secondary" className="text-xs">
            {item.config.endpoint}
          </Text>
        </Space>
      ),
    },
    {
      title: '后端',
      dataIndex: 'backendType',
      width: 150,
      render: (value: string, item) => <Tag>{item.providerKey ?? value}</Tag>,
    },
    {
      title: '作用域',
      key: 'scope',
      width: 280,
      render: (_, item) => (
        <Space orientation="vertical" size={2}>
          <Text>{item.scope.clusterIds?.join(', ') || '全部集群'}</Text>
          <Text type="secondary">{item.scope.namespaces?.join(', ') || '全部命名空间'}</Text>
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'status',
      width: 190,
      render: (_, item) => (
        <Space orientation="vertical" size={2}>
          <Space size={4} wrap>
            {isRetiredLogSource(item) ? (
              <StatusTag value="unsupported" label="后端已退役" />
            ) : (
              <StatusTag value={item.validationStatus} />
            )}
            <BooleanTag value={item.enabled} trueLabel="启用" falseLabel="停用" />
          </Space>
          {!isRetiredLogSource(item) &&
          item.validationStatus !== 'healthy' &&
          item.validationMessage ? (
            <Text className="text-xs" type="secondary">
              {item.validationMessage}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: '认证',
      dataIndex: 'credentialKeys',
      width: 180,
      render: (keys: string[]) => keys.map((key) => <Tag key={key}>{key}</Tag>),
    },
    {
      title: '最近验证',
      dataIndex: 'lastValidatedAt',
      width: 180,
      render: (value?: string) => (value ? formatDateTime(value) : '-'),
    },
    {
      title: '操作',
      key: 'actions',
      fixed: 'right',
      width: 104,
      render: (_, item) => {
        if (isRetiredLogSource(item)) {
          if (!canUpdate || !item.enabled) return '-'
          return (
            <Popconfirm
              title={`停用日志数据源 ${item.name}？`}
              okText="停用"
              cancelText="取消"
              disabled={writeBlocked}
              onConfirm={() => disableRetiredSource(item)}
            >
              <ManagementIconButton
                aria-label="停用旧日志数据源"
                disabled={writeBlocked}
                icon={<PoweroffOutlined />}
                tooltip="停用"
              />
            </Popconfirm>
          )
        }
        return (
          <Space size={2}>
            <ManagementIconButton
              aria-label="验证日志数据源"
              disabled={!canValidate || validateMutation.blocked || denied}
              icon={<ExperimentOutlined />}
              loading={validateMutation.isPending && validateMutation.variables === item.id}
              tooltip="验证连接"
              onClick={() => validateMutation.mutate(item.id)}
            />
            <ManagementIconButton
              aria-label="编辑日志数据源"
              disabled={!canUpdate || writeBlocked}
              icon={<EditOutlined />}
              tooltip="编辑"
              onClick={() => openEditor(item)}
            />
          </Space>
        )
      },
    },
  ]

  return (
    <div className="soha-page">
      <h1 className="soha-observability-page-heading">日志数据源</h1>
      {!open ? (
        <>
          {createMutation.feedback}
          {updateMutation.feedback}
        </>
      ) : null}
      {validateMutation.feedback}
      <AdminTable
        enableDensity
        error={sourcesQuery.error}
        hasReadData={sourcesQuery.data !== undefined}
        refreshing={sourcesQuery.isFetching}
        onRefresh={() => void sourcesQuery.refetch()}
        headerExtra={
          canCreate ? (
            <ManagementTableToolbar>
              <Button
                disabled={writeBlocked}
                icon={<PlusOutlined />}
                type="primary"
                onClick={() => openEditor()}
              >
                新建数据源
              </Button>
            </ManagementTableToolbar>
          ) : null
        }
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={sourcesQuery.data ?? []}
        empty={<ManagementState bordered={false} compact description="暂无日志数据源" />}
        loading={sourcesQuery.isLoading}
        pageSize={20}
        rowKey="id"
        scroll={{ x: 'max-content' }}
      />

      <StepFormModal
        open={open && !denied}
        closeDisabled={writeBlocked}
        feedback={
          <>
            <ReadFeedback label="日志 Provider" query={providersQuery} />
            {createMutation.feedback}
            {updateMutation.feedback}
          </>
        }
        title={editing ? '编辑日志数据源' : '新建日志数据源'}
        width={860}
        onClose={closeEditor}
      >
        <StepForm
          disabled={blocked}
          contentMaxWidth="100%"
          onCancel={closeEditor}
          form={form}
          current={step}
          onCurrentChange={setStep}
          onFinish={submit}
          loading={createMutation.isPending || updateMutation.isPending}
          initialValues={formValues(editing ?? undefined)}
          steps={[
            {
              title: '连接与 Provider',
              fieldNames: ['name', 'providerKey', 'endpoint'],
              children: (
                <div className="soha-observability-form-grid">
                  <Form.Item
                    label="名称"
                    name="name"
                    rules={[{ required: true, whitespace: true }]}
                  >
                    <Input />
                  </Form.Item>
                  <Form.Item
                    label="Provider"
                    name="providerKey"
                    rules={[
                      {
                        required: true,
                        validator: (_, value: string) =>
                          providers.some((provider) => provider.providerKey === value)
                            ? Promise.resolve()
                            : Promise.reject(new Error('请选择可用的日志 Provider')),
                      },
                    ]}
                  >
                    <Select
                      loading={providersQuery.isLoading}
                      options={providerOptions}
                      placeholder="选择日志 Provider"
                    />
                  </Form.Item>
                  <Form.Item
                    label="Endpoint"
                    name="endpoint"
                    rules={[{ required: true, type: 'url' }]}
                  >
                    <Input placeholder="https://logs.example.com" />
                  </Form.Item>
                  <Form.Item label="Tenant ID" name="tenantId">
                    <Input />
                  </Form.Item>
                  <Form.Item label="启用" name="enabled" valuePropName="checked">
                    <Switch />
                  </Form.Item>
                </div>
              ),
            },
            {
              title: '范围与查询限制',
              fieldNames: ['maxEntries', 'maxRangeSeconds', 'timeoutSeconds'],
              children: (
                <div className="soha-observability-form-grid">
                  <Form.Item label="集群范围" name="clusterIds">
                    <Select mode="tags" tokenSeparators={[',']} placeholder="留空表示全部集群" />
                  </Form.Item>
                  <Form.Item label="命名空间范围" name="namespaces">
                    <Select
                      mode="tags"
                      tokenSeparators={[',']}
                      placeholder="留空表示全部命名空间"
                    />
                  </Form.Item>
                  <Form.Item label="每页最大行数" name="maxEntries" rules={[{ required: true }]}>
                    <InputNumber min={1} max={1000} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item
                    label="最大时间范围（秒）"
                    name="maxRangeSeconds"
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={60} max={2592000} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item
                    label="查询超时（秒）"
                    name="timeoutSeconds"
                    rules={[{ required: true }]}
                  >
                    <InputNumber min={1} max={60} style={{ width: '100%' }} />
                  </Form.Item>
                  <Form.Item label="脱敏属性" name="dropAttributeKeys">
                    <Select mode="tags" tokenSeparators={[',']} />
                  </Form.Item>
                </div>
              ),
            },
            {
              title: '认证与字段映射',
              children: (
                <>
                  <div className="soha-observability-form-grid">
                    {builtIn ? (
                      <Form.Item label="Bearer Token" name="bearerToken">
                        <Input.Password autoComplete="new-password" placeholder="留空保持不变" />
                      </Form.Item>
                    ) : null}
                    {!builtIn ? (
                      <>
                        <Form.List name="configuration">
                          {(fields, { add, remove }) => (
                            <Form.Item label="Provider 配置">
                              <Space orientation="vertical" style={{ width: '100%' }}>
                                {fields.map((field) => (
                                  <div className="soha-log-provider-field-row" key={field.key}>
                                    <Form.Item name={[field.name, 'key']} noStyle>
                                      <Input placeholder="配置键" />
                                    </Form.Item>
                                    <Form.Item name={[field.name, 'value']} noStyle>
                                      <Input placeholder="配置值" />
                                    </Form.Item>
                                    <Button onClick={() => remove(field.name)}>移除</Button>
                                  </div>
                                ))}
                                <Button onClick={() => add()}>添加配置</Button>
                              </Space>
                            </Form.Item>
                          )}
                        </Form.List>
                        <Form.List name="credentials">
                          {(fields, { add, remove }) => (
                            <Form.Item label="Provider 凭据">
                              <Space orientation="vertical" style={{ width: '100%' }}>
                                {fields.map((field) => (
                                  <div className="soha-log-provider-field-row" key={field.key}>
                                    <Form.Item name={[field.name, 'key']} noStyle>
                                      <Input placeholder="凭据键" />
                                    </Form.Item>
                                    <Form.Item name={[field.name, 'value']} noStyle>
                                      <Input.Password
                                        autoComplete="new-password"
                                        placeholder="凭据值"
                                      />
                                    </Form.Item>
                                    <Button onClick={() => remove(field.name)}>移除</Button>
                                  </div>
                                ))}
                                <Button onClick={() => add()}>添加凭据</Button>
                              </Space>
                            </Form.Item>
                          )}
                        </Form.List>
                      </>
                    ) : null}
                    {editing?.credentialKeys.length ? (
                      <Form.Item label="清除认证字段" name="clearCredentialKeys">
                        <Select
                          mode="multiple"
                          options={editing.credentialKeys.map((key) => ({
                            value: key,
                            label: key,
                          }))}
                        />
                      </Form.Item>
                    ) : null}
                  </div>

                  {builtIn ? (
                    <Collapse
                      ghost
                      items={[
                        {
                          key: 'mapping',
                          label: '标签映射',
                          children: (
                            <div className="soha-observability-form-grid">
                              {[
                                ['labelCluster', 'Cluster'],
                                ['labelNamespace', 'Namespace'],
                                ['labelService', 'Service'],
                                ['labelWorkload', 'Workload'],
                                ['labelSeverity', 'Severity'],
                                ['labelPod', 'Pod'],
                                ['labelContainer', 'Container'],
                              ].map(([name, label]) => (
                                <Form.Item key={name} label={label} name={name}>
                                  <Input />
                                </Form.Item>
                              ))}
                            </div>
                          ),
                        },
                      ]}
                    />
                  ) : null}
                </>
              ),
            },
          ]}
        />
      </StepFormModal>
    </div>
  )
}
