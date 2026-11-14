import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { BooleanTag, MetadataTag, StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import {
  BellOutlined,
  EditOutlined,
  EyeOutlined,
  NotificationOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableProps } from 'antd'
import {
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
  Typography,
} from 'antd'
import { useEffect, useMemo, useState } from 'react'
import '../observability-pages.css'
import {
  buildNotificationChannelPayload,
  buildNotificationPolicyPayload,
  buildNotificationRoutePayload,
  buildNotificationSilencePayload,
  buildNotificationTemplatePayload,
  formatNotificationSilenceStatus,
  prettyNotificationJson,
  resolveChannelEndpoint,
  shortNotificationJson,
  stringifyNotificationMatchers,
} from './model'
import { observabilityNotificationMutations } from './mutations'
import { observabilityNotificationQueries } from './queries'
import type {
  NotificationChannel,
  NotificationChannelConfig,
  NotificationChannelFormValues,
  NotificationMatchers,
  NotificationPolicy,
  NotificationPolicyFormValues,
  NotificationRoute,
  NotificationRouteFormValues,
  NotificationSilence,
  NotificationSilenceFormValues,
  NotificationTemplate,
  NotificationTemplateFormValues,
} from './types'

const { Text } = Typography

export function NotificationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateNotification = hasPermission(permissionSnapshot, 'observe.notifications.create')
  const canUpdateNotification = hasPermission(permissionSnapshot, 'observe.notifications.update')
  const [policyForm] = Form.useForm<NotificationPolicyFormValues>()
  const [templateForm] = Form.useForm<NotificationTemplateFormValues>()
  const [channelForm] = Form.useForm<NotificationChannelFormValues>()
  const [routeForm] = Form.useForm<NotificationRouteFormValues>()
  const [silenceForm] = Form.useForm<NotificationSilenceFormValues>()
  const [policyOpen, setPolicyOpen] = useState(false)
  const [templateOpen, setTemplateOpen] = useState(false)
  const [channelOpen, setChannelOpen] = useState(false)
  const [routeOpen, setRouteOpen] = useState(false)
  const [silenceOpen, setSilenceOpen] = useState(false)
  const [editingPolicy, setEditingPolicy] = useState<NotificationPolicy | null>(null)
  const [editingTemplate, setEditingTemplate] = useState<NotificationTemplate | null>(null)
  const [editingChannel, setEditingChannel] = useState<NotificationChannel | null>(null)
  const [editingRoute, setEditingRoute] = useState<NotificationRoute | null>(null)
  const [editingSilence, setEditingSilence] = useState<NotificationSilence | null>(null)
  const [previewOpen, setPreviewOpen] = useState(false)
  const [previewPolicy, setPreviewPolicy] = useState<NotificationPolicy | null>(null)
  const [previewEventId, setPreviewEventId] = useState('')

  const channelsQuery = useQuery(observabilityNotificationQueries.channels())
  const alertEventsQuery = useQuery(observabilityNotificationQueries.previewEvents())
  const policiesQuery = useQuery(observabilityNotificationQueries.policies())
  const templatesQuery = useQuery(observabilityNotificationQueries.templates())
  const routesQuery = useQuery(observabilityNotificationQueries.routes())
  const silencesQuery = useQuery(observabilityNotificationQueries.silences())
  const oncallSchedulesQuery = useQuery(observabilityNotificationQueries.oncallSchedules())
  const oncallPoliciesQuery = useQuery(observabilityNotificationQueries.oncallPolicies())

  const createPolicy = useOperationMutation(
    observabilityNotificationMutations.createPolicy(queryClient),
    {
      label: ['创建通知策略', 'Create notification policy'],
      target: (input) => input.name,
      receipt: () => ({ message: '通知策略记录已保存' }),
    },
  )
  const updatePolicy = useOperationMutation(
    observabilityNotificationMutations.updatePolicy(queryClient),
    {
      label: ['更新通知策略', 'Update notification policy'],
      target: ({ id }) => id,
      receipt: () => ({ message: '通知策略记录已保存' }),
    },
  )
  const createTemplate = useOperationMutation(
    observabilityNotificationMutations.createTemplate(queryClient),
    {
      label: ['创建通知模板', 'Create notification template'],
      target: (input) => input.name,
      receipt: () => ({ message: '通知模板记录已保存' }),
    },
  )
  const updateTemplate = useOperationMutation(
    observabilityNotificationMutations.updateTemplate(queryClient),
    {
      label: ['更新通知模板', 'Update notification template'],
      target: ({ id }) => id,
      receipt: () => ({ message: '通知模板记录已保存' }),
    },
  )
  const createChannel = useOperationMutation(
    observabilityNotificationMutations.createChannel(queryClient),
    {
      label: ['创建通知渠道', 'Create notification channel'],
      target: (input) => input.name,
      receipt: () => ({ message: '通知渠道记录已保存' }),
    },
  )
  const updateChannel = useOperationMutation(
    observabilityNotificationMutations.updateChannel(queryClient),
    {
      label: ['更新通知渠道', 'Update notification channel'],
      target: ({ id }) => id,
      receipt: () => ({ message: '通知渠道记录已保存' }),
    },
  )
  const createRoute = useOperationMutation(
    observabilityNotificationMutations.createRoute(queryClient),
    {
      label: ['创建路由规则', 'Create notification route'],
      target: (input) => input.name,
      receipt: () => ({ message: '路由规则记录已保存' }),
    },
  )
  const updateRoute = useOperationMutation(
    observabilityNotificationMutations.updateRoute(queryClient),
    {
      label: ['更新路由规则', 'Update notification route'],
      target: ({ id }) => id,
      receipt: () => ({ message: '路由规则记录已保存' }),
    },
  )
  const createSilence = useOperationMutation(
    observabilityNotificationMutations.createSilence(queryClient),
    {
      label: ['创建静默规则', 'Create notification silence'],
      target: (input) => input.name,
      receipt: () => ({ message: '静默规则记录已保存' }),
    },
  )
  const updateSilence = useOperationMutation(
    observabilityNotificationMutations.updateSilence(queryClient),
    {
      label: ['更新静默规则', 'Update notification silence'],
      target: ({ id }) => id,
      receipt: () => ({ message: '静默规则记录已保存' }),
    },
  )
  const previewQuery = useQuery(
    observabilityNotificationQueries.preview(
      previewPolicy?.id ?? '',
      previewEventId,
      previewOpen && !queryAccessDenied(policiesQuery) && !queryAccessDenied(alertEventsQuery),
    ),
  )
  const policyDenied = queryAccessDenied(policiesQuery)
  const policyWriteBlocked = createPolicy.blocked || updatePolicy.blocked
  const policyBlocked =
    policyWriteBlocked ||
    policyDenied ||
    !readableQueryData(channelsQuery) ||
    !readableQueryData(oncallSchedulesQuery) ||
    !readableQueryData(oncallPoliciesQuery)
  function closePolicyEditor() {
    if (policyWriteBlocked) return
    setPolicyOpen(false)
    setEditingPolicy(null)
    policyForm.resetFields()
  }
  useEffect(() => {
    if (policyDenied || queryAccessDenied(createPolicy) || queryAccessDenied(updatePolicy)) {
      setPolicyOpen(false)
      setEditingPolicy(null)
      policyForm.resetFields()
    }
  }, [policyDenied, createPolicy.error, updatePolicy.error, policyForm])
  const templateDenied = queryAccessDenied(templatesQuery)
  const templateWriteBlocked = createTemplate.blocked || updateTemplate.blocked
  const templateBlocked = templateWriteBlocked || templateDenied
  function closeTemplateEditor() {
    if (templateWriteBlocked) return
    setTemplateOpen(false)
    setEditingTemplate(null)
    templateForm.resetFields()
  }
  useEffect(() => {
    if (templateDenied || queryAccessDenied(createTemplate) || queryAccessDenied(updateTemplate)) {
      setTemplateOpen(false)
      setEditingTemplate(null)
      templateForm.resetFields()
    }
  }, [templateDenied, createTemplate.error, updateTemplate.error, templateForm])
  const channelDenied = queryAccessDenied(channelsQuery)
  const channelWriteBlocked = createChannel.blocked || updateChannel.blocked
  const channelBlocked = channelWriteBlocked || channelDenied
  function closeChannelEditor() {
    if (channelWriteBlocked) return
    setChannelOpen(false)
    setEditingChannel(null)
    channelForm.resetFields()
  }
  useEffect(() => {
    if (channelDenied || queryAccessDenied(createChannel) || queryAccessDenied(updateChannel)) {
      setChannelOpen(false)
      setEditingChannel(null)
      channelForm.resetFields()
    }
  }, [channelDenied, createChannel.error, updateChannel.error, channelForm])
  const routeDenied = queryAccessDenied(routesQuery)
  const routeWriteBlocked = createRoute.blocked || updateRoute.blocked
  const routeBlocked = routeWriteBlocked || routeDenied || !readableQueryData(channelsQuery)
  function closeRouteEditor() {
    if (routeWriteBlocked) return
    setRouteOpen(false)
    setEditingRoute(null)
    routeForm.resetFields()
  }
  useEffect(() => {
    if (routeDenied || queryAccessDenied(createRoute) || queryAccessDenied(updateRoute)) {
      setRouteOpen(false)
      setEditingRoute(null)
      routeForm.resetFields()
    }
  }, [routeDenied, createRoute.error, updateRoute.error, routeForm])
  const silenceDenied = queryAccessDenied(silencesQuery)
  const silenceWriteBlocked = createSilence.blocked || updateSilence.blocked
  const silenceBlocked = silenceWriteBlocked || silenceDenied
  function closeSilenceEditor() {
    if (silenceWriteBlocked) return
    setSilenceOpen(false)
    setEditingSilence(null)
    silenceForm.resetFields()
  }
  useEffect(() => {
    if (silenceDenied || queryAccessDenied(createSilence) || queryAccessDenied(updateSilence)) {
      setSilenceOpen(false)
      setEditingSilence(null)
      silenceForm.resetFields()
    }
  }, [silenceDenied, createSilence.error, updateSilence.error, silenceForm])

  const channelNamesById = useMemo(
    () =>
      Object.fromEntries(
        (readableQueryData(channelsQuery) ?? []).map((item) => [item.id, item.name]),
      ),
    [channelsQuery.data, channelsQuery.error],
  )
  const oncallOptions = useMemo(
    () => [
      ...(readableQueryData(oncallSchedulesQuery) ?? []).map((item) => ({
        value: item.id,
        label: `值班表 · ${item.name}`,
      })),
      ...(readableQueryData(oncallPoliciesQuery) ?? []).map((item) => ({
        value: item.id,
        label: `升级策略 · ${item.name}`,
      })),
    ],
    [
      oncallPoliciesQuery.data,
      oncallPoliciesQuery.error,
      oncallSchedulesQuery.data,
      oncallSchedulesQuery.error,
    ],
  )
  const channelOptions = useMemo(
    () =>
      (readableQueryData(channelsQuery) ?? []).map((item) => ({
        value: item.id,
        label: item.name,
      })),
    [channelsQuery.data, channelsQuery.error],
  )

  function openPolicyEditor(record: NotificationPolicy | null) {
    if (policyWriteBlocked || policyDenied) return
    createPolicy.reset()
    updatePolicy.reset()
    policyForm.resetFields()
    setEditingPolicy(record)
    setPolicyOpen(true)
    policyForm.setFieldsValue(
      record
        ? {
            ...record,
            matchers: prettyNotificationJson(record.matchers ?? {}),
            processorChain: record.processorChain ?? [],
            channelRefs: record.channelRefs ?? [],
          }
        : {
            name: '',
            matchers: '{}',
            processorChain: ['template_render', 'webhook_update'],
            channelRefs: [],
            oncallRef: '',
            sendResolved: false,
            cooldownSeconds: 0,
            enabled: true,
          },
    )
  }

  function openTemplateEditor(record: NotificationTemplate | null) {
    if (templateWriteBlocked || templateDenied) return
    createTemplate.reset()
    updateTemplate.reset()
    templateForm.resetFields()
    setEditingTemplate(record)
    setTemplateOpen(true)
    templateForm.setFieldsValue(
      record
        ? {
            ...record,
            headers: prettyNotificationJson(record.headers ?? {}),
            queryParams: prettyNotificationJson(record.queryParams ?? {}),
            samplePayload: prettyNotificationJson(record.samplePayload ?? {}),
          }
        : {
            name: '',
            templateType: 'generic_json',
            contentType: 'application/json',
            bodyTemplate: '{"alert":"{{ .alert.title }}"}',
            headers: '{}',
            queryParams: '{}',
            samplePayload: '{}',
            enabled: true,
          },
    )
  }

  function openChannelEditor(record: NotificationChannel | null) {
    if (channelWriteBlocked || channelDenied) return
    createChannel.reset()
    updateChannel.reset()
    channelForm.resetFields()
    setEditingChannel(record)
    setChannelOpen(true)
    channelForm.setFieldsValue(
      record
        ? { ...record, config: prettyNotificationJson(record.config ?? {}) }
        : {
            name: '',
            channelType: 'webhook',
            config: '{\n  "url": "https://example.com/webhook"\n}',
            enabled: true,
          },
    )
  }

  function openRouteEditor(record: NotificationRoute | null) {
    if (routeWriteBlocked || routeDenied) return
    createRoute.reset()
    updateRoute.reset()
    routeForm.resetFields()
    setEditingRoute(record)
    setRouteOpen(true)
    routeForm.setFieldsValue(
      record
        ? {
            ...record,
            matchers: prettyNotificationJson(record.matchers ?? {}),
            channelIds: record.channelIds ?? [],
          }
        : {
            name: '',
            matchers: '{\n  "severity": "critical"\n}',
            channelIds: [],
            enabled: true,
          },
    )
  }

  function openSilenceEditor(record: NotificationSilence | null) {
    if (silenceWriteBlocked || silenceDenied) return
    createSilence.reset()
    updateSilence.reset()
    silenceForm.resetFields()
    setEditingSilence(record)
    setSilenceOpen(true)
    const now = Date.now()
    silenceForm.setFieldsValue(
      record
        ? { ...record, matchers: prettyNotificationJson(record.matchers ?? {}) }
        : {
            name: '',
            matchers: '{\n  "severity": "warning"\n}',
            reason: '',
            startsAt: new Date(now).toISOString(),
            endsAt: new Date(now + 60 * 60 * 1000).toISOString(),
            enabled: true,
          },
    )
  }

  function closePolicy() {
    setPolicyOpen(false)
    setEditingPolicy(null)
    policyForm.resetFields()
  }

  function submitPolicy(values: NotificationPolicyFormValues) {
    if (policyBlocked || !(editingPolicy ? canUpdateNotification : canCreateNotification)) return
    try {
      const payload = buildNotificationPolicyPayload(values)
      if (editingPolicy?.id) {
        updatePolicy.mutate({ id: editingPolicy.id, payload }, { onSuccess: () => closePolicy() })
        return
      }
      createPolicy.mutate(payload, { onSuccess: () => closePolicy() })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitTemplate(values: NotificationTemplateFormValues) {
    if (templateBlocked || !(editingTemplate ? canUpdateNotification : canCreateNotification))
      return
    try {
      const payload = buildNotificationTemplatePayload(values)
      const onSuccess = () => {
        setTemplateOpen(false)
        setEditingTemplate(null)
        templateForm.resetFields()
      }
      if (editingTemplate?.id) {
        updateTemplate.mutate({ id: editingTemplate.id, payload }, { onSuccess })
        return
      }
      createTemplate.mutate(payload, { onSuccess })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitChannel(values: NotificationChannelFormValues) {
    if (channelBlocked || !(editingChannel ? canUpdateNotification : canCreateNotification)) return
    try {
      const payload = buildNotificationChannelPayload(values)
      const onSuccess = () => {
        setChannelOpen(false)
        setEditingChannel(null)
        channelForm.resetFields()
      }
      if (editingChannel?.id) {
        updateChannel.mutate({ id: editingChannel.id, payload }, { onSuccess })
        return
      }
      createChannel.mutate(payload, { onSuccess })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitRoute(values: NotificationRouteFormValues) {
    if (routeBlocked || !(editingRoute ? canUpdateNotification : canCreateNotification)) return
    try {
      const payload = buildNotificationRoutePayload(values)
      const onSuccess = () => {
        setRouteOpen(false)
        setEditingRoute(null)
        routeForm.resetFields()
      }
      if (editingRoute?.id) {
        updateRoute.mutate({ id: editingRoute.id, payload }, { onSuccess })
        return
      }
      createRoute.mutate(payload, { onSuccess })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  function submitSilence(values: NotificationSilenceFormValues) {
    if (silenceBlocked || !(editingSilence ? canUpdateNotification : canCreateNotification)) return
    try {
      const payload = buildNotificationSilencePayload(values)
      const onSuccess = () => {
        setSilenceOpen(false)
        setEditingSilence(null)
        silenceForm.resetFields()
      }
      if (editingSilence?.id) {
        updateSilence.mutate({ id: editingSilence.id, payload }, { onSuccess })
        return
      }
      createSilence.mutate(payload, { onSuccess })
    } catch (error) {
      message.error(error instanceof Error ? error.message : '保存失败')
    }
  }

  const channelColumns: TableProps<NotificationChannel>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '类型',
      dataIndex: 'channelType',
      render: (value: string) => <MetadataTag label={value} />,
    },
    {
      title: 'Endpoint',
      dataIndex: 'config',
      ellipsis: true,
      render: (value: NotificationChannelConfig) => resolveChannelEndpoint(value),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '更新时间',
      dataIndex: 'updatedAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateNotification ? (
          <ManagementIconButton
            aria-label="编辑通知渠道"
            disabled={channelWriteBlocked || channelDenied}
            icon={<BellOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => openChannelEditor(record)}
          />
        ) : null,
    },
  ]

  const routeColumns: TableProps<NotificationRoute>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '匹配规则',
      dataIndex: 'matchers',
      render: (value: NotificationMatchers) => (
        <Text code>{stringifyNotificationMatchers(value)}</Text>
      ),
    },
    {
      title: '接收器',
      dataIndex: 'channelIds',
      render: (value: string[]) => {
        const items = (value ?? []).map((item) => channelNamesById[item] || item)
        return items.length > 0 ? (
          <Space wrap>
            {items.map((item) => (
              <MetadataTag key={item} label={item} />
            ))}
          </Space>
        ) : (
          '-'
        )
      },
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    {
      ...tableColumnPresets.datetime,
      title: '更新时间',
      dataIndex: 'updatedAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateNotification ? (
          <ManagementIconButton
            aria-label="编辑通知路由"
            icon={<NotificationOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => openRouteEditor(record)}
          />
        ) : null,
    },
  ]

  const silenceColumns: TableProps<NotificationSilence>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '匹配器',
      dataIndex: 'matchers',
      render: (value: NotificationMatchers) => <Text code>{shortNotificationJson(value)}</Text>,
    },
    { title: '原因', dataIndex: 'reason', render: (value: string) => value || '-' },
    {
      ...tableColumnPresets.datetime,
      title: '开始时间',
      dataIndex: 'startsAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      ...tableColumnPresets.datetime,
      title: '结束时间',
      dataIndex: 'endsAt',
      render: (value: string) => formatDateTime(value),
    },
    {
      ...tableColumnPresets.status,
      title: '状态',
      dataIndex: 'enabled',
      render: (_: boolean, record) => <StatusTag value={formatNotificationSilenceStatus(record)} />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateNotification ? (
          <ManagementIconButton
            aria-label="编辑静默规则"
            disabled={silenceWriteBlocked || silenceDenied}
            icon={<EditOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => openSilenceEditor(record)}
          />
        ) : null,
    },
  ]

  const policyColumns: TableProps<NotificationPolicy>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '处理链',
      dataIndex: 'processorChain',
      render: (value: string[]) => (
        <Space wrap>
          {(value ?? []).map((item) => (
            <MetadataTag key={item} label={item} />
          ))}
        </Space>
      ),
    },
    {
      title: '渠道',
      dataIndex: 'channelRefs',
      render: (value: string[]) => (
        <Space wrap>
          {(value ?? []).map((item) => (
            <MetadataTag key={item} label={channelNamesById[item] || item} />
          ))}
        </Space>
      ),
    },
    {
      title: 'OnCall',
      dataIndex: 'oncallRef',
      render: (value: string) =>
        oncallOptions.find((item) => item.value === value)?.label || value || '-',
    },
    {
      title: '恢复通知',
      dataIndex: 'sendResolved',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="发送" falseLabel="不发送" />,
    },
    { title: '冷却(s)', dataIndex: 'cooldownSeconds' },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) => (
        <Space className="soha-row-action-icons" size={2}>
          {canUpdateNotification ? (
            <ManagementIconButton
              aria-label="编辑通知策略"
              disabled={policyWriteBlocked || policyDenied}
              icon={<EditOutlined />}
              size="small"
              tooltip="编辑"
              onClick={() => openPolicyEditor(record)}
            />
          ) : null}
          <ManagementIconButton
            aria-label="预览通知策略"
            icon={<EyeOutlined />}
            size="small"
            tooltip="预览"
            onClick={() => {
              const firstEvent = readableQueryData(alertEventsQuery)?.[0]?.id || ''
              setPreviewPolicy(record)
              setPreviewEventId(firstEvent)
              setPreviewOpen(true)
            }}
          />
        </Space>
      ),
    },
  ]

  const templateColumns: TableProps<NotificationTemplate>['columns'] = [
    { title: '名称', dataIndex: 'name' },
    {
      title: '模板类型',
      dataIndex: 'templateType',
      render: (value: string) => <MetadataTag label={value} />,
    },
    { title: '内容类型', dataIndex: 'contentType' },
    {
      title: '启用',
      dataIndex: 'enabled',
      render: (value: boolean) => <BooleanTag value={value} trueLabel="启用" falseLabel="禁用" />,
    },
    {
      ...tableColumnPresets.action,
      title: '操作',
      dataIndex: 'id',
      render: (_: string, record) =>
        canUpdateNotification ? (
          <ManagementIconButton
            aria-label="编辑通知模板"
            disabled={templateWriteBlocked || templateDenied}
            icon={<EditOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => openTemplateEditor(record)}
          />
        ) : null,
    },
  ]

  return (
    <div className="soha-page">
      {!policyOpen ? (
        <>
          {createPolicy.feedback}
          {updatePolicy.feedback}
        </>
      ) : null}
      {!templateOpen ? (
        <>
          {createTemplate.feedback}
          {updateTemplate.feedback}
        </>
      ) : null}
      {!channelOpen ? (
        <>
          {createChannel.feedback}
          {updateChannel.feedback}
        </>
      ) : null}
      {!routeOpen ? (
        <>
          {createRoute.feedback}
          {updateRoute.feedback}
        </>
      ) : null}
      {!silenceOpen ? (
        <>
          {createSilence.feedback}
          {updateSilence.feedback}
        </>
      ) : null}
      <Tabs
        items={[
          {
            key: 'policies',
            label: '通知策略',
            children: (
              <AdminTable
                enableDensity
                error={policiesQuery.error}
                hasReadData={policiesQuery.data !== undefined}
                refreshing={policiesQuery.isFetching}
                onRefresh={() => void policiesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateNotification ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      disabled={policyWriteBlocked || policyDenied}
                      onClick={() => openPolicyEditor(null)}
                    >
                      新建策略
                    </Button>
                  ) : null
                }
                columns={policyColumns}
                dataSource={policiesQuery.data ?? []}
                rowKey="id"
                loading={policiesQuery.isLoading}
              />
            ),
          },
          {
            key: 'templates',
            label: '通知模板',
            children: (
              <AdminTable
                enableDensity
                error={templatesQuery.error}
                hasReadData={templatesQuery.data !== undefined}
                refreshing={templatesQuery.isFetching}
                onRefresh={() => void templatesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateNotification ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      disabled={templateWriteBlocked || templateDenied}
                      onClick={() => openTemplateEditor(null)}
                    >
                      新建模板
                    </Button>
                  ) : null
                }
                columns={templateColumns}
                dataSource={templatesQuery.data ?? []}
                rowKey="id"
                loading={templatesQuery.isLoading}
              />
            ),
          },
          {
            key: 'channels',
            label: '通知渠道',
            children: (
              <AdminTable
                enableDensity
                error={channelsQuery.error}
                hasReadData={channelsQuery.data !== undefined}
                refreshing={channelsQuery.isFetching}
                onRefresh={() => void channelsQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateNotification ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      disabled={channelWriteBlocked || channelDenied}
                      onClick={() => openChannelEditor(null)}
                    >
                      新建渠道
                    </Button>
                  ) : null
                }
                columns={channelColumns}
                dataSource={readableQueryData(channelsQuery) ?? []}
                rowKey="id"
                loading={channelsQuery.isLoading}
              />
            ),
          },
          {
            key: 'routes',
            label: '路由规则',
            children: (
              <AdminTable
                enableDensity
                error={routesQuery.error}
                hasReadData={routesQuery.data !== undefined}
                refreshing={routesQuery.isFetching}
                onRefresh={() => void routesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                columns={routeColumns}
                dataSource={routesQuery.data ?? []}
                rowKey="id"
                loading={routesQuery.isLoading}
                headerExtra={
                  <Space>
                    <Text data-testid="notification-route-compat-note" type="secondary">
                      兼容 `/alert-routes`，保存后同步到通知策略。
                    </Text>
                    {canCreateNotification ? (
                      <Button
                        type="primary"
                        icon={<PlusOutlined />}
                        disabled={routeWriteBlocked || routeDenied}
                        onClick={() => openRouteEditor(null)}
                      >
                        新建路由
                      </Button>
                    ) : null}
                  </Space>
                }
              />
            ),
          },
          {
            key: 'silences',
            label: '静默规则',
            children: (
              <AdminTable
                enableDensity
                error={silencesQuery.error}
                hasReadData={silencesQuery.data !== undefined}
                refreshing={silencesQuery.isFetching}
                onRefresh={() => void silencesQuery.refetch()}
                columnSettingPlacement="header"
                columnSettingIconOnly
                shellClassName="soha-management-table-shell"
                headerExtra={
                  canCreateNotification ? (
                    <Button
                      icon={<PlusOutlined />}
                      type="primary"
                      disabled={silenceWriteBlocked || silenceDenied}
                      onClick={() => openSilenceEditor(null)}
                    >
                      新建静默
                    </Button>
                  ) : null
                }
                columns={silenceColumns}
                dataSource={silencesQuery.data ?? []}
                rowKey="id"
                loading={silencesQuery.isLoading}
              />
            ),
          },
        ]}
      />

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={editingPolicy ? '编辑通知策略' : '新建通知策略'}
        open={policyOpen && !policyDenied}
        closable={!policyWriteBlocked}
        keyboard={!policyWriteBlocked}
        mask={{ closable: false }}
        onCancel={closePolicyEditor}
        footer={
          <Space>
            <Button disabled={policyWriteBlocked} onClick={closePolicyEditor}>
              取消
            </Button>
            <Button
              type="primary"
              loading={createPolicy.isPending || updatePolicy.isPending}
              disabled={policyBlocked}
              onClick={() => policyForm.submit()}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={760}
      >
        {createPolicy.feedback}
        {updatePolicy.feedback}
        <ReadFeedback label="通知渠道选项" query={channelsQuery} />
        <ReadFeedback label="值班表选项" query={oncallSchedulesQuery} />
        <ReadFeedback label="升级策略选项" query={oncallPoliciesQuery} />
        <Form
          layout="vertical"
          disabled={policyBlocked}
          form={policyForm}
          onFinish={submitPolicy}
          initialValues={{ sendResolved: false, cooldownSeconds: 0, enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)">
            <Input.TextArea rows={4} />
          </Form.Item>
          <Form.Item name="processorChain" label="处理链">
            <Select
              mode="tags"
              options={[
                { value: 'template_render', label: 'template_render' },
                { value: 'webhook_update', label: 'webhook_update' },
              ]}
            />
          </Form.Item>
          <Form.Item name="channelRefs" label="渠道引用">
            <Select mode="multiple" allowClear options={channelOptions} />
          </Form.Item>
          <Form.Item name="oncallRef" label="OnCall 引用">
            <Select allowClear options={oncallOptions} />
          </Form.Item>
          <Form.Item name="cooldownSeconds" label="冷却(s)">
            <InputNumber min={0} style={{ width: '100%' }} />
          </Form.Item>
          <Form.Item name="sendResolved" label="恢复通知" valuePropName="checked">
            <Switch />
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
        title={editingTemplate ? '编辑通知模板' : '新建通知模板'}
        open={templateOpen && !templateDenied}
        closable={!templateWriteBlocked}
        keyboard={!templateWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeTemplateEditor}
        footer={
          <Space>
            <Button disabled={templateWriteBlocked} onClick={closeTemplateEditor}>
              取消
            </Button>
            <Button
              type="primary"
              loading={createTemplate.isPending || updateTemplate.isPending}
              disabled={templateBlocked}
              onClick={() => templateForm.submit()}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={860}
      >
        {createTemplate.feedback}
        {updateTemplate.feedback}
        <Form
          layout="vertical"
          disabled={templateBlocked}
          form={templateForm}
          onFinish={submitTemplate}
          initialValues={{
            templateType: 'generic_json',
            contentType: 'application/json',
            enabled: true,
          }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <div className="soha-observability-form-grid">
            <Form.Item name="templateType" label="模板类型">
              <Select
                options={[
                  { value: 'generic_json', label: 'generic_json' },
                  { value: 'alertmanager_v1', label: 'alertmanager_v1' },
                  { value: 'grafana_v1', label: 'grafana_v1' },
                ]}
              />
            </Form.Item>
            <Form.Item name="contentType" label="Content-Type">
              <Input />
            </Form.Item>
          </div>
          <Form.Item name="bodyTemplate" label="Body 模板">
            <Input.TextArea rows={6} />
          </Form.Item>
          <Form.Item name="headers" label="Headers(JSON)">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="queryParams" label="QueryParams(JSON)">
            <Input.TextArea rows={3} />
          </Form.Item>
          <Form.Item name="samplePayload" label="样例 Payload(JSON)">
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
        title={editingChannel ? '编辑通知渠道' : '新建通知渠道'}
        open={channelOpen && !channelDenied}
        closable={!channelWriteBlocked}
        keyboard={!channelWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeChannelEditor}
        footer={
          <Space>
            <Button disabled={channelWriteBlocked} onClick={closeChannelEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={channelBlocked}
              onClick={() => channelForm.submit()}
              loading={createChannel.isPending || updateChannel.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={760}
      >
        {createChannel.feedback}
        {updateChannel.feedback}
        <Form
          layout="vertical"
          disabled={channelBlocked}
          form={channelForm}
          onFinish={submitChannel}
          initialValues={{ channelType: 'webhook', enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="channelType" label="渠道类型" rules={[{ required: true }]}>
            <Select
              options={['webhook', 'slack', 'feishu', 'dingtalk', 'wechat'].map((value) => ({
                value,
                label: value,
              }))}
            />
          </Form.Item>
          <Form.Item name="config" label="配置(JSON)" rules={[{ required: true }]}>
            <Input.TextArea rows={7} />
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
        title={editingRoute ? '编辑路由规则' : '新建路由规则'}
        open={routeOpen && !routeDenied}
        closable={!routeWriteBlocked}
        keyboard={!routeWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeRouteEditor}
        footer={
          <Space>
            <Button disabled={routeWriteBlocked} onClick={closeRouteEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={routeBlocked}
              onClick={() => routeForm.submit()}
              loading={createRoute.isPending || updateRoute.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={760}
      >
        {createRoute.feedback}
        {updateRoute.feedback}
        <ReadFeedback label="通知渠道选项" query={channelsQuery} />
        <Form
          layout="vertical"
          disabled={routeBlocked}
          form={routeForm}
          onFinish={submitRoute}
          initialValues={{ enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)">
            <Input.TextArea rows={5} />
          </Form.Item>
          <Form.Item name="channelIds" label="接收渠道">
            <Select mode="multiple" allowClear options={channelOptions} />
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
        title={editingSilence ? '编辑静默规则' : '新建静默规则'}
        open={silenceOpen && !silenceDenied}
        closable={!silenceWriteBlocked}
        keyboard={!silenceWriteBlocked}
        mask={{ closable: false }}
        onCancel={closeSilenceEditor}
        footer={
          <Space>
            <Button disabled={silenceWriteBlocked} onClick={closeSilenceEditor}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={silenceBlocked}
              onClick={() => silenceForm.submit()}
              loading={createSilence.isPending || updateSilence.isPending}
            >
              保存
            </Button>
          </Space>
        }
        destroyOnHidden
        width={760}
      >
        {createSilence.feedback}
        {updateSilence.feedback}
        <Form
          layout="vertical"
          disabled={silenceBlocked}
          form={silenceForm}
          onFinish={submitSilence}
          initialValues={{ enabled: true }}
        >
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input />
          </Form.Item>
          <Form.Item name="matchers" label="匹配器(JSON)" rules={[{ required: true }]}>
            <Input.TextArea rows={5} />
          </Form.Item>
          <Form.Item name="reason" label="静默原因">
            <Input.TextArea rows={3} />
          </Form.Item>
          <div className="soha-observability-form-grid">
            <Form.Item name="startsAt" label="开始时间(ISO)" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
            <Form.Item name="endsAt" label="结束时间(ISO)" rules={[{ required: true }]}>
              <Input />
            </Form.Item>
          </div>
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
        title={previewPolicy ? `通知预览 · ${previewPolicy.name}` : '通知预览'}
        open={previewOpen}
        onCancel={() => setPreviewOpen(false)}
        footer={null}
        width={960}
        destroyOnHidden
      >
        <Space orientation="vertical" style={{ width: '100%' }} size={16}>
          <ReadFeedback label="预览事件选项" query={alertEventsQuery} />
          <ReadFeedback label="通知策略" query={policiesQuery} />
          <Select
            aria-label="通知预览事件"
            disabled={queryAccessDenied(policiesQuery) || !readableQueryData(alertEventsQuery)}
            value={previewEventId}
            onChange={(value) => {
              const next = String(value)
              setPreviewEventId(next)
            }}
            style={{ width: '100%' }}
            placeholder="选择告警事件"
            options={(readableQueryData(alertEventsQuery) ?? []).map((item) => ({
              value: item.id,
              label: `${item.title} (${item.status})`,
            }))}
          />
          <AdminTable
            columnSettingPlacement="header"
            columnSettingIconOnly
            columns={[
              { title: '渠道', dataIndex: 'channelId' },
              {
                title: '模板',
                dataIndex: 'templateId',
                render: (value: string) => value || '-',
              },
              { title: 'URL', dataIndex: 'url', ellipsis: true },
              { title: 'Method', dataIndex: 'method' },
              { title: 'Content-Type', dataIndex: 'contentType' },
              {
                title: 'Body',
                dataIndex: 'body',
                render: (value: string) => <Text code>{String(value || '')}</Text>,
              },
            ]}
            error={previewQuery.error}
            hasReadData={previewQuery.data !== undefined}
            loading={previewQuery.isLoading}
            refreshing={previewQuery.isFetching}
            onRefresh={() => void previewQuery.refetch()}
            dataSource={
              !queryAccessDenied(policiesQuery) && !queryAccessDenied(alertEventsQuery)
                ? (readableQueryData(previewQuery) ?? [])
                : []
            }
            rowKey={(record) =>
              `${record.channelId || 'channel'}:${record.templateId || 'template'}:${record.url || 'url'}`
            }
            pagination={false}
          />
        </Space>
      </Modal>
    </div>
  )
}
