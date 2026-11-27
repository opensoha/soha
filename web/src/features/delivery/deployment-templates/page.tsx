import { MutationFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { TemplateMigrationFields, migrationPolicyInput } from '../template-migration'
import { lazy, Suspense, useState } from 'react'
import { App, Button, Form, Input, Modal, Space, Switch, Tabs, Typography } from 'antd'
import {
  CopyOutlined,
  DiffOutlined,
  HistoryOutlined,
  PlusOutlined,
  StopOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementKeywordField,
  ManagementQueryActions,
  ManagementState,
} from '@/components/management-list'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { MetadataTag, BooleanTag } from '@/components/status-tag'
import { tableColumnPresets } from '@/utils/table-columns'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import { TemplatePublicationStatus, TemplateVersionHistory } from '../template-versions'
import type { ServiceDeploymentTemplate, ServiceDeploymentTemplateInput } from '../types'
import { HelmSourceFields, parseHelmSource } from '../helm-fields'
import { deploymentTemplateDocument } from '../documents/model'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { TemplateSourcesButton } from '../template-sources/entry'
import { DocumentSourcePanel } from '../template-sources/source-panel'

const SourceEditor = lazy(() =>
  import('../documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentSourceEditor,
  })),
)
const ImportDialog = lazy(() =>
  import('../documents/import-dialog').then((module) => ({
    default: module.DeliveryDocumentImportDialog,
  })),
)

export function deploymentTemplateInput(
  item: ServiceDeploymentTemplate,
): ServiceDeploymentTemplateInput {
  const {
    key,
    name,
    description,
    source,
    parameterSchema,
    defaults,
    environmentOverrides,
    migrationPolicy,
    artifacts,
    health,
    enabled,
  } = item
  return {
    key,
    name,
    description,
    source,
    parameterSchema,
    defaults,
    environmentOverrides,
    migrationPolicy,
    artifacts,
    health,
    enabled,
  }
}

const emptyTemplate: ServiceDeploymentTemplateInput = {
  key: '',
  name: '',
  enabled: true,
  source: {
    renderer: 'raw_yaml',
    files: [
      {
        path: 'config.yaml',
        content: 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: service-config\ndata: {}\n',
      },
    ],
  },
  parameterSchema: { type: 'object', properties: {} },
  defaults: {},
  health: { mode: 'configuration_only', timeoutSeconds: 300 },
}

