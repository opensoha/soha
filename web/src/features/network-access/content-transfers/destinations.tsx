import { useEffect, useState } from 'react'
import { Alert, Button, Form, Input, Popconfirm, Select, Space, Switch, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  DLPDataLevel,
  ManagedContentDestination,
  ManagedContentDestinationConfig,
  ManagedContentPolicy,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { DataLevelFields, DLPShadowPanel } from '@/components/content-dlp-policy-tools'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import {
  adminDestinationQuery,
  saveDestination,
  publishDestination,
  previewDestination,
} from './api'
import { managedContentKeys } from './keys'
import { useTransferContext } from './context'

interface Fields {
  name: string
  kind: 's3' | 'webdav'
  enabled: boolean
  storageIntegrationId?: string
  baseUrl?: string
  secretRef?: string
  privateCIDRsText?: string
  mode: ManagedContentPolicy['mode']
  detectors: ManagedContentPolicy['detectors']
  minimumDataLevel?: DLPDataLevel
  detectorVersion?: ManagedContentPolicy['detectorVersion']
  networkAction?: ManagedContentPolicy['networkAction']
  requireStepUp?: boolean
  rulesJSON: string
}
const defaults: Fields = {
  name: '',
  kind: 's3',
  enabled: true,
  mode: 'block',
  detectors: ['private_key', 'aws_access_key', 'credential_assignment'],
  rulesJSON: '[]',
  minimumDataLevel: 'public',
  detectorVersion: 'content-dlp-text-v1',
  networkAction: 'none',
}
function values(config: ManagedContentDestinationConfig): Fields {
  return {
    name: config.name,
    kind: config.kind,
    enabled: config.enabled,
    storageIntegrationId: config.storageIntegrationId,
    baseUrl: config.baseUrl,
    secretRef: config.secretRef,
    privateCIDRsText: config.privateCIDRs?.join('\n'),
    mode: config.policy.mode,
    minimumDataLevel: config.policy.minimumDataLevel ?? 'public',
    detectorVersion: config.policy.detectorVersion ?? 'content-dlp-text-v1',
    networkAction: config.policy.networkAction ?? 'none',
    requireStepUp: config.policy.requireStepUp ?? false,
    detectors: config.policy.detectors,
    rulesJSON: JSON.stringify(config.policy.rules, null, 2),
  }
}
function configuration(fields: Fields): ManagedContentDestinationConfig {
  if (typeof fields.enabled !== 'boolean')
    throw new Error('An explicit enabled decision is required.')
  const parsed: unknown = JSON.parse(fields.rulesJSON || '[]')
  if (!Array.isArray(parsed)) throw new Error('Rules must be a JSON array.')
  const rules = parsed.map((r: unknown): ManagedContentPolicy['rules'][number] => {
    if (
      typeof r !== 'object' ||
      r === null ||
      !('id' in r) ||
      !('expression' in r) ||
      typeof r.id !== 'string' ||
      typeof r.expression !== 'string' ||
      Object.keys(r).some((k) => k !== 'id' && k !== 'expression' && k !== 'dataLevel')
    )
      throw new Error('Each rule requires id, expression and optional dataLevel.')
    const level = 'dataLevel' in r ? r.dataLevel : undefined
    if (
      level !== undefined &&
      !['public', 'internal', 'confidential', 'restricted'].includes(String(level))
    )
      throw new Error('Invalid rule dataLevel.')
    return {
      id: r.id,
      expression: r.expression,
      ...(level ? { dataLevel: level as DLPDataLevel } : {}),
    }
  })
  return {
    name: fields.name.trim(),
    kind: fields.kind,
    enabled: Boolean(fields.enabled),
    policy: {
      mode: fields.mode,
      detectors: fields.detectors ?? [],
      rules,
      minimumDataLevel: fields.minimumDataLevel ?? 'public',
      detectorVersion: fields.detectorVersion ?? 'content-dlp-text-v1',
      networkAction: fields.networkAction ?? 'none',
      requireStepUp: fields.requireStepUp ?? false,
    },
    ...(fields.kind === 's3'
      ? { storageIntegrationId: fields.storageIntegrationId?.trim() }
      : {
          baseUrl: fields.baseUrl?.trim(),
          ...(fields.secretRef?.trim() ? { secretRef: fields.secretRef.trim() } : {}),
          privateCIDRs: (fields.privateCIDRsText ?? '')
            .split(/\r?\n/)
            .map((v) => v.trim())
            .filter(Boolean),
        }),
  }
}
export function Destinations() {
  const { viewer, scope, access, text } = useTransferContext()
  const client = useQueryClient()
  const query = useQuery(adminDestinationQuery(viewer, scope, access.destinationView))
  const data = readableQueryData(query)
  const [form] = Form.useForm<Fields>()
  const [id, setID] = useState<string>(() => crypto.randomUUID())
  const [revision, setRevision] = useState(0)
  const [dirty, setDirty] = useState(false)
  const kind = Form.useWatch('kind', form) ?? 's3'
  const mode = Form.useWatch('mode', form)
  const server = data?.find((d) => d.id === id)
  const conflict = revision > 0 && server !== undefined && revision !== server.revision
  useEffect(() => {
    if (server && !dirty) {
      form.setFieldsValue(values(server.draft))
      setRevision(server.revision)
    }
  }, [server, dirty, form])
  const accept = (d: ManagedContentDestination) => {
    setID(d.id)
    setRevision(d.revision)
    form.setFieldsValue(values(d.draft))
    setDirty(false)
    void client.invalidateQueries({ queryKey: managedContentKeys.all })
  }
  const save = useOperationMutation(
    {
      retry: false,
      mutationFn: (draft: ManagedContentDestinationConfig) =>
        saveDestination(viewer, id, revision, draft),
      onSuccess: accept,
    },
    {
      label: ['保存目的连接草稿', 'Save destination draft'],
      target: () => id,
      receipt: (d) => ({
        message: text('草稿已保存，尚未发布。', 'Draft saved; not published yet.'),
        evidence: `${d.id} · r${d.revision}`,
      }),
    },
  )
  const publish = useOperationMutation(
    { retry: false, mutationFn: () => publishDestination(viewer, id, revision), onSuccess: accept },
    {
      label: ['发布内容目的连接', 'Publish content destination'],
      target: () => id,
      receipt: (d) => ({
        message: text(
          '版本已发布，后续请求使用新规则。',
          'Version published for subsequent requests.',
        ),
        evidence: `${d.id} · v${d.publishedVersion}`,
      }),
    },
  )
  const busy = save.blocked || publish.blocked
  const blocked = busy || query.isError || query.isFetching || conflict
  const visible = revision === 0 || Boolean(server)
  const edit = (d: ManagedContentDestination) => {
    setID(d.id)
    setRevision(d.revision)
    setDirty(false)
    form.resetFields()
    form.setFieldsValue(values(d.draft))
    save.reset()
    publish.reset()
  }
  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <ReadFeedback
        canRead={access.destinationView}
        query={query}
        label={text('目的连接草稿与发布版本', 'Destination drafts and published versions')}
      />
      <Alert
        type="info"
        title={text(
          '编辑与发布分权；连接和策略一起版本化。',
          'Editing and publishing have separate permissions; connection and policy share a version.',
        )}
        description={text(
          '发布需要全主体范围。S3还需查看该存储集成的权限；WebDAV使用当前调用者的secret.use与connection/<目的ID>绑定。停用不删除已有文件；更换后端时保留旧记录并安排原位置的数据迁移。',
          'Publishing requires all-subject scope. S3 also requires view permission for the storage integration. WebDAV uses the current caller’s secret.use and connection/<destination ID> binding. Disabling does not delete files. Preserve old records and plan data migration before changing backends.',
        )}
      />
      <AdminTable
        rowKey="id"
        dataSource={data ?? []}
        loading={query.isFetching}
        onRefresh={() => void query.refetch()}
        pagination={{ pageSize: 10, showSizeChanger: false }}
        columns={[
          {
            title: text('连接', 'Destination'),
            key: 'name',
            render: (_: unknown, d: ManagedContentDestination) => (
              <Space orientation="vertical" size={2}>
                <span>{d.draft.name}</span>
                <Typography.Text copyable>{d.id}</Typography.Text>
              </Space>
            ),
          },
          {
            title: text('草稿 / 发布', 'Draft / Published'),
            key: 'version',
            render: (_: unknown, d: ManagedContentDestination) =>
              `r${d.revision} / v${d.publishedVersion}`,
          },
          {
            title: text('已发布状态', 'Published state'),
            key: 'state',
            render: (_: unknown, d: ManagedContentDestination) =>
              d.published
                ? `${d.published.kind} · ${d.published.enabled ? text('启用', 'Enabled') : text('停用', 'Disabled')} · ${d.published.policy.mode}`
                : text('尚未发布', 'Not published'),
          },
          {
            title: text('编辑', 'Edit'),
            key: 'edit',
            render: (_: unknown, d: ManagedContentDestination) => (
              <Button size="small" disabled={busy} onClick={() => edit(d)}>
                {text('查看 / 编辑', 'View / Edit')}
              </Button>
            ),
          },
        ]}
      />
      <Button
        disabled={!access.update || busy}
        onClick={() => {
          setID(crypto.randomUUID())
          setRevision(0)
          setDirty(false)
          form.resetFields()
          form.setFieldsValue(defaults)
          save.reset()
          publish.reset()
        }}
      >
        {text('新建目的连接', 'New destination')}
      </Button>
      {visible ? (
        <>
          <Typography.Text copyable>
            {text('目的连接ID（用于密钥绑定）', 'Destination ID (for secret binding)')}: {id}
          </Typography.Text>
          {conflict ? (
            <Alert
              type="warning"
              title={text('服务端版本已变化', 'The server version changed')}
              description={text(
                '本地编辑尚未提交。重新读取会丢弃本地草稿，再按最新版本编辑。',
                'Local edits are not submitted. Reload to discard this draft, then edit the latest version.',
              )}
            />
          ) : null}
          <Form
            form={form}
            layout="vertical"
            initialValues={defaults}
            disabled={!access.update || blocked}
            onValuesChange={(changed) => {
              setDirty(true)
              if (changed.mode && changed.mode !== 'block')
                form.setFieldValue('networkAction', 'none')
              if (changed.mode === 'audit') form.setFieldValue('requireStepUp', false)
            }}
            onFinish={(fields) => {
              try {
                const draft = configuration(fields)
                save.mutate(draft)
              } catch (error) {
                form.setFields([
                  {
                    name: 'rulesJSON',
                    errors: [error instanceof Error ? error.message : 'Invalid rules'],
                  },
                ])
              }
            }}
          >
            <Form.Item
              name="name"
              label={text('连接名称', 'Destination name')}
              rules={[{ required: true, whitespace: true }]}
            >
              <Input maxLength={120} />
            </Form.Item>
            <Form.Item
              name="kind"
              label={text('存储协议', 'Storage protocol')}
              rules={[{ required: true }]}
            >
              <Select
                options={[
                  {
                    value: 's3',
                    label: text('受管文件库（S3兼容）', 'Managed files (S3 compatible)'),
                  },
                  {
                    value: 'webdav',
                    label: text(
                      '批准网盘目录（HTTPS WebDAV）',
                      'Approved drive folder (HTTPS WebDAV)',
                    ),
                  },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="enabled"
              valuePropName="checked"
              label={text('允许新传输', 'Allow new transfers')}
            >
              <Switch />
            </Form.Item>
            {kind === 's3' ? (
              <Form.Item
                name="storageIntegrationId"
                label={text('现有存储集成ID', 'Existing storage integration ID')}
                extra={text(
                  '使用系统集成中已启用的S3连接；文件只写入独立受管目录。',
                  'Use an enabled S3 connection from system integrations; files use a dedicated managed namespace.',
                )}
                rules={[{ required: true }]}
              >
                <Input maxLength={128} />
              </Form.Item>
            ) : (
              <>
                <Form.Item
                  name="baseUrl"
                  label={text('HTTPS WebDAV既有目录', 'Existing HTTPS WebDAV folder')}
                  rules={[{ required: true, type: 'url' }]}
                >
                  <Input maxLength={2048} placeholder="https://drive.example.com/dav/approved/" />
                </Form.Item>
                <Form.Item
                  name="secretRef"
                  label={text(
                    '密钥引用（可选；匿名连接留空）',
                    'Secret reference (optional for anonymous connections)',
                  )}
                  extra={text(
                    'opaque密钥值为username/password JSON；只在密钥管理中保存正文，此处仅填soha://引用。',
                    'An opaque secret contains username/password JSON. Store its value only in secret management; enter a soha:// reference here.',
                  )}
                >
                  <Input maxLength={256} placeholder="soha://secrets/id/versions/1" />
                </Form.Item>
                <Form.Item
                  name="privateCIDRsText"
                  label={text(
                    '明确允许的内网CIDR（每行一个，可留空）',
                    'Explicit private CIDRs (one per line; optional)',
                  )}
                  extra={text(
                    '默认拒绝私网；不会允许环回、链路本地或元数据地址。',
                    'Private addresses are denied by default. Loopback, link-local and metadata addresses remain denied.',
                  )}
                >
                  <Input.TextArea rows={3} />
                </Form.Item>
              </>
            )}
            <Form.Item
              name="mode"
              label={text('内容模式', 'Content mode')}
              rules={[{ required: true }]}
            >
              <Select
                options={[
                  {
                    value: 'block',
                    label: text(
                      '阻断：命中或未完整检查不提交',
                      'Block: deny matches and incomplete inspection',
                    ),
                  },
                  {
                    value: 'warn',
                    label: text(
                      '警告：匹配需确认，未完整检查仍拒绝',
                      'Warn: confirm matches; reject incomplete inspection',
                    ),
                  },
                  {
                    value: 'audit',
                    label: text(
                      '审计：可能放行命中或未检查内容',
                      'Audit: may allow matched or uninspected content',
                    ),
                  },
                ]}
              />
            </Form.Item>
            <DataLevelFields allowArchives />
            <Form.Item
              name="requireStepUp"
              label={text('命中放行前额外验证 MFA', 'Require MFA before releasing a match')}
              valuePropName="checked"
              extra={text(
                '适用于警告/阻断；领取及使用须在近期完成二次验证的同一会话，不替代原审批。',
                'For warn/block. Claim and use require the same recently verified session; existing approval remains required.',
              )}
            >
              <Switch disabled={!access.update || blocked || mode === 'audit'} />
            </Form.Item>
            <Form.Item
              name="networkAction"
              label={text('命中阻断后的网络联动', 'Network response after a matched block')}
              extra={text(
                '需要独立网络联动权限及全主体范围。限制只在全部关联命中均获批准后解除，旧会话不会恢复。',
                'Requires independent linkage permission and all-subject scope. All linked findings must be approved before release; old sessions stay revoked.',
              )}
            >
              <Select
                disabled={!access.link || mode !== 'block' || blocked}
                options={[
                  { value: 'none', label: text('不联动', 'None') },
                  {
                    value: 'revoke_network',
                    label: text('限制该用户受管网络', 'Restrict the user’s managed network access'),
                  },
                ]}
              />
            </Form.Item>
            <Form.Item name="detectors" label={text('内置检测器', 'Built-in detectors')}>
              <Select
                mode="multiple"
                options={[
                  { value: 'private_key', label: text('私钥', 'Private keys') },
                  { value: 'aws_access_key', label: text('AWS访问密钥', 'AWS access keys') },
                  {
                    value: 'credential_assignment',
                    label: text('凭据赋值', 'Credential assignments'),
                  },
                ]}
              />
            </Form.Item>
            <Form.Item
              name="rulesJSON"
              label={text(
                '自定义RE2规则（JSON数组，最多16条）',
                'Custom RE2 rules (JSON array, up to 16)',
              )}
              extra='[{"id":"sensitive","expression":"pattern","dataLevel":"confidential"}]'
            >
              <Input.TextArea rows={5} />
            </Form.Item>
            <Button
              type="primary"
              htmlType="submit"
              disabled={!access.update || blocked}
              loading={save.isPending}
            >
              {text('保存草稿', 'Save draft')}
            </Button>
          </Form>
          <DLPShadowPanel
            key={`${id}:${revision}`}
            revision={revision}
            disabled={!access.update || blocked || dirty || revision < 1}
            evaluate={(input) => previewDestination(viewer, id, input)}
          />
          {save.feedback}
          {publish.feedback}
          <Space wrap>
            <Popconfirm
              title={text(
                '发布当前已保存的连接和规则？',
                'Publish the currently saved connection and rules?',
              )}
              description={text(
                '可能允许内容离开Core；审计模式允许未检查内容。已有文件不会自动搬迁，旧确认不能跨版本使用。',
                'Content may leave Core. Audit can allow uninspected content. Existing files are not automatically moved; old acknowledgements cannot cross versions.',
              )}
              onConfirm={() => publish.mutate()}
            >
              <Button
                disabled={!access.publish || blocked || dirty || revision < 1}
                loading={publish.isPending}
              >
                {text('发布已保存版本', 'Publish saved version')}
              </Button>
            </Popconfirm>
            <Button
              disabled={busy}
              onClick={() => {
                setDirty(false)
                void query.refetch()
              }}
            >
              {text('重新读取（丢弃本地草稿）', 'Reload (discard local draft)')}
            </Button>
          </Space>
        </>
      ) : null}
    </Space>
  )
}
