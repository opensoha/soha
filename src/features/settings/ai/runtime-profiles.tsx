import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import type { WorkbenchRuntimeProfile, WorkbenchRuntimeProfileUpdate } from '@/features/copilot'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, Form, Input, InputNumber, Modal, Select, Space, Switch, Typography } from 'antd'
import { useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import { settingsMutations } from '../mutations'
import { settingsQueries } from '../queries'
import { SettingsCard } from '../shared/components'
import type { WorkbenchRuntimeAsset } from '../types'

const idPattern = /^[a-z0-9][a-z0-9-]{0,47}$/
const scopes = [
  { value: 'chat', label: 'Chat' },
  { value: 'heya', label: 'Heya' },
]
const assetKinds = { mcp: 'MCP', skill: 'Skill', hook: 'Hook' }
const defaultProfiles = ['chat-default', 'heya-default']
const effortLabels = { auto: '自动', low: '轻度', medium: '标准', high: '深度' }
type AssetReference = Omit<WorkbenchRuntimeProfile['assets'][number], 'required'>
type ProfileDraft = Omit<WorkbenchRuntimeProfileUpdate, 'expectedRevision' | 'assets'> & {
  assets: { reference: AssetReference; required: boolean }[]
}

const assetKey = ({ kind, id, version }: AssetReference) => `${kind}:${id}:${version}`

function assetFitsScope(asset: WorkbenchRuntimeAsset, scope: ProfileDraft['scope']) {
  return asset.transport !== 'stdio' || scope === 'heya'
}

function draftFromRevision(id: string, revision?: WorkbenchRuntimeProfile): ProfileDraft {
  return {
    name: revision?.name ?? '',
    scope: revision?.scope ?? (id === 'heya-default' ? 'heya' : 'chat'),
    enabled: revision?.enabled ?? true,
    assets: revision?.assets?.map(({ required, ...reference }) => ({ reference, required })) ?? [],
    modelPreferences: revision?.modelPreferences,
    maxOutputTokens: revision?.maxOutputTokens,
  }
}

export function RuntimeProfilesSettings({
  owner,
  canManage,
  publicModels,
}: {
  owner: string
  canManage: boolean
  publicModels: string[]
}) {
  const queryClient = useQueryClient()
  const [params, setParams] = useSearchParams()
  const requestedId = params.get('runtimeProfile') || ''
  const id = idPattern.test(requestedId) ? requestedId : ''
  const [version, setVersion] = useState<{ id: string; revision: number }>()
  const [editor, setEditor] = useState<{
    id: string
    expectedRevision: number
    draft: ProfileDraft
  }>()
  const history = useQuery(settingsQueries.ai.runtimeProfileHistory(owner, id))
  const catalog = useQuery(settingsQueries.ai.runtimeAssets(owner, Boolean(editor) && canManage))
  const modelCatalog = useQuery(settingsQueries.ai.workbenchCatalog(Boolean(editor) && canManage))
  const modelData = readableQueryData(modelCatalog)
  const reasoningEfforts = (model?: string) =>
    modelData?.modelOptions?.find((item) => item.publicModel === (model || modelData.defaultPublicModel))
      ?.reasoningEfforts ?? []
  const registeredAssets = new Map(
    (readableQueryData(catalog) ?? []).map((asset) => [assetKey(asset), asset]),
  )
  const historicalAssets = new Map(
    editor?.draft.assets.map(({ reference }) => [assetKey(reference), reference]),
  )
  const assetReferences = new Map<string, AssetReference>([
    ...historicalAssets,
    ...registeredAssets,
  ])
  const revisions = readableQueryData(history) ?? []
  const latest = revisions[0]
  const viewed =
    (version?.id === id && revisions.find((item) => item.revision === version.revision)) || latest
  const publish = useOperationMutation(
    settingsMutations.ai.publishRuntimeProfile(queryClient, owner),
    {
      label: ['发布运行配置', 'Publish runtime profile'],
      target: (input) => input.id,
      receipt: (profile) => ({
        message: `已发布 ${profile.name} · v${profile.revision}，下一轮运行时生效。`,
      }),
    },
  )
  const load = (next: string) => {
    const target = next.trim()
    if (!idPattern.test(target)) return
    setParams((current) => {
      const updated = new URLSearchParams(current)
      updated.set('runtimeProfile', target)
      return updated
    })
  }
  const blocked = !canManage || !history.isSuccess || queryAccessDenied(publish) || publish.blocked
  const modelOptions = [
    ...new Set([
      ...(editor?.draft.modelPreferences?.publicModel
        ? [editor.draft.modelPreferences.publicModel]
        : []),
      ...publicModels,
    ]),
  ].map((model) => ({ label: model, value: model }))
  return (
    <SettingsCard title="Workbench 运行配置">
      <Space orientation="vertical" size="middle" style={{ width: '100%' }}>
        <Typography.Text type="secondary">
          为 Chat 或 Heya 发布模型、预算和指定版本的能力引用。会话从下一轮解析，新版本保留发布历史。
        </Typography.Text>
        <Space wrap>
          <Form layout="inline" onFinish={({ profileId }) => load(profileId)}>
            <Form.Item
              name="profileId"
              rules={[
                {
                  required: true,
                  pattern: idPattern,
                  message: '输入 1–48 位小写字母、数字或连字符',
                },
              ]}
            >
              <Input placeholder="配置 ID" aria-label="运行配置 ID" maxLength={48} />
            </Form.Item>
            <Button htmlType="submit">查看配置</Button>
          </Form>
          {defaultProfiles.map((profileId, index) => (
            <Button key={profileId} onClick={() => load(profileId)}>
              {index ? 'Heya 默认配置' : 'Chat 默认配置'}
            </Button>
          ))}
        </Space>
        <ReadFeedback query={history} label="运行配置历史" canRead={Boolean(id)} />
        {id && history.isFetching && (
          <Typography.Text type="secondary">正在读取 {id}…</Typography.Text>
        )}
        {id && history.isSuccess && (
          <Space orientation="vertical" size={6}>
            <Typography.Text strong>{id}</Typography.Text>
            {viewed ? (
              <>
                <Space wrap>
                  <Select
                    aria-label="已发布版本"
                    value={viewed.revision}
                    onChange={(revision) => setVersion({ id, revision })}
                    options={revisions.map((item) => ({
                      value: item.revision,
                      label: `v${item.revision} · ${item.name}${item === latest ? '（当前）' : ''}`,
                    }))}
                  />
                  <StatusTag
                    label={viewed.enabled ? '已启用' : '已停用'}
                    value={viewed.enabled ? 'enabled' : 'disabled'}
                  />
                  <span>{viewed.scope === 'heya' ? 'Heya' : 'Chat'}</span>
                </Space>
                <Typography.Text type="secondary">
                  模型 · {viewed.modelPreferences?.publicModel || '工作台默认'} · 输出上限 ·{' '}
                  {viewed.maxOutputTokens || '默认'}
                </Typography.Text>
                {viewed.assets?.map((asset) => (
                  <span key={`${asset.kind}:${asset.id}`}>
                    {assetKinds[asset.kind]} · {asset.id} @ {asset.version} ·{' '}
                    {asset.required ? '必需' : '可选'}
                  </span>
                ))}
                {!viewed.assets?.length && (
                  <Typography.Text type="secondary">此版本无附加能力。</Typography.Text>
                )}
              </>
            ) : (
              <Typography.Text type="secondary">此配置尚未发布。</Typography.Text>
            )}
            {canManage && (
              <Button
                disabled={blocked}
                onClick={() =>
                  setEditor({
                    id,
                    expectedRevision: latest?.revision ?? 0,
                    draft: draftFromRevision(id, viewed),
                  })
                }
              >
                {latest
                  ? viewed === latest
                    ? '编辑并发布新版本'
                    : '基于此版本重新发布'
                  : '创建并发布'}
              </Button>
            )}
          </Space>
        )}
        {!editor && publish.feedback}
      </Space>
      <Modal
        title={editor ? `发布运行配置 · ${editor.id}` : '发布运行配置'}
        open={Boolean(editor) && canManage}
        footer={null}
        onCancel={() => {
          if (!publish.isPending) setEditor(undefined)
        }}
      >
        {editor && (
          <Form<ProfileDraft>
            layout="vertical"
            initialValues={editor.draft}
            onFinish={(draft) => {
              if (
                blocked ||
                editor.id !== id ||
                editor.expectedRevision !== (latest?.revision ?? 0)
              )
                return
              const { modelPreferences, maxOutputTokens } = draft
              publish.mutate(
                {
                  id: editor.id,
                  profile: {
                    expectedRevision: editor.expectedRevision,
                    name: draft.name.trim(),
                    scope: draft.scope,
                    enabled: draft.enabled,
                    assets: (draft.assets ?? []).map(({ reference, required }) => ({
                      kind: reference.kind,
                      id: reference.id,
                      version: reference.version,
                      required,
                    })),
                    ...(modelPreferences?.publicModel || modelPreferences?.reasoningEffort
                      ? { modelPreferences }
                      : {}),
                    ...(maxOutputTokens ? { maxOutputTokens } : {}),
                  },
                },
                {
                  onSuccess: () => {
                    setEditor(undefined)
                    setVersion(undefined)
                  },
                },
              )
            }}
          >
            <Form.Item
              name="name"
              label="配置名称"
              rules={[{ required: true, whitespace: true, max: 100 }]}
            >
              <Input maxLength={100} />
            </Form.Item>
            <Form.Item name="scope" label="作用域" rules={[{ required: true }]}>
              <Select
                options={scopes}
                disabled={editor.expectedRevision > 0 || defaultProfiles.includes(editor.id)}
              />
            </Form.Item>
            <Form.Item name="enabled" label="允许会话使用" valuePropName="checked">
              <Switch />
            </Form.Item>
            <Form.Item name={['modelPreferences', 'publicModel']} label="模型">
              <Select allowClear options={modelOptions} placeholder="跟随工作台默认" />
            </Form.Item>
            <Form.Item
              noStyle
              shouldUpdate={(previous, current) =>
                previous.modelPreferences?.publicModel !== current.modelPreferences?.publicModel
              }
            >
              {({ getFieldValue }) => {
                const modelField = ['modelPreferences', 'publicModel']
                const efforts = reasoningEfforts(getFieldValue(modelField))
                return (
                  <Form.Item
                    name={['modelPreferences', 'reasoningEffort']}
                    label="推理强度"
                    dependencies={[modelField]}
                    rules={[{
                      validator: async (_, effort) => {
                        if (effort && effort !== 'auto' &&
                          !reasoningEfforts(getFieldValue(modelField)).includes(effort)) {
                          throw new Error('当前模型未确认支持此推理强度，请改选可用强度或清除。')
                        }
                      },
                    }]}
                  >
                    <Select
                      allowClear
                      placeholder="跟随工作台默认"
                      loading={modelCatalog.isFetching}
                      options={['auto' as const, ...efforts].map((value) => ({
                        label: effortLabels[value],
                        value,
                      }))}
                    />
                  </Form.Item>
                )
              }}
            </Form.Item>
            <Form.Item name="maxOutputTokens" label="输出上限（tokens）">
              <InputNumber min={1} max={4096} precision={0} placeholder="默认" />
            </Form.Item>
            <Typography.Paragraph type="secondary">
              新增能力请选择注册表中已启用的精确版本。历史引用可保留或移除。
            </Typography.Paragraph>
            <ReadFeedback query={catalog} label="运行资产目录" />
            <Form.Item
              noStyle
              shouldUpdate={(previous, current) => previous.scope !== current.scope}
            >
              {({ getFieldValue }) => {
                const scope = getFieldValue('scope') as ProfileDraft['scope']
                const options = Array.from(assetReferences, ([value, reference]) => {
                  const asset = registeredAssets.get(value)
                  const note = !catalog.isSuccess
                    ? '目录未就绪'
                    : !asset
                      ? '历史引用'
                      : !asset.enabled
                        ? '已停用'
                        : !assetFitsScope(asset, scope)
                          ? '仅 Heya'
                          : asset.executionPosition === 'personal-docker'
                            ? '需个人环境绑定'
                            : undefined
                  return {
                    value,
                    label: [
                      `${assetKinds[reference.kind]} · ${reference.id} @ ${reference.version}`,
                      note,
                    ]
                      .filter(Boolean)
                      .join(' · '),
                    disabled:
                      !catalog.isSuccess || !asset?.enabled || !assetFitsScope(asset, scope),
                  }
                })
                return (
                  <Form.List
                    name="assets"
                    rules={[
                      {
                        validator: async (_, items: ProfileDraft['assets'] = []) => {
                          const selected = items.flatMap((item) =>
                            item?.reference ? [item.reference] : [],
                          )
                          if (
                            new Set(selected.map(({ kind, id }) => `${kind}:${id}`)).size !==
                            selected.length
                          ) {
                            throw new Error('同一能力只能选择一个版本')
                          }
                          if (
                            selected.some((reference) => {
                              const key = assetKey(reference)
                              if (historicalAssets.has(key)) return false
                              const asset = registeredAssets.get(key)
                              return (
                                !catalog.isSuccess ||
                                !asset?.enabled ||
                                !assetFitsScope(asset, scope)
                              )
                            })
                          ) {
                            throw new Error('请选择当前作用域中已启用的资产版本')
                          }
                        },
                      },
                    ]}
                  >
                    {(fields, { add, remove }, { errors }) => (
                      <>
                        {fields.map((field, index) => (
                          <Space key={field.key} wrap align="baseline">
                            <Form.Item
                              name={[field.name, 'reference']}
                              label={`资产版本 ${index + 1}`}
                              rules={[{ required: true, message: '请选择资产版本' }]}
                              getValueProps={(reference?: AssetReference) => ({
                                value: reference && assetKey(reference),
                              })}
                              normalize={(value: string) => assetReferences.get(value)}
                            >
                              <Select
                                options={options}
                                showSearch={{ optionFilterProp: 'label' }}
                                placeholder="选择已注册版本"
                                style={{ width: 'min(290px, 65vw)' }}
                              />
                            </Form.Item>
                            <Form.Item
                              name={[field.name, 'required']}
                              label="必需"
                              valuePropName="checked"
                            >
                              <Switch />
                            </Form.Item>
                            <Button
                              type="text"
                              onClick={() => remove(field.name)}
                              aria-label={`移除能力 ${index + 1}`}
                            >
                              移除
                            </Button>
                          </Space>
                        ))}
                        <Button
                          onClick={() => add({ required: false })}
                          loading={catalog.isLoading}
                          disabled={
                            fields.length >= 16 || !options.some((option) => !option.disabled)
                          }
                        >
                          添加能力引用
                        </Button>
                        <Form.ErrorList errors={errors} />
                      </>
                    )}
                  </Form.List>
                )
              }}
            </Form.Item>
            <Space orientation="vertical" style={{ marginBlockStart: 16 }}>
              {publish.feedback}
              {editor.expectedRevision !== (latest?.revision ?? 0) && (
                <Typography.Text type="warning">
                  发布版本已变化。请关闭此表单，从最新版本重新打开编辑。
                </Typography.Text>
              )}
              <Button
                type="primary"
                htmlType="submit"
                loading={publish.isPending}
                disabled={blocked || editor.expectedRevision !== (latest?.revision ?? 0)}
              >
                发布新版本
              </Button>
            </Space>
          </Form>
        )}
      </Modal>
    </SettingsCard>
  )
}
