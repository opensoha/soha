import { lazy, Suspense, useRef, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Card,
  Descriptions,
  Popconfirm,
  Space,
  Steps,
  Tabs,
  Typography,
} from 'antd'
import { CopyOutlined, EditOutlined, PlusOutlined, UploadOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementSearchableListPane,
  ManagementState,
  TemplateDesignerShell,
} from '@/components/management-list'
import { normalizeReleaseDagDefinition } from '@/components/release-flow-dag-definition'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import { deliveryModeLabels, deliveryStageLabels } from '../batches/model'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { TemplatePublicationStatus } from '../template-versions'
import { TemplateUsageImpactPanel } from '../template-usage-impact'
import type { DeliveryBatchTemplateDefinition, WorkflowTemplate } from '../types'
import './styles.css'
import { workflowTemplateDocument } from '../documents/model'
import { TemplateSourcesButton } from '../template-sources/entry'
import { DocumentSourcePanel } from '../template-sources/source-panel'

const ImportDialog = lazy(() =>
  import('../documents/import-dialog').then((module) => ({
    default: module.DeliveryDocumentImportDialog,
  })),
)
const ExportView = lazy(() =>
  import('../documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentExportView,
  })),
)

const DraftEditor = lazy(() =>
  import('./draft-editor').then((module) => ({ default: module.WorkflowTemplateDraftEditor })),
)
const ReadView = lazy(() =>
  import('../documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentReadView,
  })),
)
const UseVersionDialog = lazy(() =>
  import('./use-version-dialog').then((module) => ({ default: module.UseWorkflowTemplateVersion })),
)

