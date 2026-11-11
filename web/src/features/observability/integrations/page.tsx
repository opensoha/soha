import { AdminTable } from '@/components/admin-table'
import {
  ManagementIconButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { CopyOutlined, EditOutlined, ExperimentOutlined, PlusOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import { App, Button, Form, Input, Modal, Select, Space, Switch, Tag, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'
import '../observability-pages.css'
import {
  alertIntegrationSamplePayload,
  alertIntegrationTypeLabel,
  alertIntegrationTypeOptions,
  buildAlertIntegrationPayload,
  buildAlertIntegrationTestPayload,
  buildWebhookURL,
  prettyObservabilityJson,
} from './model'
import { observabilityIntegrationMutations } from './mutations'
import { observabilityIntegrationQueries } from './queries'
import type {
  AlertIntegration,
  AlertIntegrationFormValues,
  AlertIntegrationTestFormValues,
  AlertIntegrationTestResult,
} from './types'

const { Text } = Typography

export function AlertIntegrationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateIntegration = hasPermission(
    permissionSnapshot,
    'observe.alert-integrations.create',
  )
  const canUpdateIntegration = hasPermission(
    permissionSnapshot,
    'observe.alert-integrations.update',
  )
  const canTestIntegration = hasPermission(permissionSnapshot, 'observe.alert-integrations.test')
  const [editorForm] = Form.useForm<AlertIntegrationFormValues>()
  const [testForm] = Form.useForm<AlertIntegrationTestFormValues>()
  const [editorOpen, setEditorOpen] = useState(false)
  const [testOpen, setTestOpen] = useState(false)
  const [editingIntegration, setEditingIntegration] = useState<AlertIntegration | null>(null)
  const [createdSecret, setCreatedSecret] = useState<AlertIntegration | null>(null)
  const [testResult, setTestResult] = useState<AlertIntegrationTestResult | null>(null)

  const integrationsQuery = useQuery(observabilityIntegrationQueries.list())
  const denied = queryAccessDenied(integrationsQuery)
  const secretGeneration = useRef(0)
  const secretAllowed = useRef({ create: false, update: false })
  secretAllowed.current = {
    create: !denied && canCreateIntegration,
    update: !denied && canUpdateIntegration,
  }
  useEffect(
    () => () => {
      secretGeneration.current += 1
    },
    [],
  )
  const createOptions = observabilityIntegrationMutations.create(queryClient)
  const updateOptions = observabilityIntegrationMutations.update(queryClient)
  const createIntegration = useOperationMutation(
    {
      ...createOptions,
      gcTime: 0,
      mutationFn: async (input, context) => {
        const generation = secretGeneration.current
        const integration = await createOptions.mutationFn!(input, context)
        if (
          generation === secretGeneration.current &&
          secretAllowed.current.create &&
          integration.token
        )
          setCreatedSecret(integration)
        return { ...integration, token: undefined }
      },
      onSuccess: async (...args) => {
        await createOptions.onSuccess?.(...args)
        setEditorOpen(false)
        setEditingIntegration(null)
        editorForm.resetFields()
      },
    },
    {
      label: ['创建告警集成', 'Create alert integration'],
      target: (input) => input.name || input.id,
      receipt: (result) => ({ message: '告警集成记录已创建', evidence: result.id }),
    },
  )
  const updateIntegration = useOperationMutation(
    {
      ...updateOptions,
      gcTime: 0,
      mutationFn: async (input, context) => {
        const generation = secretGeneration.current
        const integration = await updateOptions.mutationFn!(input, context)
        if (
          generation === secretGeneration.current &&
          secretAllowed.current.update &&
          integration.token
        )
          setCreatedSecret(integration)
        return { ...integration, token: undefined }
      },
      onSuccess: async (...args) => {
        await updateOptions.onSuccess?.(...args)
        setEditorOpen(false)
        setEditingIntegration(null)
        editorForm.resetFields()
      },
    },
    {
      label: ['更新告警集成', 'Update alert integration'],
      target: ({ id }) => id,
      receipt: (result) => ({ message: '告警集成记录已更新', evidence: result.id }),
    },
  )
  const testIntegration = useOperationMutation(
    {
      ...observabilityIntegrationMutations.test(),
      onSuccess: (result) => setTestResult(result),
    },
    {
      label: ['归一化测试', 'Normalize payload'],
      target: (input) => input.integrationType,
      readOnly: true,
      receipt: (result) => ({
        message: 'Payload 已归一化',
        evidence: `返回 ${result.acceptedCount} 项；未投递通知。`,
      }),
    },
  )
  const writeBlocked = createIntegration.blocked || updateIntegration.blocked
  const blocked = writeBlocked || denied
  useEffect(() => {
    if (
      denied ||
      queryAccessDenied(createIntegration) ||
      queryAccessDenied(updateIntegration) ||
      !(canCreateIntegration || canUpdateIntegration)
    ) {
      secretGeneration.current += 1
      setCreatedSecret(null)
      setEditorOpen(false)
      setEditingIntegration(null)
      editorForm.resetFields()
    }
  }, [
    denied,
    createIntegration.error,
    updateIntegration.error,
    canCreateIntegration,
    canUpdateIntegration,
    editorForm,
  ])
  useEffect(() => {
    if (denied || !canTestIntegration || queryAccessDenied(testIntegration)) {
      setTestOpen(false)
      setTestResult(null)
      testForm.resetFields()
    }
  }, [denied, canTestIntegration, testIntegration.error, testForm])
  function closeEditor() {
    if (writeBlocked) return
    setEditorOpen(false)
    setEditingIntegration(null)
    editorForm.resetFields()
  }
  function closeTest() {
    if (testIntegration.isPending) return
    setTestOpen(false)
    setTestResult(null)
    testForm.resetFields()
    testIntegration.reset()
  }

  function copyText(value: string, label: string) {
    const text = value.trim()
    if (!text || !navigator.clipboard) {
      message.warning(`${label}不可复制`)
      return
    }
    navigator.clipboard.writeText(text).then(
      () => message.success(`${label}已复制`),
      () => message.error('复制失败'),
    )
  }

  function openEditor(record: AlertIntegration | null) {
    if (blocked) return
    secretGeneration.current += 1
    createIntegration.reset()
    updateIntegration.reset()
    editorForm.resetFields()
    setEditingIntegration(record)
    setEditorOpen(true)
    editorForm.setFieldsValue(
      record
        ? {
            id: record.id,
            name: record.name,
            integrationType: record.integrationType,
            description: record.description || '',
            token: '',
            labelMapping: prettyObservabilityJson(record.labelMapping ?? {}),
            dedupeConfig: prettyObservabilityJson(record.dedupeConfig ?? {}),
            enabled: record.enabled,
          }
        : {
            id: '',
            name: '',
            integrationType: 'alertmanager_v1',
            description: '',
            token: '',
            labelMapping:
              '{\n  "clusterId": "cluster",\n  "namespace": "namespace",\n  "service": "service",\n  "role": "role"\n}',
            dedupeConfig:
              '{\n  "fingerprintLabels": ["alertname", "cluster", "namespace", "service"]\n}',
            enabled: true,
          },
    )
  }

  function openTest(record?: AlertIntegration) {
    if (!canTestIntegration || testIntegration.isPending || denied) return
    testIntegration.reset()
    testForm.resetFields()
    const integrationType = record?.integrationType || 'alertmanager_v1'
    setTestResult(null)
    setTestOpen(true)
    testForm.setFieldsValue({
      integrationType,
      labelMapping: prettyObservabilityJson(record?.labelMapping ?? {}),
      dedupeConfig: prettyObservabilityJson(record?.dedupeConfig ?? {}),
      payload: alertIntegrationSamplePayload(integrationType),
    })
  }

  function submitEditor(values: AlertIntegrationFormValues) {
    if (blocked || !(editingIntegration ? canUpdateIntegration : canCreateIntegration)) return
    try {
      const payload = buildAlertIntegrationPayload(values)
      if (editingIntegration?.id) {
        updateIntegration.mutate({ id: editingIntegration.id, payload })
        return
      }
      createIntegration.mutate(payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitTest(values: AlertIntegrationTestFormValues) {
    if (
      !canTestIntegration ||
      denied ||
      testIntegration.isPending ||
      queryAccessDenied(testIntegration)
    )
      return
    try {
      testIntegration.mutate(buildAlertIntegrationTestPayload(values))
    } catch (error) {
      message.error(error instanceof Error ? error.message : '测试失败')
    }
  }

  const columns: TableProps<AlertIntegration>['columns'] = [
    {
      title: '名称',
      dataIndex: 'name',
      width: 220,
      render: (value: string, record) => (
        <Space orientation="vertical" size={2}>
          <Text strong>{value || record.id}</Text>
          <Text type="secondary" className="text-xs">
            {record.id}
          </Text>
        </Space>
      ),
    },
    {
      title: '来源类型',
      dataIndex: 'integrationType',
      width: 180,
      render: (value: string) => <Tag>{alertIntegrationTypeLabel(value)}</Tag>,
    },
    {
      title: 'Webhook',
      dataIndex: 'webhookPath',
      width: 360,
      ellipsis: true,
      render: (value: string) => {
        const webhookURL = buildWebhookURL(value)
        return webhookURL ? (
          <Space size={4}>
            <Text code ellipsis style={{ maxWidth: 290 }}>
              {webhookURL}
            </Text>
            <ManagementIconButton
              aria-label="复制 Webhook 地址"
              icon={<CopyOutlined />}
              size="small"
              tooltip="复制地址"
              onClick={() => copyText(webhookURL, 'Webhook 地址')}
            />
          </Space>
        ) : (
          '-'
        )
      },
    },
    {
      title: 'Token',
      dataIndex: 'tokenPreview',
      width: 140,
      render: (value: string) => (value ? <Text code>{value}</Text> : '-'),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'status',
      render: (value: string, record) => (
        <Space size={4}>
          <StatusTag value={value || 'pending'} />
          <BooleanTag value={record.enabled} trueLabel="启用" falseLabel="禁用" />
        </Space>
      ),
    },
    {
      title: '最近接收',
      dataIndex: 'lastReceivedAt',
      width: 180,
      render: (value: string) => formatDateTime(value),
    },
    {
      title: '错误',
      dataIndex: 'lastError',
      ellipsis: true,
      render: (value: string) => value || '-',
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) => (
        <Space className="soha-row-action-icons" size={2}>
          <ManagementIconButton
            aria-label="测试告警集成"
            disabled={!canTestIntegration || testIntegration.isPending || denied}
            icon={<ExperimentOutlined />}
            size="small"
            tooltip="测试"
            onClick={() => openTest(record)}
          />
          <ManagementIconButton
            aria-label="复制 Webhook 地址"
            icon={<CopyOutlined />}
            size="small"
            tooltip="复制地址"
            onClick={() => copyText(buildWebhookURL(record.webhookPath), 'Webhook 地址')}
          />
          {canUpdateIntegration ? (
            <ManagementIconButton
              aria-label="编辑告警集成"
              disabled={blocked}
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => openEditor(record)}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      {!testOpen ? testIntegration.feedback : null}
      {!editorOpen ? (
        <>
          {createIntegration.feedback}
          {updateIntegration.feedback}
        </>
      ) : null}
      <h1 className="soha-observability-page-heading">告警集成</h1>
      <AdminTable
        enableDensity
        error={integrationsQuery.error}
        hasReadData={integrationsQuery.data !== undefined}
        refreshing={integrationsQuery.isFetching}
        onRefresh={() => void integrationsQuery.refetch()}
        headerExtra={
          canCreateIntegration || canTestIntegration ? (
            <ManagementTableToolbar>
              {canTestIntegration ? (
                <Button
                  disabled={testIntegration.isPending || denied || !canTestIntegration}
                  icon={<ExperimentOutlined />}
                  onClick={() => openTest()}
                >
                  测试 Payload
                </Button>
              ) : null}
              {canCreateIntegration ? (
                <Button
                  disabled={blocked}
                  icon={<PlusOutlined />}
                  type="primary"
                  onClick={() => openEditor(null)}
                >
                  新建集成
                </Button>
              ) : null}
            </ManagementTableToolbar>
          ) : null
        }
        columnSettingIconOnly
        columnSettingPlacement="header"
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={integrationsQuery.data ?? []}
        empty={<ManagementState bordered={false} compact description="暂无告警集成" />}
        rowKey="id"
        loading={integrationsQuery.isLoading}
        pageSize={20}
        scroll={{ x: 'max-content' }}
      />

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={editingIntegration ? '编辑告警集成' : '新建告警集成'}
        open={editorOpen && !denied}
        closable={!writeBlocked}
        keyboard={!writeBlocked}
        mask={{ closable: false }}
        onCancel={closeEditor}
        footer={
          <Space>
            <Button disabled={writeBlocked} onClick={closeEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={blocked}
              onClick={() => editorForm.submit()}
              loading={createIntegration.isPending || updateIntegration.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={820}
      >
        {createIntegration.feedback}
        {updateIntegration.feedback}
        <Form
          layout="vertical"
          disabled={blocked}
          form={editorForm}
          onFinish={submitEditor}
          initialValues={{ integrationType: 'alertmanager_v1', enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <div className="soha-observability-form-grid">
            <Form.Item name="id" label="集成 ID">
              <Input disabled={Boolean(editingIntegration)} placeholder="留空自动生成" />
            </Form.Item>
            <Form.Item name="integrationType" label="来源类型" rules={[{ required: true }]}>
              <Select options={alertIntegrationTypeOptions} />
            </Form.Item>
          </div>
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Form.Item name="token" label="Token">
            <Input.Password placeholder={editingIntegration ? '留空则不轮换' : '留空自动生成'} />
          </Form.Item>
          <Form.Item name="labelMapping" label="标签映射(JSON)">
            <Input.TextArea rows={5} />
          </Form.Item>
          <Form.Item name="dedupeConfig" label="去重配置(JSON)">
            <Input.TextArea rows={4} />
          </Form.Item>
          <Form.Item name="enabled" label="启用" valuePropName="checked">
            <Switch />
          </Form.Item>
        </Form>
      </Modal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="测试 Payload"
        open={testOpen && !denied && canTestIntegration && !queryAccessDenied(testIntegration)}
        closable={!testIntegration.isPending}
        keyboard={!testIntegration.isPending}
        mask={{ closable: false }}
        onCancel={closeTest}
        footer={
          <Space>
            <Button disabled={testIntegration.isPending} onClick={closeTest}>
              关闭
            </Button>
            <Button
              type="primary"
              disabled={!canTestIntegration || testIntegration.isPending || denied}
              onClick={() => testForm.submit()}
              loading={testIntegration.isPending}
            >
              归一化测试
            </Button>
          </Space>
        }
        destroyOnHidden
        width={920}
      >
        {testIntegration.feedback}
        <Form
          layout="vertical"
          disabled={testIntegration.isPending || denied || !canTestIntegration}
          onValuesChange={() => {
            setTestResult(null)
            testIntegration.reset()
          }}
          form={testForm}
          onFinish={submitTest}
          initialValues={{ integrationType: 'alertmanager_v1' }}
        >
          <Form.Item name="integrationType" label="来源类型" rules={[{ required: true }]}>
            <Select
              options={alertIntegrationTypeOptions}
              onChange={(value) =>
                testForm.setFieldValue('payload', alertIntegrationSamplePayload(String(value)))
              }
            />
          </Form.Item>
          <div className="soha-observability-form-grid">
            <Form.Item name="labelMapping" label="标签映射(JSON)">
              <Input.TextArea rows={5} />
            </Form.Item>
            <Form.Item name="dedupeConfig" label="去重配置(JSON)">
              <Input.TextArea rows={5} />
            </Form.Item>
          </div>
          <Form.Item name="payload" label="Payload(JSON)" rules={[{ required: true }]}>
            <Input.TextArea rows={10} />
          </Form.Item>
        </Form>
        {testResult ? (
          <Input.TextArea
            style={{ marginTop: 16 }}
            rows={8}
            readOnly
            value={prettyObservabilityJson(testResult)}
          />
        ) : null}
      </Modal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="集成 Token"
        open={Boolean(createdSecret) && !denied && (canCreateIntegration || canUpdateIntegration)}
        onCancel={() => setCreatedSecret(null)}
        footer={
          <Button type="primary" onClick={() => setCreatedSecret(null)}>
            完成
          </Button>
        }
        destroyOnHidden
      >
        <Space orientation="vertical" style={{ width: '100%' }} size={12}>
          <Input readOnly value={createdSecret?.token || ''} />
          <Button
            icon={<CopyOutlined />}
            onClick={() => copyText(createdSecret?.token || '', 'Token')}
          >
            复制 Token
          </Button>
          <Text type="secondary">关闭后仅显示 Token 摘要。</Text>
        </Space>
      </Modal>
    </div>
  )
}
