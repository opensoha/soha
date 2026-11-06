import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  App,
  Button,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Tabs,
} from 'antd'
import type { TableColumnsType } from 'antd'
import {
  EditOutlined,
  EyeOutlined,
  HistoryOutlined,
  PlusOutlined,
  ReloadOutlined,
} from '@ant-design/icons'
import type {
  NetworkEndpointResponsePolicy,
  NetworkEndpointResponsePolicyConfiguration,
  NetworkEndpointResponsePreviewInput,
  NetworkEndpointResponsePolicyRevision,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { requestErrorMessage, useInlineApiError } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { ManagementIconButton, ManagementTableToolbar } from '@/components/management-list'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EndpointResponsePolicyFields, responsePolicyDefaults } from './policy-fields'
import { endpointResponseMutations } from './mutations'
import { endpointResponseQueries } from './queries'
import type { EndpointResponseFilters } from './api'
import { responseLabel } from './labels'

export function EndpointResponsePolicies({
  can,
  filters,
}: {
  can: (key: string) => boolean
  filters: EndpointResponseFilters
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const { message } = App.useApp()
  const client = useQueryClient()
  const policies = useQuery(
    endpointResponseQueries.policies(
      filters,
      can('network_access.endpoint_response_policies.view'),
    ),
  )
  const save = useOperationMutation(endpointResponseMutations.save(client), {
    label: ['保存联动规则草稿', 'Save response policy draft'],
    target: ({ input }) => input.configuration.name,
  })
  const publish = useOperationMutation(endpointResponseMutations.publish(client), {
    label: ['发布联动规则', 'Publish response policy'],
    target: ({ policyId }) => policyId,
  })
  const rollback = useOperationMutation(endpointResponseMutations.rollback(client), {
    label: ['回滚发布联动规则', 'Roll back response policy'],
    target: ({ policyId, revision }) => `${policyId} · v${revision}`,
  })
  const remove = useOperationMutation(endpointResponseMutations.remove(client), {
    label: ['删除联动规则', 'Delete response policy'],
    target: ({ policyId }) => policyId,
  })
  const preview = useMutation(endpointResponseMutations.preview())
  const [editor, setEditor] = useState<NetworkEndpointResponsePolicy | 'new' | null>(null)
  const [selectedPolicy, setSelected] = useState<NetworkEndpointResponsePolicy | null>(null)
  const selected = readableQueryData({ data: selectedPolicy, error: policies.error }) ?? null
  const [form] = Form.useForm<NetworkEndpointResponsePolicyConfiguration>()
  const [previewForm] = Form.useForm<NetworkEndpointResponsePreviewInput>()
  const revisions = useQuery(endpointResponseQueries.revisions(selected?.id ?? ''))
  const pending = save.blocked || publish.blocked || rollback.blocked || remove.blocked
  useInlineApiError(selected ? preview.error : null)

  function edit(policy?: NetworkEndpointResponsePolicy) {
    if (pending) return
    save.reset()
    setEditor(policy ?? 'new')
    form.resetFields()
    form.setFieldsValue(policy?.configuration ?? structuredClone(responsePolicyDefaults))
  }
  async function submit(configuration: NetworkEndpointResponsePolicyConfiguration) {
    if (!editor || pending) return
    try {
      await save.mutateAsync({
        policyId: editor === 'new' ? undefined : editor.id,
        input: {
          expectedRevision: editor === 'new' ? 0 : editor.revision,
          configuration: { ...configuration, name: configuration.name.trim() },
        },
      })
      setEditor(null)
      void message.success(text('草稿已保存，发布后生效', 'Draft saved. Publish to apply it.'))
    } catch {
      /* The editor retains the draft and shows the mutation error. */
    }
  }
  const columns: TableColumnsType<NetworkEndpointResponsePolicy> = [
    { title: text('名称', 'Name'), key: 'name', render: (_, row) => row.configuration.name },
    {
      title: text('检测信号', 'Signal'),
      key: 'signal',
      render: (_, row) =>
        text(
          row.configuration.signal === 'service_unavailable' ? '保护服务失联' : 'UI 失联',
          row.configuration.signal === 'service_unavailable'
            ? 'Protection service unavailable'
            : 'UI unavailable',
        ),
    },
    {
      title: text('发布状态', 'Publication'),
      key: 'publication',
      render: (_, row) => (
        <Space>
          <StatusTag
            value={row.publishedRevision ? 'published' : 'draft'}
            label={
              row.publishedRevision
                ? text(`已发布 v${row.publishedRevision}`, `Published v${row.publishedRevision}`)
                : text('草稿', 'Draft')
            }
          />
          {row.revision !== row.publishedRevision && row.publishedRevision > 0
            ? text('有未发布修改', 'Unpublished changes')
            : null}
        </Space>
      ),
    },
    {
      title: text('已发布规则启用', 'Published policy enabled'),
      key: 'enabled',
      render: (_, row) =>
        row.publishedConfiguration ? (
          <BooleanTag
            value={row.publishedConfiguration.enabled}
            trueLabel={text('是', 'Yes')}
            falseLabel={text('否', 'No')}
          />
        ) : (
          '—'
        ),
    },
    { title: text('更新时间', 'Updated'), dataIndex: 'updatedAt', render: formatDateTime },
    {
      title: text('操作', 'Actions'),
      key: 'actions',
      width: 240,
      render: (_, row) => (
        <Space>
          {can('network_access.endpoint_response_policies.update') && (
            <ManagementIconButton
              icon={<EditOutlined />}
              aria-label={text('编辑策略', 'Edit policy')}
              tooltip={text('编辑', 'Edit')}
              disabled={pending}
              onClick={() => edit(row)}
            />
          )}
          <ManagementIconButton
            icon={<EyeOutlined />}
            aria-label={text('查看与预览', 'Details and preview')}
            tooltip={text('查看与预览', 'Details and preview')}
            onClick={() => {
              preview.reset()
              previewForm.resetFields()
              setSelected(row)
            }}
          />
          {can('network_access.endpoint_response_policies.publish') && (
            <Popconfirm
              title={text('发布此版本？', 'Publish this revision?')}
              okText={text('确认发布', 'Confirm publication')}
              cancelText={text('取消', 'Cancel')}
              description={text(
                '启用的规则将参与准入和在线会话处置；停用不会解除现有限制。',
                'Enabled policies affect admission and active sessions. Disabling does not clear existing restrictions.',
              )}
              onConfirm={() => {
                if (pending) return
                publish.mutate(
                  { policyId: row.id, expectedRevision: row.revision },
                  {
                    onSuccess: () => {
                      void message.success(text('已发布', 'Published'))
                    },
                  },
                )
              }}
            >
              <Button size="small" disabled={pending || row.revision === row.publishedRevision}>
                {text('发布', 'Publish')}
              </Button>
            </Popconfirm>
          )}
          {can('network_access.endpoint_response_policies.delete') && (
            <Popconfirm
              title={text(
                '删除此策略？已有限制仍保留。',
                'Delete this policy? Existing restrictions remain.',
              )}
              onConfirm={() => {
                if (!pending) remove.mutate({ policyId: row.id, expectedRevision: row.revision })
              }}
            >
              <Button size="small" danger disabled={pending}>
                {text('删除', 'Delete')}
              </Button>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ]
  return (
    <>
      {editor == null && save.feedback}
      {!selected && publish.feedback}
      {!selected && remove.feedback}
      {!selected && rollback.feedback}
      <AdminTable
        rowKey="id"
        columns={columns}
        dataSource={policies.data ?? []}
        loading={policies.isLoading}
        refreshing={policies.isFetching}
        error={policies.error}
        hasReadData={policies.data !== undefined}
        onRetry={() => void policies.refetch()}
        viewportScroll
        toolbar={
          <ManagementTableToolbar>
            <span>
              {text(
                '保存草稿 → 预览 → 发布；发布版本保留审计记录',
                'Save draft → preview → publish. Published revisions retain their audit history.',
              )}
            </span>
            <ManagementIconButton
              icon={<ReloadOutlined />}
              aria-label={text('刷新策略', 'Refresh policies')}
              tooltip={text('刷新', 'Refresh')}
              loading={policies.isFetching}
              onClick={() => void policies.refetch()}
            />
            {can('network_access.endpoint_response_policies.create') && (
              <Button
                size="small"
                type="primary"
                icon={<PlusOutlined />}
                disabled={pending}
                onClick={() => edit()}
              >
                {text('新增规则', 'New policy')}
              </Button>
            )}
          </ManagementTableToolbar>
        }
      />
      <Modal
        open={readableQueryData({ data: editor, error: policies.error }) != null}
        title={text(
          editor === 'new' ? '新增终端联动规则' : '编辑终端联动规则',
          editor === 'new' ? 'New endpoint response policy' : 'Edit endpoint response policy',
        )}
        width={760}
        destroyOnHidden
        mask={{ closable: false }}
        confirmLoading={save.isPending}
        okButtonProps={{ disabled: pending }}
        okText={text('保存草稿', 'Save draft')}
        cancelText={text('取消', 'Cancel')}
        onOk={() => form.submit()}
        onCancel={() => {
          if (!save.isPending) setEditor(null)
        }}
      >
        <Form
          className="endpoint-response-form"
          form={form}
          layout="vertical"
          onFinish={(values) => void submit(values)}
        >
          <EndpointResponsePolicyFields can={can} />
          {save.feedback}
        </Form>
      </Modal>
      <Drawer
        open={selected !== null}
        title={selected?.configuration.name}
        size="large"
        destroyOnHidden
        onClose={() => setSelected(null)}
      >
        {selected && publish.feedback}
        {selected && remove.feedback}
        {selected && rollback.feedback}
        {selected && (
          <Tabs
            items={[
              {
                key: 'preview',
                label: text('状态与预览', 'State and preview'),
                children: (
                  <>
                    <Descriptions
                      column={1}
                      items={[
                        { key: 'id', label: 'ID', children: selected.id },
                        {
                          key: 'revision',
                          label: text('当前草稿 / 已发布', 'Draft / published'),
                          children: `${selected.revision} / ${selected.publishedRevision}`,
                        },
                      ]}
                    />
                    {can('network_access.endpoint_response_policies.preview') && (
                      <Form
                        form={previewForm}
                        layout="vertical"
                        onFinish={(input) => preview.mutate({ policyId: selected.id, input })}
                      >
                        <Form.Item
                          name="deviceId"
                          label={text('终端 ID', 'Endpoint ID')}
                          rules={[{ required: true, whitespace: true }]}
                        >
                          <Input maxLength={128} />
                        </Form.Item>
                        <Form.Item
                          name="sessionId"
                          label={text('会话 ID（可选）', 'Session ID (optional)')}
                        >
                          <Input maxLength={128} />
                        </Form.Item>
                        <Button type="primary" htmlType="submit" loading={preview.isPending}>
                          {text('预览当前草稿', 'Preview current draft')}
                        </Button>
                      </Form>
                    )}
                    {preview.error && (
                      <Alert
                        className="endpoint-response-form-alert"
                        type="error"
                        showIcon
                        title={text('预览失败', 'Preview failed')}
                        description={requestErrorMessage(preview.error, localeCode)}
                      />
                    )}
                    {preview.data && (
                      <Descriptions
                        className="endpoint-response-detail"
                        column={1}
                        bordered
                        size="small"
                        items={[
                          {
                            key: 'matched',
                            label: text('范围匹配', 'Scope matches'),
                            children: (
                              <BooleanTag
                                value={preview.data.matched}
                                trueLabel={text('是', 'Yes')}
                                falseLabel={text('否', 'No')}
                              />
                            ),
                          },
                          {
                            key: 'reason',
                            label: text('判断原因', 'Reason'),
                            children: preview.data.reasonCode,
                          },
                          {
                            key: 'service',
                            label: text('保护服务', 'Protection service'),
                            children: <StatusTag value={preview.data.presence.serviceState} />,
                          },
                          {
                            key: 'ui',
                            label: 'UI',
                            children: <StatusTag value={preview.data.presence.uiState} />,
                          },
                          {
                            key: 'nas',
                            label: text('NAS 会话', 'NAS sessions'),
                            children: preview.data.sessions.map((session) => (
                              <div key={session.sessionId}>
                                {session.sessionId} · <StatusTag value={session.state} /> ·{' '}
                                {formatDateTime(session.lastReceivedAt)}
                              </div>
                            )),
                          },
                          {
                            key: 'trigger',
                            label: text('预计触发时间', 'Trigger time'),
                            children: formatDateTime(preview.data.triggerAfter),
                          },
                          {
                            key: 'steps',
                            label: text('处置计划', 'Action plan'),
                            children: preview.data.steps.map((step, i) => (
                              <div key={i}>
                                {step.afterSeconds}s · {responseLabel(step.action, localeCode)}
                                {step.targetAccessProfile
                                  ? ` · ${responseLabel(step.targetAccessProfile, localeCode)}`
                                  : ''}
                              </div>
                            )),
                          },
                          {
                            key: 'unsupported',
                            label: text('不支持的动作', 'Unsupported actions'),
                            children:
                              preview.data.unsupportedActions
                                .map((action) => responseLabel(action, localeCode))
                                .join(', ') || text('无', 'None'),
                          },
                        ]}
                      />
                    )}
                  </>
                ),
              },
              {
                key: 'history',
                label: (
                  <Space>
                    <HistoryOutlined />
                    {text('发布历史', 'Publication history')}
                  </Space>
                ),
                children: (
                  <>
                    <AdminTable
                      rowKey="revision"
                      loading={revisions.isLoading}
                      refreshing={revisions.isFetching}
                      error={revisions.error}
                      hasReadData={revisions.data !== undefined}
                      onRetry={() => void revisions.refetch()}
                      columns={[
                        { title: text('版本', 'Revision'), dataIndex: 'revision' },
                        {
                          title: text('发布时间', 'Published'),
                          dataIndex: 'createdAt',
                          render: formatDateTime,
                        },
                        { title: text('发布者', 'Actor'), dataIndex: 'actorId' },
                        {
                          title: text('操作', 'Action'),
                          key: 'rollback',
                          render: (_: unknown, row: NetworkEndpointResponsePolicyRevision) =>
                            can('network_access.endpoint_response_policies.publish') && (
                              <Popconfirm
                                title={text(
                                  '以此配置发布一个新版本？',
                                  'Publish a new revision with this configuration?',
                                )}
                                onConfirm={() => {
                                  if (pending) return
                                  rollback.mutate(
                                    {
                                      policyId: selected.id,
                                      expectedRevision: selected.revision,
                                      revision: row.revision,
                                    },
                                    { onSuccess: setSelected },
                                  )
                                }}
                              >
                                <Button size="small" disabled={pending}>
                                  {text('回滚发布', 'Roll back')}
                                </Button>
                              </Popconfirm>
                            ),
                        },
                      ]}
                      dataSource={revisions.data ?? []}
                    />
                  </>
                ),
              },
            ]}
          />
        )}
      </Drawer>
    </>
  )
}
