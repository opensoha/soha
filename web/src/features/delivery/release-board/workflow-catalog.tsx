import { lazy, Suspense, useState } from 'react'
import { Button, Card, Dropdown, Pagination, Segmented, Space, Typography } from 'antd'
import {
  EllipsisOutlined,
  HistoryOutlined,
  ImportOutlined,
  PlayCircleOutlined,
  PlusOutlined,
  SettingOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementState,
  ManagementRefreshButton,
  ManagementToolbarSearch,
} from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag, StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { readableQueryData } from '@/utils/query-feedback'
import { applicationWorkspacePath } from '../applications/workspace-navigation'
import { deliveryQueries } from '../queries'
import { TemplateSourcesButton } from '../template-sources/entry'
import type {
  DeliveryWorkflowDefinition,
  DeliveryBatchTemplateDefinition,
  WorkflowCatalogEntry,
} from '../types'
import { deliveryModeLabels } from '../batches/model'
import { WorkflowCenterRuns } from './center-runs'
import { centerRunPhase, mergeCenterRuns } from './center-runs-model'
import { definitionHistoryPath, ExecutionTrend } from './execution-trend'
import './styles.css'

const DeliveryBatchEditor = lazy(() =>
  import('../batches/editor').then((module) => ({ default: module.DeliveryBatchEditor })),
)
const ImportDialog = lazy(() =>
  import('../documents/import-dialog').then((module) => ({
    default: module.DeliveryDocumentImportDialog,
  })),
)

function definitionPath(card: WorkflowCatalogEntry, run = false) {
  const scope = card.scopes[0]
  const path = applicationWorkspacePath(
    scope.applicationId,
    card.sourceKind === 'build_source' && !run ? 'application' : 'delivery',
    scope.applicationEnvironmentId,
  )
  return card.sourceKind === 'build_source'
    ? path + '&buildSourceId=' + encodeURIComponent(card.sourceId)
    : path
}

type WorkflowCenterView = 'active' | 'recent' | 'definitions'

function workflowCenterView(search: URLSearchParams): WorkflowCenterView {
  const view = search.get('view')
  if (view === 'active' || view === 'recent' || view === 'definitions') return view
  return ['workflowId', 'create', 'tab', 'applicationId', 'environmentId'].some((key) =>
    search.has(key),
  )
    ? 'definitions'
    : 'active'
}

