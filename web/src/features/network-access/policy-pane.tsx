import { ManagementIconButton } from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons'
import type {
  EndpointDevicePostureStatus,
  EndpointDeviceManagementState,
  EndpointDeviceStatus,
  NetworkAccessMode,
  NetworkAccessPolicy,
  NetworkAccessPolicyInput,
  NetworkAccessProfile,
  NetworkConflictAnalysisRequest,
  NetworkConflictRange,
  NetworkConflictSourceType,
  NetworkPolicyEffect,
  NetworkPolicyPreviewRequest,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType, TabsProps } from 'antd'
import {
  Alert,
  Button,
  Card,
  Descriptions,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tabs,
} from 'antd'
import { useState } from 'react'
import { TablePane } from './table-pane'
import { BooleanTag, StatusTag } from '@/components/status-tag'
import { formatDateTime } from '@/utils/time'
import { networkAccessMutations } from './mutations'
import { networkAccessQueries } from './queries'
import { NetworkAdmissionPreview } from './admission-preview'
import { endpointManagementLabel } from './device-detail-drawer'

interface NetworkPolicyPaneProps {
  canCreate: boolean
  canDelete: boolean
  canUpdate: boolean
}
interface PolicyFormValues {
  accessProfile: NetworkAccessProfile
  deviceStatuses: EndpointDeviceStatus[]
  deviceManagementStates: EndpointDeviceManagementState[]
  effect: NetworkPolicyEffect
  enabled: boolean
  modes: NetworkAccessMode[]
  name: string
  postureStatuses: EndpointDevicePostureStatus[]
  priority: number
  resourceIdsText?: string
  rolesText?: string
  siteIdsText?: string
  tagsText?: string
  teamsText?: string
  usersText?: string
}
interface ConflictFormValues {
  rangesText?: string
}
const LIST_FILTER = { limit: 200 } as const
const MODES: NetworkAccessMode[] = [
  'internal_direct',
  'internal_ztna',
  'external_vpn',
  'external_vpn_ztna',
  'external_direct_ztna',
]
const CONFLICT_SOURCE_TYPES: NetworkConflictSourceType[] = [
  'lan',
  'wireguard_overlay',
  'container',
  'kubernetes_pod',
  'kubernetes_service',
  'mihomo_fake_ip',
]
function parseTextList(value?: string) {
  return String(value ?? '')
    .split(/[\s,]+/)
    .map((item) => item.trim())
    .filter(Boolean)
}
function parseConflictRanges(value?: string): NetworkConflictRange[] {
  return String(value ?? '')
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [sourceType, name, cidr, sourceId, ...extra] = line
        .split(',')
        .map((item) => item.trim())
      if (
        extra.length ||
        !CONFLICT_SOURCE_TYPES.includes(sourceType as NetworkConflictSourceType) ||
        !name ||
        !cidr
      ) {
        throw new Error('invalid conflict range')
      }
      return {
        sourceType: sourceType as NetworkConflictSourceType,
        name,
        cidr,
        ...(sourceId ? { sourceId } : {}),
      }
    })
}
function textList(values: string[]) {
  return values.join('\n')
}
export function NetworkPolicyPane({ canCreate, canDelete, canUpdate }: NetworkPolicyPaneProps) {
  const { t, localeCode } = useI18n()
  const queryClient = useQueryClient()
  const policies = useQuery(networkAccessQueries.policies(LIST_FILTER))
  const visiblePoliciesData = policies.isEnabled === false ? undefined : readableQueryData(policies)
  const snapshot = useQuery(networkAccessQueries.policySnapshot())
  const visibleSnapshotData = snapshot.isEnabled === false ? undefined : readableQueryData(snapshot)
  const createPolicy = useOperationMutation(networkAccessMutations.policy.create(queryClient), {
    label: ['创建 网络策略', 'Create network policy'],
    target: (v) => v.name,
  })
  const updatePolicy = useOperationMutation(networkAccessMutations.policy.update(queryClient), {
    label: ['保存 网络策略', 'Save network policy'],
    target: (v) => v.policyId,
  })
  const deletePolicy = useOperationMutation(networkAccessMutations.policy.remove(queryClient), {
    label: ['删除 网络策略', 'Delete network policy'],
    target: (v) => v,
  })
  const compilePolicy = useOperationMutation(networkAccessMutations.policy.compile(queryClient), {
    label: ['编译网络策略', 'Compile network policy network policy'],
    target: () => '网络策略',
  })
  const previewPolicy = useOperationMutation(networkAccessMutations.policy.preview(), {
    label: ['预览网络策略', 'Preview network policy network policy'],
    target: () => '网络策略',
    readOnly: true,
  })
  const analyzeConflicts = useOperationMutation(networkAccessMutations.policy.analyzeConflicts(), {
    label: ['分析策略冲突 网络策略', 'Analyze policy conflicts network policy'],
    target: () => '网络策略',
    readOnly: true,
  })
  const operationBlocked =
    createPolicy.blocked ||
    updatePolicy.blocked ||
    deletePolicy.blocked ||
    compilePolicy.blocked ||
    (policies.isEnabled !== false && queryAccessDenied(policies))
  const [editor, setEditor] = useState<NetworkAccessPolicy | 'new' | null>(null)
  const [policyForm] = Form.useForm<PolicyFormValues>()
  const [previewForm] = Form.useForm<NetworkPolicyPreviewRequest>()
  const [conflictForm] = Form.useForm<ConflictFormValues>()
  const effect = Form.useWatch('effect', policyForm)
  function openEditor(policy?: NetworkAccessPolicy) {
    setEditor(policy ?? 'new')
    policyForm.resetFields()
    policyForm.setFieldsValue(
      policy
        ? {
            name: policy.name,
            enabled: policy.enabled,
            priority: policy.priority,
            effect: policy.effect,
            accessProfile: policy.accessProfile,
            usersText: textList(policy.subjects.users),
            rolesText: textList(policy.subjects.roles ?? []),
            teamsText: textList(policy.subjects.teams),
            tagsText: textList(policy.subjects.tags),
            siteIdsText: textList(policy.siteIds),
            resourceIdsText: textList(policy.resourceIds),
            modes: policy.modes,
            deviceStatuses: policy.deviceStatuses,
            deviceManagementStates: policy.deviceManagementStates ?? [],
            postureStatuses: policy.postureStatuses,
          }
        : {
            name: '',
            enabled: true,
            priority: 100,
            effect: 'allow',
            accessProfile: 'full',
            usersText: '',
            rolesText: '',
            teamsText: '',
            tagsText: '',
            siteIdsText: '',
            resourceIdsText: '',
            modes: [],
            deviceStatuses: ['active'],
            deviceManagementStates: [],
            postureStatuses: ['compliant'],
          },
    )
  }
  async function submitPolicy() {
    const values = await policyForm.validateFields()
    const input: NetworkAccessPolicyInput = {
      name: values.name.trim(),
      enabled: values.enabled,
      priority: values.priority,
      effect: values.effect,
      subjects: {
        users: parseTextList(values.usersText),
        roles: parseTextList(values.rolesText),
        teams: parseTextList(values.teamsText),
        tags: parseTextList(values.tagsText),
      },
      siteIds: parseTextList(values.siteIdsText),
      resourceIds: parseTextList(values.resourceIdsText),
      modes: values.modes,
      deviceStatuses: values.deviceStatuses,
      ...(editor === 'new' ||
      [...(values.deviceManagementStates ?? [])].sort().join(',') !==
        [...(editor?.deviceManagementStates ?? [])].sort().join(',')
        ? { deviceManagementStates: values.deviceManagementStates ?? [] }
        : {}),
      postureStatuses: values.postureStatuses,
      accessProfile: values.effect === 'deny' ? 'deny' : values.accessProfile,
    }
    if (editor === 'new') await createPolicy.mutateAsync(input)
    else if (editor) await updatePolicy.mutateAsync({ policyId: editor.id, input })
    setEditor(null)
  }
  function submitPreview(values: NetworkPolicyPreviewRequest) {
    previewPolicy.mutate({
      subjectUserId: values.subjectUserId.trim(),
      deviceId: values.deviceId.trim(),
      resourceId: values.resourceId.trim(),
      mode: values.mode,
      ...(values.siteId?.trim() ? { siteId: values.siteId.trim() } : {}),
    })
  }
  function submitConflicts(values: ConflictFormValues) {
    const input: NetworkConflictAnalysisRequest = {
      runtimeRanges: parseConflictRanges(values.rangesText),
    }
    analyzeConflicts.mutate(input)
  }
  const columns: TableColumnsType<NetworkAccessPolicy> = [
    { title: t('networkAccess.name', '名称'), dataIndex: 'name', width: 180 },
    {
      title: t('networkAccess.policy.effect', '效果'),
      dataIndex: 'effect',
      width: 90,
      render: (value: NetworkPolicyEffect) => <StatusTag value={value} />,
    },
    { title: t('networkAccess.policy.priority', '优先级'), dataIndex: 'priority', width: 90 },
    {
      title: t('networkAccess.policy.enabled', '启用'),
      dataIndex: 'enabled',
      width: 80,
      render: (value: boolean) => <BooleanTag value={value} />,
    },
    {
      title: t('networkAccess.policy.profile', '访问等级'),
      dataIndex: 'accessProfile',
      width: 120,
    },
    {
      title: t('networkAccess.policy.subjects', '主体'),
      render: (_: unknown, policy) =>
        [
          ...policy.subjects.users.map((value) => 'user:' + value),
          ...(policy.subjects.roles ?? []).map((value) => 'role:' + value),
          ...policy.subjects.teams.map((value) => 'team:' + value),
          ...policy.subjects.tags.map((value) => 'tag:' + value),
        ].join(', '),
    },
    {
      title: t('networkAccess.policy.modes', '访问形态'),
      dataIndex: 'modes',
      render: (value: string[]) => value.join(', ') || '*',
    },
    { title: t('networkAccess.version', '版本'), dataIndex: 'version', width: 80 },
    ...(canUpdate || canDelete
      ? [
          {
            key: 'actions',
            width: 90,
            render: (_: unknown, policy: NetworkAccessPolicy) => (
              <Space className="soha-row-action-icons">
                {canUpdate ? (
                  <ManagementIconButton
                    aria-label={t('networkAccess.policy.edit', '编辑策略')}
                    icon={<EditOutlined />}
                    tooltip={t('common.edit', '编辑')}
                    onClick={() => openEditor(policy)}
                    disabled={operationBlocked}
                  />
                ) : null}
                {canDelete ? (
                  <Popconfirm
                    title={t('networkAccess.policy.confirmDelete', '确认删除策略？')}
                    onConfirm={() => deletePolicy.mutate(policy.id)}
                    okButtonProps={{ disabled: operationBlocked }}
                    disabled={operationBlocked}
                  >
                    <ManagementIconButton
                      aria-label={t('networkAccess.policy.delete', '删除策略')}
                      danger
                      icon={<DeleteOutlined />}
                      tooltip={t('common.delete', '删除')}
                    />
                  </Popconfirm>
                ) : null}
              </Space>
            ),
          },
        ]
      : []),
  ]
  const tabs: TabsProps['items'] = [
    {
      key: 'drafts',
      label: t('networkAccess.policy.drafts', '策略草稿'),
      children: (
        <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
          <Card
            size="small"
            title={t('networkAccess.policy.publishedSnapshot', '已发布策略快照')}
            extra={
              canUpdate ? (
                <Button
                  type="primary"
                  loading={compilePolicy.isPending}
                  onClick={() => compilePolicy.mutate(undefined)}
                  disabled={operationBlocked}
                >
                  {t('networkAccess.policy.compile', '发布草稿')}
                </Button>
              ) : null
            }
          >
            {visibleSnapshotData ? (
              <Descriptions
                size="small"
                column={{ xs: 1, md: 3 }}
                items={[
                  {
                    key: 'version',
                    label: t('networkAccess.policyVersion', '策略版本'),
                    children: visibleSnapshotData.policyVersion,
                  },
                  {
                    key: 'policies',
                    label: t('networkAccess.policy.policyCount', '策略数'),
                    children: visibleSnapshotData.policyCount,
                  },
                  {
                    key: 'protected',
                    label: 'ProtectedSet',
                    children: visibleSnapshotData.protectedResourceCount,
                  },
                  {
                    key: 'hash',
                    label: t('networkAccess.policy.contentHash', '内容摘要'),
                    children: visibleSnapshotData.contentHash,
                  },
                  {
                    key: 'publishedAt',
                    label: t('networkAccess.policy.publishedAt', '发布时间'),
                    children: formatDateTime(visibleSnapshotData.publishedAt),
                  },
                ]}
              />
            ) : snapshot.isError || snapshot.isPending ? null : (
              <Alert
                showIcon
                type="warning"
                title={t('networkAccess.policy.notPublished', '尚未发布策略快照')}
              />
            )}
          </Card>
          <TablePane
            columns={columns}
            items={visiblePoliciesData}
            loading={policies.isLoading}
            error={policies.isEnabled === false ? null : policies.error}
            refreshing={policies.isFetching}
            onRefresh={() => void (policies.isEnabled === false ? undefined : policies.refetch())}
            searchPlaceholder={t('networkAccess.policy.search', '搜索策略、主体或资源')}
            getSearchValues={(policy) => [
              policy.name,
              policy.effect,
              policy.accessProfile,
              ...policy.subjects.users,
              ...(policy.subjects.roles ?? []),
              ...policy.subjects.teams,
              ...policy.subjects.tags,
              ...policy.siteIds,
              ...policy.resourceIds,
              ...policy.modes,
            ]}
            createAction={
              canCreate ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => openEditor()}
                  disabled={operationBlocked}
                >
                  {t('networkAccess.policy.add', '新增策略')}
                </Button>
              ) : null
            }
          />
        </Space>
      ),
    },
    {
      key: 'admission-preview',
      label: t('networkAccess.policy.admissionPreview', '准入预览'),
      children: <NetworkAdmissionPreview />,
    },
    {
      key: 'preview',
      label: t('networkAccess.policy.preview', '预览决策'),
      children: (
        <Card>
          <Form
            form={previewForm}
            layout="vertical"
            onFinish={submitPreview}
            disabled={operationBlocked}
          >
            <Space wrap align="start">
              {[
                ['subjectUserId', t('networkAccess.policy.userId', '用户 ID')],
                ['deviceId', t('networkAccess.policy.deviceId', '设备 ID')],
                ['resourceId', t('networkAccess.policy.resourceId', '资源 ID')],
              ].map(([name, label]) => (
                <Form.Item
                  key={name}
                  name={name}
                  label={label}
                  rules={[{ required: true, whitespace: true }]}
                >
                  <Input maxLength={128} />
                </Form.Item>
              ))}
              <Form.Item
                name="siteId"
                label={t('networkAccess.policy.siteOptional', '站点 ID（可选）')}
              >
                <Input maxLength={128} />
              </Form.Item>
              <Form.Item
                name="mode"
                label={t('networkAccess.policy.mode', '访问形态')}
                rules={[{ required: true }]}
              >
                <Select
                  style={{ width: 220 }}
                  options={MODES.map((value) => ({ value, label: value }))}
                />
              </Form.Item>
              <Form.Item label=" ">
                <Button
                  htmlType="submit"
                  type="primary"
                  loading={previewPolicy.isPending}
                  disabled={operationBlocked}
                >
                  {t('networkAccess.policy.preview', '预览决策')}
                </Button>
              </Form.Item>
            </Space>
          </Form>
          {previewPolicy.data ? (
            <>
              <Alert
                showIcon
                type={previewPolicy.data.decision === 'allow' ? 'success' : 'error'}
                title={
                  previewPolicy.data.decision === 'allow'
                    ? t('networkAccess.policy.allow', '允许访问')
                    : t('networkAccess.policy.deny', '拒绝访问')
                }
                description={previewPolicy.data.reasons.join(' · ')}
              />
              <Descriptions
                bordered
                size="small"
                column={{ xs: 1, md: 2, xl: 3 }}
                items={[
                  {
                    key: 'path',
                    label: t('networkAccess.policy.path', '执行路径'),
                    children: previewPolicy.data.path,
                  },
                  {
                    key: 'version',
                    label: t('networkAccess.policyVersion', '策略版本'),
                    children: previewPolicy.data.policyVersion,
                  },
                  {
                    key: 'protected',
                    label: 'ProtectedSet',
                    children: String(previewPolicy.data.protected),
                  },
                  {
                    key: 'networkLease',
                    label: t('networkAccess.policy.networkLease', '需要网络租约'),
                    children: String(previewPolicy.data.networkLeaseRequired),
                  },
                  {
                    key: 'resourceLease',
                    label: t('networkAccess.policy.resourceLease', '需要资源租约'),
                    children: String(previewPolicy.data.resourceLeaseRequired),
                  },
                  {
                    key: 'profile',
                    label: t('networkAccess.policy.profile', '访问等级'),
                    children: previewPolicy.data.networkProfile || '-',
                  },
                ]}
              />
            </>
          ) : null}
        </Card>
      ),
    },
    {
      key: 'conflicts',
      label: t('networkAccess.policy.conflicts', 'CIDR 冲突'),
      children: (
        <Card>
          <Form
            form={conflictForm}
            layout="vertical"
            onFinish={submitConflicts}
            disabled={operationBlocked}
          >
            <Form.Item
              name="rangesText"
              label={t('networkAccess.policy.runtimeRanges', '待检查运行时网段')}
              rules={[
                {
                  validator: (_, value) => {
                    try {
                      parseConflictRanges(value)
                      return Promise.resolve()
                    } catch {
                      return Promise.reject(
                        new Error(
                          t(
                            'networkAccess.policy.invalidRanges',
                            '每行格式：类型,名称,CIDR[,来源 ID]',
                          ),
                        ),
                      )
                    }
                  },
                },
              ]}
            >
              <Input.TextArea
                autoSize={{ minRows: 5, maxRows: 12 }}
                placeholder="wireguard_overlay,VPN overlay,10.200.0.0/16,gateway-1"
              />
            </Form.Item>
            <Button
              htmlType="submit"
              type="primary"
              loading={analyzeConflicts.isPending}
              disabled={operationBlocked}
            >
              {t('networkAccess.policy.analyzeConflicts', '分析冲突')}
            </Button>
          </Form>
          {analyzeConflicts.data ? (
            <Space orientation="vertical" size="small" style={{ width: '100%', marginTop: 16 }}>
              <Alert
                showIcon
                type={analyzeConflicts.data.valid ? 'success' : 'error'}
                title={
                  analyzeConflicts.data.valid
                    ? t('networkAccess.policy.noConflicts', '未发现地址冲突')
                    : t('networkAccess.policy.conflictsFound', '发现地址冲突')
                }
                description={
                  t('networkAccess.policy.rangesAnalyzed', '已分析网段') +
                  ': ' +
                  analyzeConflicts.data.rangesAnalyzed
                }
              />
              {analyzeConflicts.data.conflicts.map((conflict, index) => (
                <Alert
                  key={
                    conflict.left.sourceType +
                    conflict.left.cidr +
                    conflict.right.sourceType +
                    conflict.right.cidr +
                    index
                  }
                  type="warning"
                  title={conflict.reason}
                  description={
                    conflict.left.name +
                    ' (' +
                    conflict.left.cidr +
                    ') ↔ ' +
                    conflict.right.name +
                    ' (' +
                    conflict.right.cidr +
                    ')'
                  }
                />
              ))}
            </Space>
          ) : null}
        </Card>
      ),
    },
  ]
  return (
    <>
      {!(editor !== null && !(policies.isEnabled !== false && queryAccessDenied(policies))) && (
        <>
          {' '}
          {createPolicy.feedback}
          {updatePolicy.feedback}
          {deletePolicy.feedback}
          {compilePolicy.feedback}
          {previewPolicy.feedback}
          {analyzeConflicts.feedback}{' '}
        </>
      )}

      <ReadFeedback canRead={snapshot.isEnabled} query={snapshot} label="策略快照" />

      <Tabs items={tabs} />
      <Modal
        styles={operationModalStyles}
        style={{ top: 32 }}
        centered
        destroyOnHidden
        mask={{ closable: false }}
        open={editor !== null && !(policies.isEnabled !== false && queryAccessDenied(policies))}
        title={
          editor === 'new'
            ? t('networkAccess.policy.add', '新增策略')
            : t('networkAccess.policy.edit', '编辑策略')
        }
        confirmLoading={createPolicy.isPending || updatePolicy.isPending}
        okText={t('common.save', '保存')}
        cancelText={t('common.cancel', '取消')}
        onCancel={() => {
          if (!operationBlocked) (() => setEditor(null))()
        }}
        onOk={() => void submitPolicy()}
        closable={!operationBlocked}
        cancelButtonProps={{ disabled: operationBlocked }}
        okButtonProps={{ disabled: operationBlocked }}
      >
        {createPolicy.feedback}
        {updatePolicy.feedback}
        {deletePolicy.feedback}
        {compilePolicy.feedback}
        {previewPolicy.feedback}
        {analyzeConflicts.feedback}

        <Form form={policyForm} layout="vertical" disabled={operationBlocked}>
          <Form.Item
            name="name"
            label={t('networkAccess.name', '名称')}
            rules={[{ required: true, whitespace: true }]}
          >
            <Input maxLength={200} />
          </Form.Item>
          <Space wrap align="start">
            <Form.Item
              name="priority"
              label={t('networkAccess.policy.priority', '优先级')}
              rules={[{ required: true }]}
            >
              <InputNumber min={1} max={10000} precision={0} />
            </Form.Item>
            <Form.Item
              name="effect"
              label={t('networkAccess.policy.effect', '效果')}
              rules={[{ required: true }]}
            >
              <Select
                style={{ width: 120 }}
                options={['allow', 'deny'].map((value) => ({ value, label: value }))}
                onChange={(value) => {
                  if (value === 'deny') policyForm.setFieldValue('accessProfile', 'deny')
                  else if (policyForm.getFieldValue('accessProfile') === 'deny')
                    policyForm.setFieldValue('accessProfile', 'full')
                }}
              />
            </Form.Item>
            <Form.Item
              name="accessProfile"
              label={t('networkAccess.policy.profile', '访问等级')}
              rules={[{ required: true }]}
            >
              <Select
                style={{ width: 150 }}
                disabled={operationBlocked || effect === 'deny'}
                options={(effect === 'deny'
                  ? ['deny']
                  : ['onboarding', 'full', 'restricted', 'quarantine']
                ).map((value) => ({ value, label: value }))}
              />
            </Form.Item>
            <Form.Item
              name="enabled"
              label={t('networkAccess.policy.enabled', '启用')}
              valuePropName="checked"
            >
              <Switch />
            </Form.Item>
          </Space>
          <Form.Item
            name="usersText"
            label={t('networkAccess.policy.users', '用户 ID')}
            dependencies={['rolesText', 'teamsText', 'tagsText']}
            rules={[
              {
                validator: (_, value) =>
                  parseTextList(value).length ||
                  parseTextList(policyForm.getFieldValue('rolesText')).length ||
                  parseTextList(policyForm.getFieldValue('teamsText')).length ||
                  parseTextList(policyForm.getFieldValue('tagsText')).length
                    ? Promise.resolve()
                    : Promise.reject(
                        new Error(
                          t(
                            'networkAccess.policy.requiredSubject',
                            '用户、角色、团队或标签至少填写一项',
                          ),
                        ),
                      ),
              },
            ]}
          >
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item name="rolesText" label={t('networkAccess.policy.roles', '角色 ID')}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item name="teamsText" label={t('networkAccess.policy.teams', '团队 ID')}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item name="tagsText" label={t('networkAccess.policy.tags', '用户标签')}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item name="siteIdsText" label={t('networkAccess.policy.siteIds', '站点 ID')}>
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item
            name="resourceIdsText"
            label={t('networkAccess.policy.resourceIds', '资源 ID')}
          >
            <Input.TextArea autoSize={{ minRows: 2, maxRows: 4 }} />
          </Form.Item>
          <Form.Item name="modes" label={t('networkAccess.policy.modes', '访问形态')}>
            <Select mode="multiple" options={MODES.map((value) => ({ value, label: value }))} />
          </Form.Item>
          <Form.Item
            name="deviceStatuses"
            label={t('networkAccess.policy.deviceStatuses', '设备状态')}
          >
            <Select
              mode="multiple"
              options={['pending', 'active', 'quarantined', 'revoked'].map((value) => ({
                value,
                label: value,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="deviceManagementStates"
            label={t('networkAccess.device.managementState', '受管状态')}
            extra={t(
              'networkAccess.policy.managementHelp',
              '空列表表示不筛选状态，站点治理仍生效；受限非受管访问需指定状态、未保护资源和受限等级。',
            )}
          >
            <Select
              mode="multiple"
              allowClear
              options={(['managed', 'unmanaged', 'unknown'] as EndpointDeviceManagementState[]).map(
                (value) => ({ value, label: endpointManagementLabel(value, localeCode) }),
              )}
            />
          </Form.Item>
          <Form.Item
            name="postureStatuses"
            label={t('networkAccess.policy.postureStatuses', '合规状态')}
          >
            <Select
              mode="multiple"
              options={['compliant', 'non_compliant', 'unknown'].map((value) => ({
                value,
                label: value,
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </>
  )
}