export function WorkflowTemplatesPage() {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const navigate = useNavigate()
  const client = useQueryClient()
  const permissionQuery = usePermissionSnapshot()
  const permissions = permissionQuery.data?.data
  const canView = hasPermission(permissions, 'delivery.workflow-templates.view')
  const canCreate = hasPermission(permissions, 'delivery.workflow-templates.create')
  const canUpdate = hasPermission(permissions, 'delivery.workflow-templates.update')
  const canDelete = hasPermission(permissions, 'delivery.workflow-templates.delete')
  const [search, setSearch] = useSearchParams()
  const [text, setText] = useState('')
  const [draft, setDraft] = useState<{
    template?: WorkflowTemplate
    copiedFrom?: { id: string; revision: number; version?: number }
  }>()
  const [usingVersion, setUsingVersion] = useState<WorkflowTemplate>()
  const [tab, setTab] = useState('definition')
  const [importing, setImporting] = useState(false)
  const [draftGeneration, setDraftGeneration] = useState(0)
  const templates = useQuery(deliveryQueries.workflowTemplates.list(canView))
  const items = (readableQueryData(templates) ?? []).filter(
    (item) => !item.category?.startsWith('application:'),
  )
  const requestedId = search.get('templateId')
  const selected = requestedId ? items.find((item) => item.id === requestedId) : items[0]
  const source = useQuery(deliveryQueries.documents.source('WorkflowTemplate', selected?.id ?? ''))
  const canEdit = canUpdate && source.isSuccess && !source.data.association
  const requestedVersion = search.get('version')
  const invalidVersion =
    requestedVersion !== null &&
    (!/^\d+$/.test(requestedVersion) ||
      !Number.isSafeInteger(Number(requestedVersion)) ||
      Number(requestedVersion) < 1)
  const selectedVersion =
    requestedVersion && !invalidVersion ? Number(requestedVersion) : selected?.publishedVersion || 0
  const version = useQuery(
    deliveryQueries.workflowTemplates.version(
      selected?.id ?? '',
      selectedVersion,
      canView && !invalidVersion,
    ),
  )
  const versions = useQuery(
    deliveryQueries.workflowTemplates.versions(selected?.id ?? '', canView && tab === 'versions'),
  )
  const usage = useQuery(
    deliveryQueries.workflowTemplates.usage(selected?.id ?? '', canView && tab === 'usage'),
  )
  const publish = useMutation(deliveryMutations.workflowTemplates.publish(client))
  const deprecate = useMutation(deliveryMutations.workflowTemplates.delete(client))
  const submitting = useRef(false)
  const locked =
    publish.isPending ||
    deprecate.isPending ||
    writeOutcomeUnknown(publish.error) ||
    writeOutcomeUnknown(deprecate.error)
  const submit = async (action: 'publish' | 'deprecate') => {
    if (submitting.current || locked || !selected || templates.isError || source.isError) return
    if (
      (action === 'publish' && (!canUpdate || !selected.revision)) ||
      (action === 'deprecate' && !canDelete)
    )
      return
    submitting.current = true
    try {
      if (action === 'publish') {
        await publish.mutateAsync({ id: selected.id, expectedRevision: selected.revision! })
        updateSearch({ version: undefined })
        void message.success('版本已发布')
      } else await deprecate.mutateAsync(selected.id)
    } catch {
      /* Persistent feedback keeps the submitted target. */
    } finally {
      submitting.current = false
    }
  }
  const shown = selectedVersion > 0 ? readableQueryData(version) : selected
  const recipe =
    shown?.definition?.mode === 'delivery_batch'
      ? (shown.definition as unknown as DeliveryBatchTemplateDefinition)
      : undefined
  const dag = recipe ? undefined : normalizeReleaseDagDefinition(shown?.definition)
  const updateSearch = (patch: Record<string, string | undefined>) =>
    setSearch(
      (current) => {
        const next = new URLSearchParams(current)
        for (const [key, value] of Object.entries(patch)) {
          if (value) next.set(key, value)
          else next.delete(key)
        }
        return next
      },
      { replace: true },
    )
  if (permissionQuery.isLoading) return <ManagementState kind="loading" />
  if (permissionQuery.isError)
    return (
      <ManagementState
        kind="error"
        title="权限读取失败"
        actions={<Button onClick={() => void permissionQuery.refetch()}>重试</Button>}
      />
    )
  if (!canView) return <ManagementState kind="no-permission" title="无权查看流程模板" />
  const content = templates.isLoading ? (
    <ManagementState kind="loading" />
  ) : templates.isError && !readableQueryData(templates) ? (
    <ManagementState
      kind="error"
      title="模板读取失败"
      actions={<Button onClick={() => void templates.refetch()}>重试</Button>}
    />
  ) : invalidVersion ? (
    <ManagementState kind="error" title="模板版本必须是正整数" />
  ) : !selected ? (
    <ManagementState
      kind="not-configured"
      title={requestedId ? '模板不存在或无权查看' : '暂无流程模板'}
    />
  ) : selectedVersion > 0 && version.isLoading ? (
    <ManagementState kind="loading" />
  ) : version.isError && !readableQueryData(version) ? (
    <ManagementState
      kind="error"
      title="模板版本读取失败"
      actions={<Button onClick={() => void version.refetch()}>重试</Button>}
    />
  ) : (
    <Card
      className="soha-workflow-template-browser"
      title={shown?.name}
      extra={
        <Space>
          <TemplatePublicationStatus template={shown} />
          <TemplateUpdateEntry
            kind="workflow"
            templateId={selected.id}
            initialVersion={selectedVersion > 0 ? selectedVersion : undefined}
          />
          {selectedVersion > 0 ? (
            <Button
              type="primary"
              disabled={
                selected.publicationState === 'deprecated' ||
                !selected.enabled ||
                !(recipe
                  ? hasPermission(permissions, 'delivery.workflows.trigger')
                  : hasPermission(permissions, 'delivery.application-environments.update'))
              }
              onClick={() => {
                if (recipe && shown)
                  navigate(
                    `/release-board?${new URLSearchParams({ create: 'workflow', templateId: shown.id, templateVersion: String(selectedVersion) })}`,
                  )
                else if (shown) setUsingVersion(shown)
              }}
            >
              使用此版本
            </Button>
          ) : null}
        </Space>
      }
    >
      {!selectedVersion ? (
        <Alert type="info" showIcon title="尚未发布，当前显示草稿内容" />
      ) : selected.publicationState === 'draft' ? (
        <Alert type="info" showIcon title={`正在浏览已发布 v${selectedVersion}，另有未发布草稿`} />
      ) : null}
      <Typography.Paragraph type="secondary">{shown?.description}</Typography.Paragraph>
      <Tabs
        activeKey={tab}
        onChange={setTab}
        items={[
          {
            key: 'definition',
            label: '流程',
            children: (
              <Space orientation="vertical" size={24} style={{ width: '100%' }}>
                {recipe ? (
                  <>
                    <Steps
                      current={-1}
                      items={recipe.stages.map((stage) => ({
                        title: deliveryStageLabels[stage],
                        status: 'wait',
                      }))}
                    />
                    <Descriptions
                      column={1}
                      items={[
                        {
                          key: 'mode',
                          label: '执行方式',
                          children: deliveryModeLabels[recipe.executionMode],
                        },
                        {
                          key: 'stop',
                          label: '失败处理',
                          children: recipe.stopOnFailure
                            ? '失败后停止后续目标'
                            : '继续其他可执行目标',
                        },
                        {
                          key: 'concurrency',
                          label: '最大并行数',
                          children: recipe.maxConcurrency,
                        },
                      ]}
                    />
                  </>
                ) : (
                  <div className="soha-workflow-template-node-list">
                    {dag?.nodes.map((node) => (
                      <Card size="small" key={node.id}>
                        <Space>
                          <Typography.Text strong>{node.name}</Typography.Text>
                          <MetadataTag label={node.type} />
                        </Space>
                        <Typography.Paragraph type="secondary">{node.id}</Typography.Paragraph>
                        {dag.edges.filter((edge) => edge.target === node.id).length ? (
                          <Typography.Text type="secondary">
                            依赖：
                            {dag.edges
                              .filter((edge) => edge.target === node.id)
                              .map(
                                (edge) =>
                                  dag.nodes.find((parent) => parent.id === edge.source)?.name ||
                                  edge.source,
                              )
                              .join('、')}
                          </Typography.Text>
                        ) : null}
                      </Card>
                    ))}
                  </div>
                )}
              </Space>
            ),
          },
          {
            key: 'source',
            label: 'YAML / JSON',
            children:
              selectedVersion > 0 ? (
                <Suspense fallback={<ManagementState kind="loading" />}>
                  <ExportView
                    kind="WorkflowTemplate"
                    id={selected.id}
                    version={selectedVersion}
                    legacy={shown}
                  />
                </Suspense>
              ) : (
                <Suspense fallback={<ManagementState kind="loading" />}>
                  <ReadView value={shown ? workflowTemplateDocument(shown) : {}} />
                </Suspense>
              ),
          },
          {
            key: 'versions',
            label: '版本',
            children: versions.isLoading ? (
              <ManagementState compact kind="loading" />
            ) : versions.isError && !readableQueryData(versions) ? (
              <ManagementState
                compact
                kind="error"
                actions={<Button onClick={() => void versions.refetch()}>重试</Button>}
              />
            ) : readableQueryData(versions)?.length ? (
              <Space orientation="vertical" style={{ width: '100%' }}>
                {readableQueryData(versions)!.map((item) => (
                  <Card key={item.publishedVersion} size="small">
                    <Space wrap>
                      <Button
                        onClick={() => {
                          updateSearch({
                            templateId: selected.id,
                            version: String(item.publishedVersion),
                          })
                          setTab('definition')
                        }}
                      >
                        v{item.publishedVersion} · {item.name}
                      </Button>
                      <Typography.Text code>{item.contentDigest}</Typography.Text>
                    </Space>
                  </Card>
                ))}
              </Space>
            ) : (
              <ManagementState compact title="尚未发布" />
            ),
          },
          {
            key: 'origin',
            label: '来源',
            children: (
              <DocumentSourcePanel
                kind="WorkflowTemplate"
                id={selected.id}
                version={selectedVersion || undefined}
              />
            ),
          },
          {
            key: 'usage',
            label: '引用与使用',
            children: (
              <>
                {!recipe ? (
                  <Typography.Paragraph>
                    在应用的环境工作流中选择此模板和固定版本。
                  </Typography.Paragraph>
                ) : null}
                {usage.isError && !readableQueryData(usage) ? (
                  <ManagementState
                    compact
                    kind="error"
                    title="引用读取失败"
                    actions={<Button onClick={() => void usage.refetch()}>重试</Button>}
                  />
                ) : (
                  <TemplateUsageImpactPanel
                    localeCode={localeCode}
                    loading={usage.isLoading}
                    usage={readableQueryData(usage)}
                    onNavigate={navigate}
                  />
                )}
              </>
            ),
          },
        ]}
      />
    </Card>
  )
  return (
    <>
      <TemplateDesignerShell
        className="soha-page soha-workflow-template-page"
        workspaceClassName="soha-workflow-template-workspace"
        designerClassName="soha-workflow-template-designer"
        list={
          <ManagementSearchableListPane
            activeKey={selected?.id ?? ''}
            className="soha-workflow-template-list"
            getItemKey={(item) => item.id}
            items={items.filter((item) =>
              [item.name, item.key, item.category]
                .join(' ')
                .toLowerCase()
                .includes(text.toLowerCase()),
            )}
            isLoading={templates.isLoading}
            isError={templates.isError && !readableQueryData(templates)}
            onRetry={() => void templates.refetch()}
            emptyDescription="暂无模板"
            searchPlaceholder="搜索模板"
            searchValue={text}
            searchActions={
              <>
                <ManagementIconButton
                  aria-label="新建模板"
                  tooltip="新建模板"
                  color="primary"
                  variant="solid"
                  icon={<PlusOutlined />}
                  disabled={!canCreate}
                  onClick={() => setDraft({})}
                />
                <ManagementIconButton
                  aria-label="导入文件"
                  tooltip="导入文件"
                  icon={<UploadOutlined />}
                  disabled={!canCreate && !canUpdate}
                  onClick={() => setImporting(true)}
                />
                <TemplateSourcesButton iconOnly />

                <ManagementRefreshButton
                  tooltip="刷新"
                  aria-label="刷新"
                  loading={templates.isFetching}
                  onClick={() => {
                    void templates.refetch()
                    if (selectedVersion) void version.refetch()
                  }}
                />
              </>
            }
            onSearchChange={setText}
            onItemSelect={(item) => {
              updateSearch({ templateId: item.id, version: undefined })
              setTab('definition')
            }}
            renderItem={(item) => (
              <Space orientation="vertical" size={4}>
                <Typography.Text strong>{item.name}</Typography.Text>
                <Typography.Text type="secondary">{item.key}</Typography.Text>
                <Space>
                  <MetadataTag
                    label={item.definition?.mode === 'delivery_batch' ? '发布流程' : '环境工作流'}
                  />
                  <TemplatePublicationStatus template={item} />
                </Space>
              </Space>
            )}
          />
        }
        designer={
          <>
            <header className="soha-workflow-template-toolbar" aria-label="当前流程模板操作">
              <Space wrap>
                <Button
                  icon={<EditOutlined />}
                  type="primary"
                  disabled={!selected || !canEdit || selected.publicationState === 'deprecated'}
                  onClick={() => setDraft({ template: selected })}
                >
                  编辑草稿
                </Button>
                <Button
                  icon={<CopyOutlined />}
                  disabled={!shown || !canCreate}
                  onClick={() => {
                    if (shown)
                      setDraft({
                        copiedFrom: {
                          id: shown.id,
                          revision: shown.revision!,
                          version: selectedVersion || undefined,
                        },
                        template: {
                          ...shown,
                          id: '',
                          key: `${shown.key}-copy`,
                          name: `${shown.name} Copy`,
                          revision: undefined,
                          publishedVersion: 0,
                          publicationState: 'draft',
                        },
                      })
                  }}
                >
                  复制模板
                </Button>
                <Popconfirm
                  title="发布当前草稿为新版本？"
                  description="已有服务继续使用固定版本。"
                  disabled={locked}
                  onConfirm={() => submit('publish')}
                >
                  <Button
                    disabled={
                      locked ||
                      templates.isError ||
                      source.isError ||
                      !selected ||
                      !canUpdate ||
                      selected.publicationState !== 'draft'
                    }
                    loading={publish.isPending}
                  >
                    发布版本
                  </Button>
                </Popconfirm>
                <Popconfirm
                  title="废弃模板？已有绑定和历史版本将保留。"
                  disabled={locked}
                  onConfirm={() => submit('deprecate')}
                >
                  <Button
                    danger
                    disabled={
                      locked ||
                      templates.isError ||
                      source.isError ||
                      !selected ||
                      !canDelete ||
                      selected.publicationState === 'deprecated'
                    }
                  >
                    废弃
                  </Button>
                </Popconfirm>
              </Space>
            </header>
            <ReadFeedback query={templates} label="流程模板" />
            <ReadFeedback query={source} label="模板来源" />
            <ReadFeedback query={version} label="固定版本" />
            {tab === 'versions' ? <ReadFeedback query={versions} label="版本记录" /> : null}
            {tab === 'usage' ? <ReadFeedback query={usage} label="引用与使用" /> : null}
            <MutationFeedback
              operations={[
                {
                  mutation: publish,
                  label: '发布模板版本',
                  target: publish.variables?.id,
                  receipt: { message: '模板版本已发布，已有服务保留固定版本。' },
                },
                {
                  mutation: deprecate,
                  label: '废弃模板',
                  target: deprecate.variables,
                  receipt: { message: '模板已废弃，已有绑定和历史版本保留。' },
                },
              ]}
            />
            {content}
          </>
        }
      />
      {importing ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <ImportDialog
            onClose={() => setImporting(false)}
            onImported={(result) => {
              const item = result.objects.find((object) => object.kind === 'WorkflowTemplate')
              if (item) updateSearch({ templateId: item.id, version: undefined })
            }}
          />
        </Suspense>
      ) : null}
      {draft ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <DraftEditor
            key={draftGeneration}
            onReload={(template) => {
              setDraft({ template })
              setDraftGeneration((value) => value + 1)
            }}
            template={draft.template}
            copiedFrom={draft.copiedFrom}
            onClose={() => setDraft(undefined)}
            onSaved={(template) => {
              setDraft(undefined)
              updateSearch({ templateId: template.id, version: undefined })
            }}
          />
        </Suspense>
      ) : null}
      {usingVersion ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <UseVersionDialog template={usingVersion} onClose={() => setUsingVersion(undefined)} />
        </Suspense>
      ) : null}
    </>
  )
}