export function WorkflowCatalog() {
  const [urlSearch, setURLSearch] = useSearchParams()
  const navigate = useNavigate()
  const english = useI18n().localeCode === 'en_US'
  const permissionQuery = usePermissionSnapshot()
  const permissions = permissionQuery.data?.data
  const canViewWorkflows = hasPermission(permissions, 'delivery.workflows.view')
  const canViewBuilds = hasPermission(permissions, 'delivery.applications.view')
  const canBuild = canViewBuilds && hasPermission(permissions, 'delivery.builds.trigger')
  const canManage = canViewWorkflows && hasPermission(permissions, 'delivery.workflows.trigger')
  const canView = canViewBuilds || canViewWorkflows
  const [importing, setImporting] = useState(false)
  const [selectingTargets, setSelectingTargets] = useState(false)
  const view = workflowCenterView(urlSearch)
  const viewingDefinitions = view === 'definitions'
  const viewingActive = view === 'active'
  const cursor = urlSearch.get('cursor') || undefined
  const applicationId = urlSearch.get('applicationId') || undefined
  const environmentId = urlSearch.get('environmentId') || undefined
  const search = urlSearch.get('search') || ''
  const requestedPage = Number(urlSearch.get('page'))
  const page = Number.isSafeInteger(requestedPage) && requestedPage > 0 ? requestedPage : 1
  const updateSearch = (values: Record<string, string | undefined>, resetPage = true) =>
    setURLSearch(
      (current) => {
        const next = new URLSearchParams(current)
        if (resetPage) {
          next.delete('page')
          next.delete('cursor')
          next.delete('item')
        }
        for (const [key, value] of Object.entries(values)) {
          if (value) next.set(key, value)
          else next.delete(key)
        }
        return next
      },
      { replace: true },
    )
  const editId = urlSearch.get('workflowId') || ''
  const requestedWorkflow = useQuery(
    deliveryQueries.deliveryWorkflows.detail(editId, canViewWorkflows && Boolean(editId)),
  )
  const catalog = useQuery(
    deliveryQueries.workflowCatalog(
      { applicationId, environmentId, search, offset: (page - 1) * 12, limit: 12 },
      canView && viewingDefinitions,
    ),
  )
  const running = useQuery(
    deliveryQueries.executionHistory(
      { search: search || undefined, status: 'running', limit: 12 },
      canView && viewingActive,
    ),
  )
  const approvals = useQuery(
    deliveryQueries.executionHistory(
      { search: search || undefined, status: 'approval', limit: 12 },
      canView && viewingActive,
    ),
  )
  const recent = useQuery(
    deliveryQueries.executionHistory(
      { search: search || undefined, status: 'all', cursor, limit: 12 },
      canView && view === 'recent',
    ),
  )
  const runningData = readableQueryData(running)
  const approvalData = readableQueryData(approvals)
  const recentData = readableQueryData(recent)
  const catalogData = readableQueryData(catalog)
  const rows = viewingActive
    ? mergeCenterRuns([...(runningData?.items ?? []), ...(approvalData?.items ?? [])], english)
    : mergeCenterRuns(recentData?.items ?? [], english).filter(
        (row) => centerRunPhase(row.status) === 'recent',
      )
  const runQueries = viewingActive ? [running, approvals] : [recent]
  const setView = (next: WorkflowCenterView) => updateSearch({ view: next })
  const newWorkflow = () =>
    updateSearch(
      {
        view: 'definitions',
        create: 'workflow',
        workflowId: undefined,
        templateId: undefined,
        templateVersion: undefined,
      },
      false,
    )
  const requestedTemplateId = urlSearch.get('templateId') ?? ''
  const requestedVersion = Number(urlSearch.get('templateVersion'))
  const creating = urlSearch.get('create') === 'workflow'
  const creatingFromTemplate = creating && Boolean(requestedTemplateId)
  const validVersion = Number.isSafeInteger(requestedVersion) && requestedVersion > 0
  const templateHead = useQuery(
    deliveryQueries.workflowTemplates.detail(
      requestedTemplateId,
      canManage && creatingFromTemplate,
    ),
  )
  const requestedTemplate = useQuery(
    deliveryQueries.workflowTemplates.version(
      requestedTemplateId,
      validVersion ? requestedVersion : 0,
      canManage && creatingFromTemplate && validVersion,
    ),
  )
  const invalidTemplate =
    creatingFromTemplate &&
    (!validVersion ||
      templateHead.data?.publicationState === 'deprecated' ||
      templateHead.data?.enabled === false ||
      (requestedTemplate.data && requestedTemplate.data.definition?.mode !== 'delivery_batch'))
  const recipe = requestedTemplate.data?.definition as DeliveryBatchTemplateDefinition | undefined
  const initialDefinition: DeliveryWorkflowDefinition | undefined =
    recipe && !invalidTemplate
      ? {
          name: requestedTemplate.data!.name,
          workflowTemplateId: requestedTemplateId,
          workflowTemplateVersion: requestedVersion,
          mode: recipe.executionMode,
          stopOnFailure: recipe.stopOnFailure,
          maxConcurrency: recipe.maxConcurrency,
          targets: [],
        }
      : undefined
  const editor =
    requestedWorkflow.data && editId
      ? { workflow: requestedWorkflow.data, initialDefinition: undefined }
      : creating &&
          (!creatingFromTemplate || (initialDefinition && templateHead.data)) &&
          !invalidTemplate
        ? { workflow: undefined, initialDefinition }
        : undefined
  const closeEditor = () => {
    updateSearch(
      {
        workflowId: undefined,
        create: undefined,
        templateId: undefined,
        templateVersion: undefined,
      },
      false,
    )
    void catalog.refetch()
  }
  const openDefinition = (card: WorkflowCatalogEntry, run: boolean) => {
    if (card.sourceKind !== 'delivery_workflow') {
      navigate(
        definitionPath(card, run) +
          (run ? '&launch=' + (card.sourceKind === 'build_source' ? 'build' : 'workflow') : ''),
      )
      return
    }
    updateSearch({ view: 'definitions', workflowId: card.sourceId, create: undefined }, false)
  }
  if (permissionQuery.isLoading) return <ManagementState kind="loading" />
  if (permissionQuery.isError)
    return (
      <ManagementState
        kind="error"
        title={english ? 'Permissions unavailable' : '权限加载失败'}
        actions={
          <Button onClick={() => void permissionQuery.refetch()}>
            {english ? 'Retry' : '重试'}
          </Button>
        }
      />
    )
  if (!canView) return <ManagementState compact kind="no-permission" />
  const searchLabel = english ? 'Search workflow, execution or service' : '搜索工作流、执行号或服务'
  const historyPath = (status?: 'running' | 'approval') => {
    const filters = new URLSearchParams()
    if (status) filters.set('status', status)
    if (search) filters.set('search', search)
    return filters.size ? `/execution-history?${filters}` : '/execution-history'
  }
  const visibleQueries = viewingDefinitions ? [catalog] : runQueries
  return (
    <section
      aria-label={english ? 'Workflow Center' : '工作流中心'}
      className="soha-workflow-catalog"
    >
      {invalidTemplate ? (
        <ManagementState compact kind="error" title="模板版本不可用于创建工作流" />
      ) : null}
      {requestedTemplate.isError || templateHead.isError ? (
        <ManagementState
          compact
          kind="error"
          title="流程模板版本读取失败"
          actions={
            <Button
              onClick={() => {
                void requestedTemplate.refetch()
                void templateHead.refetch()
              }}
            >
              重试
            </Button>
          }
        />
      ) : null}
      {editId && requestedWorkflow.isError ? (
        <ManagementState
          compact
          kind="error"
          title={english ? 'Failed to load workflow' : '工作流读取失败'}
          actions={
            <Space>
              <Button onClick={() => void requestedWorkflow.refetch()}>
                {english ? 'Retry' : '重试'}
              </Button>
              <Button onClick={closeEditor}>{english ? 'Close' : '关闭'}</Button>
            </Space>
          }
        />
      ) : editId && requestedWorkflow.isPending ? (
        <ManagementState compact kind="loading" />
      ) : null}
      <Card
        variant="outlined"
        className="soha-management-query-card"
        classNames={{ body: 'soha-workflow-catalog__toolbar' }}
      >
        <Segmented<WorkflowCenterView>
          aria-label={english ? 'Workflow view' : '工作流视图'}
          value={view}
          options={[
            { value: 'active', label: english ? 'In progress' : '进行中' },
            { value: 'recent', label: english ? 'Recent executions' : '最近执行' },
            { value: 'definitions', label: english ? 'Workflow definitions' : '工作流定义' },
          ]}
          onChange={setView}
        />
        <div className="soha-workflow-catalog__actions">
          <ManagementToolbarSearch
            aria-label={searchLabel}
            placeholder={searchLabel}
            maxLength={200}
            size={264}
            value={search}
            onChange={(value) => updateSearch({ search: value })}
          />
          <ManagementRefreshButton
            tooltip={english ? 'Refresh' : '刷新'}
            aria-label={english ? 'Refresh workflow center' : '刷新工作流中心'}
            loading={visibleQueries.some((query) => query.isFetching)}
            onClick={() => visibleQueries.forEach((query) => void query.refetch())}
          />
          {canManage ? (
            <>
              <TemplateSourcesButton iconOnly />
              <Dropdown
                menu={{
                  items: [
                    {
                      key: 'import',
                      icon: <ImportOutlined />,
                      label: english ? 'Import file' : '导入文件',
                    },
                    {
                      key: 'new',
                      icon: <PlusOutlined />,
                      label: english ? 'New workflow' : '新建工作流',
                    },
                  ],
                  onClick: ({ key }) => {
                    if (key === 'import') setImporting(true)
                    else newWorkflow()
                  },
                }}
                trigger={['click']}
              >
                <Button icon={<EllipsisOutlined />}>
                  {english ? 'Manage workflows' : '工作流管理'}
                </Button>
              </Dropdown>
            </>
          ) : null}
          {canManage || (canBuild && !viewingDefinitions) ? (
            <Button
              type="primary"
              icon={<PlayCircleOutlined />}
              onClick={() => {
                if (canManage) setSelectingTargets(true)
                else setView('definitions')
              }}
            >
              {english ? 'Select services to run' : '选择服务执行'}
            </Button>
          ) : null}
        </div>
      </Card>
      {viewingDefinitions ? (
        <>
          <ReadFeedback query={catalog} label={english ? 'Workflow definitions' : '工作流定义'} />
          {catalog.isPending ? (
            <ManagementState
              compact
              kind="loading"
              title={english ? 'Loading workflows' : '正在读取工作流'}
            />
          ) : null}
          {applicationId || environmentId ? (
            <Button
              className="soha-workflow-catalog__clear-scope"
              onClick={() =>
                updateSearch({
                  applicationId: undefined,
                  environmentId: undefined,
                  view: 'definitions',
                })
              }
            >
              {english ? 'Clear linked scope' : '清除链接范围'}
            </Button>
          ) : null}
          <div className="soha-workflow-catalog__list" role="list">
            {catalogData?.items.map((card) => {
              const build = card.sourceKind === 'build_source'
              const appNames = [...new Set(card.scopes.map((scope) => scope.applicationName))].join(
                '、',
              )
              const context =
                card.sourceKind === 'delivery_workflow'
                  ? deliveryModeLabels[card.context as keyof typeof deliveryModeLabels] ||
                    card.context
                  : build
                    ? card.context
                    : card.scopes[0]?.environmentName
              const canRun = build
                ? canBuild
                : canManage && (card.sourceKind === 'delivery_workflow' || canBuild)
              return (
                <Card
                  size="small"
                  key={card.id}
                  role="listitem"
                  className="soha-workflow-definition"
                >
                  <div className="soha-workflow-definition__identity">
                    <div className="soha-workflow-catalog__card-heading">
                      <strong>{card.name}</strong>
                      {build ? <MetadataTag label={english ? 'Build only' : '仅构建'} /> : null}
                    </div>
                    <Typography.Text type="secondary">
                      {appNames} · {context}
                    </Typography.Text>
                    {!card.enabled ? <StatusTag value="disabled" /> : null}
                  </div>
                  <ExecutionTrend card={card} />
                  <div className="soha-workflow-catalog__card-actions">
                    <Space>
                      <ManagementIconButton
                        aria-label={english ? 'View history' : '查看记录'}
                        tooltip={english ? 'View history' : '查看记录'}
                        icon={<HistoryOutlined />}
                        onClick={() => navigate(definitionHistoryPath(card))}
                      />
                      <ManagementIconButton
                        aria-label={english ? 'Configure' : '配置'}
                        tooltip={english ? 'Configure' : '配置'}
                        icon={<SettingOutlined />}
                        loading={editId === card.sourceId && requestedWorkflow.isFetching}
                        onClick={() => openDefinition(card, false)}
                      />
                      {canRun && card.enabled ? (
                        <Button
                          type="primary"
                          icon={<PlayCircleOutlined />}
                          loading={editId === card.sourceId && requestedWorkflow.isFetching}
                          onClick={() => void openDefinition(card, true)}
                        >
                          {english ? 'Run' : '运行'}
                        </Button>
                      ) : null}
                    </Space>
                  </div>
                </Card>
              )
            })}
          </div>
          {catalogData && !catalogData.items.length && !catalog.isError ? (
            <ManagementState
              compact
              kind="not-configured"
              title={english ? 'No matching workflows' : '暂无匹配的工作流'}
              actions={
                search ? (
                  <Button onClick={() => updateSearch({ search: undefined })}>
                    {english ? 'Clear search' : '清除搜索'}
                  </Button>
                ) : canManage ? (
                  <Button onClick={newWorkflow}>{english ? 'New workflow' : '新建工作流'}</Button>
                ) : undefined
              }
            />
          ) : null}
          {catalogData && catalogData.total > 12 ? (
            <Pagination
              current={page}
              total={catalogData.total}
              pageSize={12}
              showSizeChanger={false}
              onChange={(value) =>
                updateSearch({ page: value > 1 ? String(value) : undefined }, false)
              }
            />
          ) : null}
        </>
      ) : (
        <>
          {viewingActive ? (
            <>
              <ReadFeedback
                query={running}
                label={english ? 'Active executions' : '进行中的执行'}
              />
              <ReadFeedback
                query={approvals}
                label={english ? 'Pending approvals' : '待审批执行'}
              />
            </>
          ) : (
            <ReadFeedback query={recent} label={english ? 'Recent executions' : '最近执行'} />
          )}
          {rows.length ? (
            <WorkflowCenterRuns
              rows={rows}
              selectedKey={urlSearch.get('item')}
              active={viewingActive}
              onSelect={(item) => updateSearch({ item }, false)}
            />
          ) : runQueries.some((query) => query.isPending) ? (
            <ManagementState compact kind="loading" />
          ) : runQueries.every((query) => !query.isError) ? (
            <ManagementState
              compact
              kind="empty"
              title={
                viewingActive
                  ? english
                    ? 'No active executions'
                    : '暂无进行中的执行'
                  : english
                    ? 'No ended executions on this page'
                    : '本页暂无已结束的执行'
              }
              actions={
                <Button onClick={() => setView('definitions')}>
                  {english ? 'View workflow definitions' : '查看工作流定义'}
                </Button>
              }
            />
          ) : null}
          {viewingActive ? (
            <div className="soha-workflow-center__pagination">
              {runningData?.nextCursor ? (
                <Link to={historyPath('running')}>
                  {english ? 'More active executions' : '更多进行中的执行'}
                </Link>
              ) : null}
              {approvalData?.nextCursor ? (
                <Link to={historyPath('approval')}>
                  {english ? 'More pending approvals' : '更多待审批执行'}
                </Link>
              ) : null}
            </div>
          ) : (
            <nav
              className="soha-workflow-center__pagination"
              aria-label={english ? 'Execution pagination' : '执行记录分页'}
            >
              <Link to={historyPath()}>{english ? 'Full execution history' : '全部执行记录'}</Link>
              {cursor ? (
                <Button onClick={() => updateSearch({ cursor: undefined, item: undefined }, false)}>
                  {english ? 'Latest executions' : '最新记录'}
                </Button>
              ) : null}
              {recentData?.nextCursor ? (
                <Button
                  disabled={recent.isFetching || recent.isError}
                  onClick={() =>
                    updateSearch({ cursor: recentData.nextCursor, item: undefined }, false)
                  }
                >
                  {english ? 'Next' : '下一页'}
                </Button>
              ) : null}
            </nav>
          )}
        </>
      )}
      {importing ? (
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          <ImportDialog
            onClose={() => setImporting(false)}
            onImported={() => {
              void catalog.refetch()
            }}
          />
        </Suspense>
      ) : null}
      {selectingTargets && canManage ? (
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          <DeliveryBatchEditor selectTargets onClose={() => setSelectingTargets(false)} />
        </Suspense>
      ) : null}
      {editor ? (
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          <DeliveryBatchEditor
            key={editor.workflow?.id || `${requestedTemplateId}/${requestedVersion}`}
            workflow={editor.workflow}
            initialDefinition={editor.initialDefinition}
            onSaved={(workflow) =>
              updateSearch(
                {
                  workflowId: workflow.id,
                  create: undefined,
                  templateId: undefined,
                  templateVersion: undefined,
                },
                false,
              )
            }
            onClose={closeEditor}
          />
        </Suspense>
      ) : null}
    </section>
  )
}