export function DeploymentTemplatesPage() {
  const { message, modal } = App.useApp()
  const [versionHistoryTargetId, setVersionHistoryTargetId] = useState('')
  const [updateCheckTargetId, setUpdateCheckTargetId] = useState('')
  const permissions = usePermissionSnapshot()
  const allowed = (action: string) =>
    hasPermission(permissions.data?.data, `delivery.deployment-templates.${action}`)
  const query = useQuery(deliveryQueries.deploymentTemplates.list(allowed('view')))
  const queryClient = useQueryClient()
  const create = useMutation(deliveryMutations.deploymentTemplates.create(queryClient))
  const update = useMutation(deliveryMutations.deploymentTemplates.update(queryClient))
  const publish = useMutation({
    ...deliveryMutations.deploymentTemplates.publish(queryClient),
    onError: (error: Error) => void message.error(error.message),
  })
  const deprecate = useMutation({
    ...deliveryMutations.deploymentTemplates.delete(queryClient),
    onError: (error: Error) => void message.error(error.message),
  })
  const [form] = Form.useForm<{ definition: ServiceDeploymentTemplateInput }>()
  const definition = Form.useWatch('definition', { form, preserve: true }) as
    | ServiceDeploymentTemplateInput
    | undefined
  const [editorMode, setEditorMode] = useState('form')
  const [sourceDirty, setSourceDirty] = useState(false)
  const [sourceGeneration, setSourceGeneration] = useState(0)
  const [importing, setImporting] = useState(false)
  const [editor, setEditor] = useState<{
    item?: ServiceDeploymentTemplate
    copiedFrom?: { id: string; revision: number }
    dirty: boolean
  }>()
  const source = useQuery(
    deliveryQueries.documents.source('DeploymentTemplate', editor?.item?.id ?? ''),
  )
  const sourceReadOnly = Boolean(editor?.item && (!source.isSuccess || source.data.association))
  const [search, setSearch] = useState('')
  const [appliedSearch, setAppliedSearch] = useState('')
  const [page, setPage] = useState(1)
  const applySearch = (value: string) => {
    setAppliedSearch(value.trim())
    setPage(1)
  }
  useUnsavedDocument(Boolean(editor?.dirty || sourceDirty))
  const openEditor = (item?: ServiceDeploymentTemplate, copy = false, helm = false) => {
    const original = item ? deploymentTemplateInput(item) : emptyTemplate
    const payload = helm
      ? {
          ...original,
          source: {
            renderer: 'helm' as const,
            helm: { repositoryUrl: '', chart: '', version: '', values: {} },
          },
        }
      : original
    form.setFieldsValue({
      definition: structuredClone(
        copy ? { ...payload, key: `${payload.key}-copy`, name: `${payload.name} 副本` } : payload,
      ),
    })
    setEditorMode('form')
    setSourceDirty(false)
    setSourceGeneration((value) => value + 1)
    create.reset()
    update.reset()
    setEditor({
      item: copy ? undefined : item,
      copiedFrom: copy && item ? { id: item.id, revision: item.revision } : undefined,
      dirty: false,
    })
  }
  const save = async () => {
    if (sourceDirty || sourceReadOnly) return
    await form.validateFields()
    const raw = form.getFieldValue('definition') as ServiceDeploymentTemplateInput
    let payload: ServiceDeploymentTemplateInput
    try {
      const input = { ...raw, migrationPolicy: migrationPolicyInput(raw.migrationPolicy) }
      payload =
        input.source.renderer === 'helm'
          ? { ...input, source: { ...input.source, helm: parseHelmSource(input.source.helm!) } }
          : input
    } catch (error) {
      if (error instanceof Error) message.error(error.message)
      return
    }
    const item = editor?.item
    const saved = item
      ? await update.mutateAsync({
          id: item.id,
          payload: { ...payload, expectedRevision: item.revision },
        })
      : await create.mutateAsync({ ...payload, copiedFrom: editor?.copiedFrom })
    form.setFieldValue('definition', deploymentTemplateInput(saved))
    setSourceGeneration((value) => value + 1)
    setEditor({ item: saved, dirty: false })
    message.success('草稿已保存')
  }
  const close = () => {
    if (editor?.dirty || sourceDirty)
      modal.confirm({ title: '放弃未保存的修改？', onOk: () => setEditor(undefined) })
    else setEditor(undefined)
  }
  if (!allowed('view'))
    return (
      <ManagementState
        kind={permissions.isLoading ? 'loading' : 'empty'}
        title="没有部署模板查看权限"
      />
    )
  return (
    <>
      <ManagementDataPage
        query={{
          onFinish: () => applySearch(search),
          actions: (
            <ManagementQueryActions
              onReset={() => {
                setSearch('')
                applySearch('')
              }}
            />
          ),
          children: (
            <ManagementKeywordField
              inputProps={{ 'aria-label': '搜索部署模板' }}
              placeholder="搜索名称或 Key"
              value={search}
              onChange={setSearch}
            />
          ),
        }}
        table={{
          rowKey: 'id',
          loading: query.isLoading,
          error: query.error,
          hasReadData: readableQueryData(query) !== undefined,
          dataSource: (readableQueryData(query) ?? []).filter((item) =>
            `${item.name} ${item.key}`.toLowerCase().includes(appliedSearch.toLowerCase()),
          ),
          enableDensity: true,
          onRefresh: () => query.refetch(),
          refreshing: query.isFetching,
          columnSettingPlacement: 'header',
          columnSettingIconOnly: true,
          pagination: { current: page, onPageChange: setPage, onPageSizeChange: () => setPage(1) },
          headerExtra: (
            <Space>
              <TemplateSourcesButton />
              <Button
                disabled={!allowed('create') && !allowed('update')}
                onClick={() => setImporting(true)}
              >
                导入文件
              </Button>
              {allowed('create') ? (
                <Button onClick={() => openEditor(undefined, false, true)}>新建 Helm 模板</Button>
              ) : null}
              {allowed('create') ? (
                <Button type="primary" icon={<PlusOutlined />} onClick={() => openEditor()}>
                  新建模板
                </Button>
              ) : null}
            </Space>
          ),
          columns: [
            {
              title: '名称',
              dataIndex: 'name',
              width: 180,
              render: (name: string, item: ServiceDeploymentTemplate) => (
                <Button
                  type="link"
                  style={{ padding: 0, height: 'auto', fontWeight: 600 }}
                  onClick={() => openEditor(item)}
                >
                  {name}
                </Button>
              ),
            },
            { title: 'Key', dataIndex: 'key', width: 150 },
            {
              title: '部署方式',
              key: 'renderer',
              width: 110,
              render: (_: unknown, item: ServiceDeploymentTemplate) => (
                <MetadataTag
                  label={
                    { raw_yaml: 'YAML', helm: 'Helm', kustomize: 'Kustomize' }[item.source.renderer]
                  }
                />
              ),
            },
            {
              title: '版本',
              key: 'version',
              width: 140,
              render: (_: unknown, item: ServiceDeploymentTemplate) => (
                <TemplatePublicationStatus template={item} />
              ),
            },
            {
              title: '启用',
              dataIndex: 'enabled',
              width: 70,
              render: (enabled: boolean) => <BooleanTag value={enabled} />,
            },
            {
              ...tableColumnPresets.action,
              title: '操作',
              key: 'actions',
              render: (_: unknown, item: ServiceDeploymentTemplate) => (
                <TableRowActionMenu
                  targetLabel={item.name}
                  items={[
                    {
                      key: 'versions',
                      label: '已发布版本',
                      icon: <HistoryOutlined />,
                      onClick: () => setVersionHistoryTargetId(item.id),
                    },
                    allowed('view')
                      ? {
                          key: 'updates',
                          label: '检查模板更新',
                          icon: <DiffOutlined />,
                          onClick: () => setUpdateCheckTargetId(item.id),
                        }
                      : null,
                    allowed('create')
                      ? {
                          key: 'copy',
                          label: '复制模板',
                          icon: <CopyOutlined />,
                          onClick: () => openEditor(item, true),
                        }
                      : null,
                    allowed('update') && item.publicationState === 'draft'
                      ? {
                          key: 'publish',
                          label: '发布模板',
                          icon: <UploadOutlined />,
                          disabled: publish.isPending,
                          onClick: () => {
                            modal.confirm({
                              title: '发布此草稿为新版本？',
                              content: '固定版本保持不变；已显式订阅的应用按各自策略采用。',
                              okText: '确认发布',
                              onOk: () =>
                                publish
                                  .mutateAsync({ id: item.id, expectedRevision: item.revision })
                                  .then(() => {
                                    message.success('新版本已发布')
                                  }),
                            })
                          },
                        }
                      : null,
                    allowed('delete') && item.publicationState !== 'deprecated'
                      ? { type: 'divider' }
                      : null,
                    allowed('delete') && item.publicationState !== 'deprecated'
                      ? {
                          key: 'deprecate',
                          label: '废弃模板',
                          icon: <StopOutlined />,
                          danger: true,
                          disabled: deprecate.isPending,
                          onClick: () => {
                            modal.confirm({
                              title: '废弃此模板？',
                              content: '保留历史版本供已有服务使用。',
                              okText: '确认废弃',
                              okButtonProps: { danger: true },
                              onOk: () => deprecate.mutateAsync(item.id),
                            })
                          },
                        }
                      : null,
                  ]}
                />
              ),
            },
          ],
        }}
        afterTable={
          <>
            <TemplateVersionHistory
              kind="deployment"
              templateId={versionHistoryTargetId}
              hideTrigger
              open={Boolean(versionHistoryTargetId)}
              onOpenChange={(next) => {
                if (!next) setVersionHistoryTargetId('')
              }}
            />
            <TemplateUpdateEntry
              kind="deployment"
              templateId={updateCheckTargetId}
              hideTrigger
              open={Boolean(updateCheckTargetId)}
              onOpenChange={(next) => {
                if (!next) setUpdateCheckTargetId('')
              }}
            />
            <Modal
              title={editor?.item ? editor.item.name : '新建部署模板'}
            open={Boolean(editor)}
            onCancel={close}
            width={900}
            footer={
              <Space>
                <Button onClick={close}>关闭</Button>
                {allowed(editor?.item ? 'update' : 'create') &&
                editor?.item?.publicationState !== 'deprecated' ? (
                  <Button
                    type="primary"
                    disabled={sourceDirty || sourceReadOnly}
                    loading={create.isPending || update.isPending}
                    onClick={() => void save().catch(() => {})}
                  >
                    保存草稿
                  </Button>
                ) : null}
              </Space>
            }
          >
            {editor?.item ? (
              <DocumentSourcePanel kind="DeploymentTemplate" id={editor.item.id} />
            ) : null}
            <MutationFeedback
              operations={[
                { mutation: create, label: '保存部署模板', target: editor?.item?.name },
                { mutation: update, label: '保存部署模板', target: editor?.item?.name },
              ]}
            />
            <Tabs
              activeKey={editorMode}
              onChange={(next) => {
                if (sourceDirty) return
                try {
                  if (next === 'source' && definition?.source.renderer === 'helm')
                    form.setFieldValue('definition', {
                      ...definition,
                      source: {
                        ...definition.source,
                        helm: parseHelmSource(definition.source.helm!),
                      },
                    })
                  setEditorMode(next)
                } catch (failure) {
                  message.error(failure instanceof Error ? failure.message : '请先修正 Helm Values')
                }
              }}
              items={[
                { key: 'form', label: '模板与参数', disabled: sourceDirty },
                { key: 'source', label: 'YAML / JSON' },
              ]}
            />
            <div style={{ display: editorMode === 'form' ? undefined : 'none' }}>
              <Form
                form={form}
                layout="vertical"
                disabled={
                  sourceReadOnly ||
                  !allowed(editor?.item ? 'update' : 'create') ||
                  editor?.item?.publicationState === 'deprecated' ||
                  create.isPending ||
                  update.isPending
                }
                onValuesChange={() =>
                  setEditor((current) => (current ? { ...current, dirty: true } : current))
                }
              >
                <Form.Item
                  name={['definition', 'key']}
                  label="Key"
                  rules={[{ required: true, whitespace: true }]}
                >
                  <Input />
                </Form.Item>
                <Form.Item
                  name={['definition', 'name']}
                  label="名称"
                  rules={[{ required: true, whitespace: true }]}
                >
                  <Input />
                </Form.Item>
                <TemplateMigrationFields path={['definition', 'migrationPolicy']} />
                <Form.Item name={['definition', 'description']} label="说明">
                  <Input.TextArea rows={2} />
                </Form.Item>
                <Form.Item name={['definition', 'enabled']} label="启用" valuePropName="checked">
                  <Switch />
                </Form.Item>
                {definition?.source.renderer === 'helm' ? (
                  <HelmSourceFields name={['definition', 'source', 'helm']} />
                ) : (
                  <Typography.Paragraph type="secondary">
                    在 YAML / JSON 中维护部署文件、参数约束和健康检查。
                  </Typography.Paragraph>
                )}
              </Form>
            </div>
            {editorMode === 'source' && definition ? (
              <Suspense fallback={<ManagementState kind="loading" />}>
                <SourceEditor
                  key={sourceGeneration}
                  kind="DeploymentTemplate"
                  value={deploymentTemplateDocument(definition)}
                  targetId={editor?.item?.id}
                  expectedRevision={editor?.item?.revision}
                  disabled={
                    sourceReadOnly ||
                    !allowed(editor?.item ? 'update' : 'create') ||
                    editor?.item?.publicationState === 'deprecated' ||
                    create.isPending ||
                    update.isPending
                  }
                  onDirtyChange={setSourceDirty}
                  onValidated={(document) => {
                    if (document.kind !== 'DeploymentTemplate') return
                    form.setFieldValue('definition', {
                      ...document.spec,
                      key: document.metadata.name,
                      name: document.metadata.displayName || document.metadata.name,
                      description: document.metadata.description ?? '',
                    })
                    setEditor((current) => (current ? { ...current, dirty: true } : current))
                  }}
                />
              </Suspense>
            ) : null}
            </Modal>
          </>
        }
      />
      {importing ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <ImportDialog onClose={() => setImporting(false)} />
        </Suspense>
      ) : null}
    </>
  )
}
