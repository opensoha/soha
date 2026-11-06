import { useEffect, useState } from 'react'
import {
  Alert,
  Button,
  Drawer,
  Form,
  Input,
  Popconfirm,
  Select,
  Space,
  Switch,
  Typography,
} from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  DockerVolumeDLPContent,
  DockerVolumeDLPEvent,
  DockerVolumeDLPPolicy,
} from '@opensoha/contracts/gen/ts/sohaapi'
import {
  DataLevelFields,
  DLPShadowPanel,
  DecisionEvidence,
  dataLevelOptions,
} from '@/components/content-dlp-policy-tools'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  contentDLPKeys,
  contentDLPQueries,
  publishContentDLP,
  saveContentDLP,
  previewContentDLP,
} from './content-dlp-api'
import { useAuthStore } from '@/stores/auth-store'
import { dockerKeys } from './keys'

export function DockerContentDLP({ projectId }: { projectId: string }) {
  const [open, setOpen] = useState(false)
  const viewer = useAuthStore((s) => s.user?.userId ?? '')
  const permissionQuery = usePermissionSnapshot()
  const snapshot =
    permissionQuery.isEnabled === false || !permissionQuery.isSuccess || permissionQuery.isFetching
      ? undefined
      : readableQueryData(permissionQuery)?.data
  const permissions = [
    'docker.dlp-policy.view',
    'docker.dlp-policy.update',
    'docker.dlp-policy.publish',
    'docker.dlp-events.view',
    'network_access.dlp_linkage.manage',
  ].map((key) => hasPermission(snapshot, key))
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  useEffect(() => setOpen(false), [projectId])
  if (!permissions.slice(0, 4).some(Boolean)) return null
  return (
    <>
      <Button size="small" onClick={() => setOpen(true)}>
        {text('内容防护', 'Content protection')}
      </Button>
      {open ? (
        <DLPDrawer
          key={`${projectId}:${viewer}:${permissions.join(',')}`}
          projectId={projectId}
          viewer={viewer}
          permissions={permissions}
          onClose={() => setOpen(false)}
        />
      ) : null}
    </>
  )
}

