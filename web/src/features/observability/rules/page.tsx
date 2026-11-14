import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { StepForm } from '@/components/step-form'
import { StepFormModal } from '@/components/step-form-modal'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EditOutlined, PlayCircleOutlined, PlusOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import {
  Alert,
  App,
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Segmented,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import { useEffect, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import '../observability-pages.css'
import {
  alertRuleConditionSummary,
  alertRuleDashboardDraft,
  alertRuleFormValues,
  buildAlertRulePayload,
  prettyObservabilityJson,
} from './model'
import { observabilityRuleMutations } from './mutations'
import { observabilityRuleQueries } from './queries'
import type {
  AlertRule,
  AlertRuleDatasourceSelector,
  AlertRuleFormValues,
  AlertRuleTestResult,
} from './types'

const { Text } = Typography

export function AlertRulesPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canManageRule = hasPermission(permissionSnapshot, 'observe.alert-rules.manage')
  const canTestRule = hasPermission(permissionSnapshot, 'observe.alert-rules.view')
  const [searchParams] = useSearchParams()
  const dashboardDraftOpened = useRef(false)
  const [form] = Form.useForm<AlertRuleFormValues>()
  const [step, setStep] = useState(0)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<AlertRule | null>(null)
  const [testOpen, setTestOpen] = useState(false)
  const [testResult, setTestResult] = useState<AlertRuleTestResult | null>(null)
  const [runsOpen, setRunsOpen] = useState(false)
  const [selectedRuleId, setSelectedRuleId] = useState('')
  const editorMode = Form.useWatch('mode', form) ?? 'simple'

  const rulesQuery = useQuery(observabilityRuleQueries.list())
  const notificationPoliciesQuery = useQuery(observabilityRuleQueries.notificationPolicies())
  const healingPoliciesQuery = useQuery(observabilityRuleQueries.healingPolicies())
  const ruleRunsQuery = useQuery({
    ...observabilityRuleQueries.runs(selectedRuleId),
    enabled: runsOpen && selectedRuleId !== '',
  })
  const createMutation = useOperationMutation(observabilityRuleMutations.create(queryClient), {
    label: ['创建告警规则', 'Create alert rule'],
    target: (input) => input.name,
    receipt: () => ({ message: '告警规则记录已创建' }),
  })
  const updateMutation = useOperationMutation(observabilityRuleMutations.update(queryClient), {
    label: ['更新告警规则', 'Update alert rule'],
    target: ({ id }) => id,
    receipt: () => ({ message: '告警规则记录已更新' }),
  })
  const testMutation = useOperationMutation(observabilityRuleMutations.test(), {
    label: ['测试告警规则', 'Test alert rule'],
    target: ({ id }) => id,
    readOnly: true,
    resultError: (result) =>
      result.state === 'error' ? new Error(result.summary || '规则测试返回错误') : undefined,
    receipt: () => ({ message: '规则测试记录已返回', evidence: '本次测试未保存规则或执行处置。' }),
  })
  const denied = queryAccessDenied(rulesQuery)
  const writeBlocked = createMutation.blocked || updateMutation.blocked
  const blocked =
    writeBlocked ||
    denied ||
    !readableQueryData(notificationPoliciesQuery) ||
    !readableQueryData(healingPoliciesQuery) ||
    testMutation.isPending
  function cancelEditor() {
    if (writeBlocked || testMutation.isPending) return
    setOpen(false)
    setEditing(null)
    form.resetFields()
  }
  useEffect(() => {
    if (denied || queryAccessDenied(createMutation) || queryAccessDenied(updateMutation)) {
      setOpen(false)
      setEditing(null)
      form.resetFields()
      setTestOpen(false)
      setTestResult(null)
    }
  }, [denied, createMutation.error, updateMutation.error, form])

  useEffect(() => {
    if (dashboardDraftOpened.current || !canManageRule || denied) return
    const draft = alertRuleDashboardDraft(searchParams)
    if (!draft) return
    dashboardDraftOpened.current = true
    setEditing(null)
    setStep(0)
    form.setFieldsValue(draft)
    setOpen(true)
  }, [canManageRule, denied, form, searchParams])

  function openEditor(record: AlertRule | null) {
    if (writeBlocked || denied || testMutation.isPending) return
    createMutation.reset()
    updateMutation.reset()
    testMutation.reset()
    setTestResult(null)
    form.resetFields()
    setStep(0)
    setEditing(record)
    setOpen(true)
    form.setFieldsValue(alertRuleFormValues(record))
  }

  function closeEditor() {
    form.resetFields()
    setOpen(false)
    setEditing(null)
  }

  function submit(values: AlertRuleFormValues) {
    if (blocked || !canManageRule) return
    try {
      const payload = buildAlertRulePayload(values)
      if (editing) {
        updateMutation.mutate({ id: editing.id, payload }, { onSuccess: () => closeEditor() })
      } else {
        createMutation.mutate(payload, {
          onSuccess: () => closeEditor(),
        })
      }
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function testRule(record: AlertRule, values = buildAlertRulePayload(record)) {
    if (testMutation.isPending || denied || !canTestRule) return
    setTestResult(null)
    setTestOpen(true)
    testMutation.reset()
    testMutation.mutate(
      { id: record.id, payload: values },
      {
        onSuccess: (result) => {
          setTestResult(result)
          setTestOpen(true)
        },
      },
    )
  }

  function testEditor() {
    try {
      const payload = buildAlertRulePayload(form.getFieldsValue())
      testRule(editing ?? ({ id: 'preview' } as AlertRule), payload)
    } catch (error) {
      message.error(error instanceof Error ? error.message : '规则测试失败')
    }
  }

  const columns: TableProps<AlertRule>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '类型',
      dataIndex: 'ruleType',
      render: (value: string) => <MetadataTag label={value} />,
    },
    {
      title: '数据源',
      dataIndex: 'datasourceSelector',
      render: (value: AlertRuleDatasourceSelector) => (
        <Text code>{prettyObservabilityJson(value)}</Text>
      ),
    },
    {
      title: '通知策略',
      dataIndex: 'notificationPolicyId',
      render: (value: string) => value || '-',
    },
    {
      title: '自愈策略',
      dataIndex: 'healingPolicyIds',
      render: (value: string[]) => (
        <Space wrap>
          {(value ?? []).map((item) => (
            <MetadataTag key={item} label={item} />
          ))}
        </Space>
      ),
    },
    { title: '持续(s)', dataIndex: 'forSeconds' },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: '操作',
      key: 'actions',
      dataIndex: 'id',
      render: (_: string, record) => (
        <Space className="soha-row-action-icons" size={2}>
          <ManagementIconButton
            aria-label="测试告警规则"
            disabled={!canTestRule || testMutation.isPending || denied}
            size="small"
            tooltip="测试"
            icon={<PlayCircleOutlined />}
            onClick={() => testRule(record)}
          />
          <ManagementIconButton
            aria-label="查看运行记录"
            size="small"
            tooltip="运行记录"
            icon={<ReloadOutlined />}
            onClick={() => {
              setSelectedRuleId(record.id)
              setRunsOpen(true)
            }}
          />
          {canManageRule ? (
            <ManagementIconButton
              aria-label="编辑告警规则"
              disabled={writeBlocked || denied || testMutation.isPending}
              size="small"
              tooltip="编辑"
              icon={<EditOutlined />}
              onClick={() => openEditor(record)}
            />
          ) : null}
        </Space>
      ),
    },
  ]

  return (
    <div className="soha-page">
      {!open ? (
        <>
          {createMutation.feedback}
          {updateMutation.feedback}
        </>
      ) : null}
      <h1 className="soha-observability-page-heading">告警规则</h1>
      <AdminTable
        enableDensity
        error={rulesQuery.error}
        hasReadData={rulesQuery.data !== undefined}
        refreshing={rulesQuery.isFetching}
        onRefresh={() => void rulesQuery.refetch()}
        columnSettingPlacement="header"
        columnSettingIconOnly
        headerExtra={
          canManageRule ? (
            <Button
              disabled={writeBlocked || denied || testMutation.isPending}
              icon={<PlusOutlined />}
              type="primary"
              onClick={() => openEditor(null)}
            >
              新建规则
            </Button>
          ) : null
        }
        shellClassName="soha-management-table-shell"
        columns={columns}
        dataSource={rulesQuery.data ?? []}
        rowKey="id"
        loading={rulesQuery.isLoading}
      />

      <StepFormModal
        title={editing ? '编辑告警规则' : '新建告警规则'}
        open={open && !denied}
        closeDisabled={writeBlocked || testMutation.isPending}
        feedback={
          <>
            {createMutation.feedback}
            {updateMutation.feedback}
            <ReadFeedback label="通知策略选项" query={notificationPoliciesQuery} />
            <ReadFeedback label="自愈策略选项" query={healingPoliciesQuery} />
          </>
        }
        onClose={cancelEditor}
        width={920}
      >
        <StepForm
          onCancel={cancelEditor}
          contentMaxWidth="100%"
          disabled={blocked}
          form={form}
          onValuesChange={() => {
            setTestResult(null)
            testMutation.reset()
          }}
          current={step}
          onCurrentChange={setStep}
          onFinish={submit}
          loading={createMutation.isPending || updateMutation.isPending}
          steps={[
            {
              title: '规则与条件',
              fieldNames:
                editorMode === 'simple'
                  ? [
                      'name',
                      'metricKey',
                      'reducer',
                      'operator',
                      'thresholdValue',
                      'windowMinutes',
                      'severity',
                    ]
                  : [
                      'name',
                      'ruleType',
                      'datasourceSelector',
                      'querySpec',
                      'thresholdSpec',
                      'labels',
                      'annotations',
                    ],
              children: (
                <>
                  {' '}
                  <Form.Item name="mode" label="配置模式">
                    <Segmented
                      disabled={blocked}
                      block
                      className="soha-form-segmented"
                      options={[
                        { value: 'simple', label: '普通' },
                        { value: 'advanced', label: '高级' },
                      ]}
                    />
                  </Form.Item>
                  <Form.Item
                    name="name"
                    label="名称"
                    rules={[{ required: true, message: '请输入规则名称' }]}
                  >
                    <Input />
                  </Form.Item>
                  {editorMode === 'simple' ? (
                    <>
                      <div className="soha-observability-form-grid">
                        <Form.Item name="metricKey" label="监控指标" rules={[{ required: true }]}>
                          <Select
                            options={[
                              { value: 'cpu_usage', label: 'CPU 使用率' },
                              { value: 'memory_usage', label: '内存使用率' },
                              { value: 'restart_rate', label: '重启次数' },
                              { value: 'error_rate', label: '错误率' },
                              { value: 'latency_p95', label: 'P95 延迟' },
                            ]}
                          />
                        </Form.Item>
                        <Form.Item name="reducer" label="取值" rules={[{ required: true }]}>
                          <Select
                            options={[
                              { value: 'latest', label: '最新值' },
                              { value: 'average', label: '平均值' },
                              { value: 'max', label: '最大值' },
                              { value: 'min', label: '最小值' },
                              { value: 'sum', label: '总和' },
                              { value: 'count', label: '样本数' },
                            ]}
                          />
                        </Form.Item>
                      </div>
                      <div className="soha-observability-form-grid">
                        <Form.Item name="operator" label="条件" rules={[{ required: true }]}>
                          <Select
                            options={[
                              { value: 'gt', label: '大于' },
                              { value: 'gte', label: '大于等于' },
                              { value: 'lt', label: '小于' },
                              { value: 'lte', label: '小于等于' },
                              { value: 'eq', label: '等于' },
                            ]}
                          />
                        </Form.Item>
                        <Form.Item name="thresholdValue" label="阈值" rules={[{ required: true }]}>
                          <InputNumber style={{ width: '100%' }} />
                        </Form.Item>
                        <Form.Item
                          name="windowMinutes"
                          label="查询窗口(分钟)"
                          rules={[{ required: true }]}
                        >
                          <InputNumber min={1} max={1440} style={{ width: '100%' }} />
                        </Form.Item>
                      </div>
                      <div className="soha-observability-form-grid">
                        <Form.Item name="clusterId" label="集群">
                          <Input />
                        </Form.Item>
                        <Form.Item name="namespace" label="命名空间">
                          <Input />
                        </Form.Item>
                        <Form.Item name="workload" label="工作负载">
                          <Input />
                        </Form.Item>
                      </div>
                      <div className="soha-observability-form-grid">
                        <Form.Item name="severity" label="严重级别" rules={[{ required: true }]}>
                          <Select
                            options={[
                              { value: 'critical', label: '严重' },
                              { value: 'warning', label: '警告' },
                              { value: 'info', label: '提示' },
                            ]}
                          />
                        </Form.Item>
                        <Form.Item name="summary" label="告警摘要">
                          <Input />
                        </Form.Item>
                      </div>
                      <Form.Item noStyle shouldUpdate>
                        {({ getFieldsValue }) => (
                          <Alert
                            description={alertRuleConditionSummary(getFieldsValue(true))}
                            showIcon
                            title="条件说明"
                            type="info"
                          />
                        )}
                      </Form.Item>
                    </>
                  ) : (
                    <>
                      <Form.Item name="ruleType" label="规则类型" rules={[{ required: true }]}>
                        <Select
                          options={[
                            { value: 'metrics', label: 'Metrics' },
                            { value: 'logs', label: 'Logs' },
                            { value: 'traces', label: 'Traces' },
                            { value: 'external_passthrough', label: 'External passthrough' },
                          ]}
                        />
                      </Form.Item>
                      <Form.Item
                        name="datasourceSelector"
                        label="数据源选择器(JSON)"
                        rules={[{ required: true }]}
                      >
                        <Input.TextArea rows={3} />
                      </Form.Item>
                      <Form.Item
                        name="querySpec"
                        label="查询定义(JSON)"
                        rules={[{ required: true }]}
                      >
                        <Input.TextArea rows={4} />
                      </Form.Item>
                      <Form.Item
                        name="thresholdSpec"
                        label="阈值定义(JSON)"
                        rules={[{ required: true }]}
                      >
                        <Input.TextArea rows={3} />
                      </Form.Item>
                      <Form.Item name="groupBy" label="分组标签(逗号分隔)">
                        <Input />
                      </Form.Item>
                      <Form.Item name="labels" label="事件标签(JSON)" rules={[{ required: true }]}>
                        <Input.TextArea rows={3} />
                      </Form.Item>
                      <Form.Item
                        name="annotations"
                        label="事件注释(JSON)"
                        rules={[{ required: true }]}
                      >
                        <Input.TextArea rows={3} />
                      </Form.Item>
                    </>
                  )}
                </>
              ),
            },
            {
              title: '通知与自愈',
              children: (
                <>
                  <div className="soha-observability-form-grid">
                    <Form.Item name="forSeconds" label="持续时间(s)">
                      <InputNumber min={0} style={{ width: '100%' }} />
                    </Form.Item>
                    <Form.Item name="notificationPolicyId" label="通知策略">
                      <Select
                        allowClear
                        options={(readableQueryData(notificationPoliciesQuery) ?? []).map(
                          (item) => ({
                            value: item.id,
                            label: item.name,
                          }),
                        )}
                      />
                    </Form.Item>
                  </div>
                  <Form.Item name="healingPolicyIds" label="自愈策略">
                    <Select
                      mode="multiple"
                      allowClear
                      options={(readableQueryData(healingPoliciesQuery) ?? []).map((item) => ({
                        value: item.id,
                        label: item.name,
                      }))}
                    />
                  </Form.Item>
                  <Form.Item name="enabled" label="启用" valuePropName="checked">
                    <Switch />
                  </Form.Item>

                  {canTestRule ? (
                    <Button
                      icon={<PlayCircleOutlined />}
                      disabled={blocked}
                      loading={testMutation.isPending}
                      onClick={testEditor}
                    >
                      测试规则
                    </Button>
                  ) : null}
                </>
              ),
            },
          ]}
        />
      </StepFormModal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="规则测试结果"
        open={testOpen && !denied}
        closable={!testMutation.isPending}
        keyboard={!testMutation.isPending}
        mask={{ closable: false }}
        onCancel={() => {
          if (!testMutation.isPending) {
            setTestOpen(false)
            setTestResult(null)
            testMutation.reset()
          }
        }}
        footer={null}
        width={920}
        destroyOnHidden
      >
        {testMutation.feedback}
        {(testMutation.error || testResult?.state === 'error') && testMutation.variables ? (
          <Button
            onClick={() => {
              setTestResult(null)
              testMutation.mutate(testMutation.variables!, { onSuccess: setTestResult })
            }}
          >
            重试测试
          </Button>
        ) : null}
        {testResult ? (
          <Space orientation="vertical" style={{ width: '100%' }} size={16}>
            <Alert
              type={
                testResult?.state === 'error'
                  ? 'error'
                  : testResult?.matched
                    ? 'warning'
                    : 'success'
              }
              showIcon
              title={
                <Space>
                  <StatusTag value={String(testResult?.state ?? 'unknown')} />
                  {String(testResult?.summary ?? '-')}
                </Space>
              }
            />
            {(
              [
                ['errors', testResult?.errors],
                ['dataSources', testResult?.dataSources],
                ['samples', testResult?.samples],
                ['notificationPreview', testResult?.notificationPreview],
                ['querySnapshot', testResult?.querySnapshot],
              ] as const
            ).map(([key, value]) => (
              <Card size="small" title={key} key={key}>
                <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                  {JSON.stringify(value ?? '-', null, 2)}
                </pre>
              </Card>
            ))}
          </Space>
        ) : null}
      </Modal>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title="最近运行记录"
        open={runsOpen}
        onCancel={() => setRunsOpen(false)}
        footer={null}
        width={920}
        destroyOnHidden
      >
        <AdminTable
          enableDensity
          error={ruleRunsQuery.error}
          hasReadData={ruleRunsQuery.data !== undefined}
          refreshing={ruleRunsQuery.isFetching}
          onRefresh={() => void ruleRunsQuery.refetch()}
          columnSettingPlacement="header"
          columnSettingIconOnly
          columns={[
            { title: '运行ID', dataIndex: 'id' },
            {
              title: '状态',
              dataIndex: 'status',
              render: (value: string) => <StatusTag value={value} />,
            },
            {
              title: '命中',
              dataIndex: 'matched',
              render: (value: boolean) => (
                <BooleanTag value={value} trueLabel="命中" falseLabel="未命中" />
              ),
            },
            { title: '耗时(ms)', dataIndex: 'durationMs' },
            { title: '摘要', dataIndex: 'summary' },
            { title: '错误', dataIndex: 'error', render: (value: string) => value || '-' },
            { title: '时间', dataIndex: 'createdAt', render: formatDateTime },
          ]}
          dataSource={ruleRunsQuery.data ?? []}
          rowKey="id"
          loading={ruleRunsQuery.isLoading}
          pagination={{ pageSize: 10 }}
        />
      </Modal>
    </div>
  )
}