function DLPDrawer({
  projectId,
  permissions,
  viewer,
  onClose,
}: {
  projectId: string
  permissions: boolean[]
  viewer: string
  onClose: () => void
}) {
  const [canRead, canEdit, canPublish, canEvents, canLink] = permissions
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const client = useQueryClient()
  const [form] = Form.useForm<DockerVolumeDLPContent>()
  const mode = Form.useWatch('mode', form)
  const [revision, setRevision] = useState<number>()
  const [dirty, setDirty] = useState(false)
  const policyQuery = useQuery(contentDLPQueries.policy(projectId, viewer, canRead))
  const eventsQuery = useQuery(contentDLPQueries.events(projectId, viewer, canEvents))
  const policy = canRead ? readableQueryData(policyQuery) : undefined
  const accept = (value: DockerVolumeDLPPolicy) => {
    client.setQueryData(contentDLPKeys.policy(projectId, viewer), value)
    form.resetFields()
    form.setFieldsValue({ networkAction: 'none', ...value.draft })
    setRevision(value.revision)
    setDirty(false)
    // Old preview bytes cannot establish the newly published protection result.
    client.removeQueries({ queryKey: dockerKeys.projectRuntime(projectId), type: 'inactive' })
    void client.invalidateQueries({ queryKey: dockerKeys.projectRuntime(projectId) })
    void client.invalidateQueries({ queryKey: contentDLPKeys.events(projectId, viewer) })
  }
  useEffect(() => {
    if (!canRead || queryAccessDenied(policyQuery)) {
      form.resetFields()
      setRevision(undefined)
      setDirty(false)
      return
    }
    if (policy && revision === undefined) {
      form.setFieldsValue({ networkAction: 'none', ...policy.draft })
      setRevision(policy.revision)
    }
  }, [policy, canRead, policyQuery.isError, form, revision])
  const save = useOperationMutation(
    {
      mutationFn: (draft: DockerVolumeDLPContent) =>
        saveContentDLP(projectId, { expectedRevision: revision ?? 0, draft }),
      onSuccess: accept,
      onError: () =>
        void client.invalidateQueries({ queryKey: contentDLPKeys.policy(projectId, viewer) }),
    },
    {
      label: ['保存 DLP 草稿', 'Save DLP draft'],
      target: () => projectId,
      receipt: (data) => ({
        message: text('草稿已保存，尚未发布', 'Draft saved; not published'),
        evidence: `r${data.revision}`,
      }),
    },
  )
  const publish = useOperationMutation(
    {
      mutationFn: (value: number) => publishContentDLP(projectId, value),
      onSuccess: accept,
      onError: () =>
        void client.invalidateQueries({ queryKey: contentDLPKeys.policy(projectId, viewer) }),
    },
    {
      label: ['发布 DLP 策略', 'Publish DLP policy'],
      target: () => projectId,
      receipt: (data) => ({
        message: text(
          '规则已发布，后续文件读取使用新版本',
          'Published; subsequent file reads use this version',
        ),
        evidence: `v${data.publishedVersion}`,
      }),
    },
  )
  const blocked =
    save.blocked ||
    publish.blocked ||
    policyQuery.isError ||
    policyQuery.isFetching ||
    !policy ||
    revision === undefined ||
    revision !== policy.revision
  return (
    <Drawer
      open
      destroyOnHidden
      size={820}
      title={text('卷文件内容防护', 'Volume file content protection')}
      onClose={() => {
        if (!save.isPending && !publish.isPending) onClose()
      }}
      closable={!save.isPending && !publish.isPending}
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Typography.Text>
          {text('项目', 'Project')}: {projectId}
        </Typography.Text>
        <Alert
          type="info"
          showIcon
          title={text(
            '仅覆盖本项目卷文件预览/下载',
            'Covers only this project’s volume file preview/download',
          )}
          description={text(
            '完整 UTF-8 文本最多 256 KiB；不覆盖终端、日志、直接卷访问、App 或 USB/剪贴板。未命中只表示所配置规则未命中。',
            'Complete UTF-8 text up to 256 KiB. Terminal, logs, direct volume access, App and USB/clipboard are outside this channel. No match means only that configured rules did not match.',
          )}
        />
        {!canRead && (canEdit || canPublish) ? (
          <Alert
            type="info"
            title={text(
              '操作草稿还需要策略查看权限',
              'Policy view permission is also required to inspect a draft',
            )}
          />
        ) : null}
        <ReadFeedback
          canRead={canRead}
          query={policyQuery}
          label={text('DLP 策略', 'DLP policy')}
        />
        {save.feedback}
        {publish.feedback}
        {policy ? (
          <>
            <Typography.Text>
              {text('已发布版本', 'Published version')}:{' '}
              {policy.publishedVersion || text('未发布', 'None')} ·{' '}
              {policy.published?.enabled ? policy.published.mode : text('未启用', 'Disabled')}
            </Typography.Text>
            <Form
              form={form}
              layout="vertical"
              disabled={!canEdit || blocked}
              onValuesChange={(changed) => {
                setDirty(true)
                if (changed.mode && changed.mode !== 'block')
                  form.setFieldValue('networkAction', 'none')
                if (changed.mode === 'audit') form.setFieldValue('requireStepUp', false)
              }}
              onFinish={(values) =>
                save.mutate({
                  ...values,
                  detectors: values.detectors ?? [],
                  rules: values.rules ?? [],
                })
              }
            >
              <Form.Item
                name="enabled"
                label={text('发布后启用', 'Enable when published')}
                valuePropName="checked"
              >
                <Switch />
              </Form.Item>
              <Form.Item name="mode" label={text('模式', 'Mode')} rules={[{ required: true }]}>
                <Select
                  options={[
                    {
                      label: text(
                        '记录（命中/未检查也可返回）',
                        'Audit (matches/uninspected may pass)',
                      ),
                      value: 'audit',
                    },
                    {
                      label: text(
                        '警告（完整命中文本需明确确认）',
                        'Warn (matched complete text requires acknowledgement)',
                      ),
                      value: 'warn',
                    },
                    {
                      label: text(
                        '阻断（达到阈值的匹配或未完整检查拒绝）',
                        'Block (deny threshold matches or incomplete inspection)',
                      ),
                      value: 'block',
                    },
                  ]}
                />
              </Form.Item>
              <Form.Item
                name="requireStepUp"
                label={text('命中放行前额外验证 MFA', 'Require MFA before releasing a match')}
                valuePropName="checked"
                extra={text(
                  '适用于警告/阻断；仍需原有确认或独立审批，领取和使用均校验同一登录会话。',
                  'For warn/block. Acknowledgement or independent approval is still required; issuance and use verify the same signed-in session.',
                )}
              >
                <Switch disabled={!canEdit || blocked || mode === 'audit'} />
              </Form.Item>
              <Form.Item
                name="networkAction"
                label={text('命中阻断后的网络联动', 'Network response after a matched block')}
              >
                <Select
                  disabled={!canEdit || blocked || !canLink || mode !== 'block'}
                  options={[
                    { value: 'none', label: text('不联动', 'None') },
                    {
                      value: 'revoke_network',
                      label: text(
                        '限制该用户的受管网络访问',
                        'Restrict the user’s managed network access',
                      ),
                    },
                  ]}
                />
              </Form.Item>
              <Typography.Paragraph type="secondary">
                {text(
                  '网络联动需要独立管理权限和全主体范围授权；仅完整命中且阻断时触发。VPN 撤销先落库，配置等待运行时领取；NAS 状态以可信会话和执行回执为准，共享代理未归属流量不覆盖。',
                  'Linkage requires a separate permission and all-subject scope. It triggers only on a fully inspected matched block. VPN revocation is persisted before runtimes fetch configuration. NAS follows trusted sessions and receipts; unattributed shared proxy traffic is outside this scope.',
                )}
              </Typography.Paragraph>
              <DataLevelFields />
              <Form.Item name="detectors" label={text('内置检测器', 'Built-in detectors')}>
                <Select
                  mode="multiple"
                  options={[
                    { label: text('私钥标记', 'Private key marker'), value: 'private_key' },
                    { label: 'AWS access key', value: 'aws_access_key' },
                    {
                      label: text('凭据赋值', 'Credential assignment'),
                      value: 'credential_assignment',
                    },
                  ]}
                />
              </Form.Item>
              <Form.List name="rules">
                {(fields, { add, remove }) => (
                  <>
                    {fields.map((field) => (
                      <Space key={field.key} align="start">
                        <Form.Item
                          name={[field.name, 'id']}
                          label={text('规则 ID', 'Rule ID')}
                          rules={[{ required: true, pattern: /^[a-z][a-z0-9_-]{0,47}$/ }]}
                        >
                          <Input maxLength={48} />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, 'expression']}
                          label={text('RE2 表达式', 'RE2 expression')}
                          rules={[{ required: true, max: 256 }]}
                        >
                          <Input maxLength={256} />
                        </Form.Item>
                        <Form.Item
                          name={[field.name, 'dataLevel']}
                          initialValue="confidential"
                          label={text('等级', 'Data level')}
                        >
                          <Select options={dataLevelOptions(text)} style={{ minWidth: 130 }} />
                        </Form.Item>
                        <Button disabled={!canEdit || blocked} onClick={() => remove(field.name)}>
                          {text('移除', 'Remove')}
                        </Button>
                      </Space>
                    ))}
                    <Button
                      disabled={!canEdit || blocked || fields.length >= 16}
                      onClick={() => add({ id: '', expression: '', dataLevel: 'confidential' })}
                    >
                      {text('添加规则（最多 16 项）', 'Add rule (up to 16)')}
                    </Button>
                  </>
                )}
              </Form.List>
              <Typography.Paragraph type="secondary">
                {text(
                  '至少选择一个检测器或规则；不解压、不识别图片/OCR，也不递归解码混淆内容。',
                  'Select at least one detector or rule. No decompression, OCR or recursive decoding of obfuscated content.',
                )}
              </Typography.Paragraph>
              <Space wrap>
                <Button
                  htmlType="submit"
                  type="primary"
                  loading={save.isPending}
                  disabled={!canEdit || blocked}
                >
                  {text('保存草稿', 'Save draft')}
                </Button>
              </Space>
            </Form>
            <DLPShadowPanel
              key={`${projectId}:${policy.revision}`}
              revision={policy.revision}
              disabled={!canEdit || blocked || dirty || policy.revision < 1}
              evaluate={(input) => previewContentDLP(projectId, viewer, input)}
            />
            <Space wrap>
              <Popconfirm
                title={
                  policy.draft.enabled
                    ? text(
                        `发布 ${policy.draft.mode} 规则？`,
                        `Publish ${policy.draft.mode} rules?`,
                      )
                    : text('发布停用内容防护？', 'Publish disabled content protection?')
                }
                description={text(
                  '仅发布已保存草稿；已送达客户端的内容无法撤回。',
                  'Publishes the saved draft only. Previously delivered content cannot be recalled.',
                )}
                onConfirm={() => publish.mutate(policy.revision)}
                disabled={!canPublish || blocked || dirty || policy.revision === 0}
              >
                <Button
                  loading={publish.isPending}
                  disabled={!canPublish || blocked || dirty || policy.revision === 0}
                >
                  {text('发布已保存草稿', 'Publish saved draft')}
                </Button>
              </Popconfirm>
            </Space>
            <Button
              disabled={save.isPending || publish.isPending}
              onClick={async () => {
                const result = await policyQuery.refetch()
                if (result.data && !result.isError) {
                  form.resetFields()
                  form.setFieldsValue({ networkAction: 'none', ...result.data.draft })
                  setRevision(result.data.revision)
                  setDirty(false)
                }
              }}
            >
              {text('重新读取（丢弃本地编辑）', 'Reload (discard local edits)')}
            </Button>
          </>
        ) : null}
        <Typography.Text strong>
          {text('最近 100 条事件（不代表全部历史）', 'Latest 100 events (not full history)')}
        </Typography.Text>
        <ReadFeedback
          canRead={canEvents}
          query={eventsQuery}
          label={text('DLP 事件', 'DLP events')}
        />
        {canEvents ? (
          <AdminTable
            rowKey="id"
            dataSource={readableQueryData(eventsQuery) ?? []}
            loading={eventsQuery.isLoading}
            pagination={{ pageSize: 10, showSizeChanger: false }}
            onRefresh={() => void eventsQuery.refetch()}
            columns={[
              { title: text('事件', 'Event'), dataIndex: 'id', ellipsis: true },
              {
                title: text('操作 / 主体', 'Action / Actor'),
                key: 'actor',
                render: (_: unknown, e: DockerVolumeDLPEvent) => (
                  <>
                    {e.action}
                    <br />
                    {e.actorId}
                  </>
                ),
              },
              {
                title: text('判定', 'Decision'),
                key: 'decision',
                render: (_: unknown, e: DockerVolumeDLPEvent) => (
                  <>
                    {e.action === 'transfer' ? (
                      <StatusTag
                        value={e.allowed ? 'success' : 'error'}
                        label={e.allowed ? text('允许', 'Allowed') : text('阻断', 'Blocked')}
                      />
                    ) : (
                      text('配置变更', 'Configuration change')
                    )}
                    <br />
                    {e.status} · {e.mode} v{e.policyVersion}
                    <DecisionEvidence decision={e} />
                  </>
                ),
              },
              {
                title: text('摘要 / 规则', 'Digest / Rules'),
                key: 'evidence',
                render: (_: unknown, e: DockerVolumeDLPEvent) => (
                  <Typography.Text ellipsis title={e.contentFingerprint}>
                    {e.contentFingerprint?.slice(0, 12) || '-'}
                    <br />
                    {e.matchedRuleIds.join(', ') || '-'}
                  </Typography.Text>
                ),
              },
              { title: text('时间', 'Time'), dataIndex: 'createdAt' },
            ]}
          />
        ) : null}
      </Space>
    </Drawer>
  )
}
