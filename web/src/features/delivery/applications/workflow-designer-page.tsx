import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import './styles.css'
import { App, Button, Input, Space, Tag, Typography } from 'antd'
import { ArrowLeftOutlined, SaveOutlined } from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { ManagementState, TemplateDesignerShell } from '@/components/management-list'
import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { localeText, useI18n } from '@/i18n'
import {
  analyzeReleaseDagDefinition,
  normalizeReleaseDagDefinition,
  type ReleaseDagDefinition,
} from '@/components/release-flow-dag-definition'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import type { ApplicationEnvironment, WorkflowTemplate } from '../types'

const { Text } = Typography

const ReleaseFlowDagEditor = lazy(async () => {
  const module = await import('@/components/release-flow-dag-editor')
  return { default: module.ReleaseFlowDagEditor }
})

const EMPTY_WORKFLOW: ReleaseDagDefinition = {
  schemaVersion: 2,
  mode: 'release_dag',
  nodes: [],
  edges: [],
}

const RELEASE_DAG_ERROR_KEYS: Record<string, string> = {
  '至少需要 1 个 DAG 节点': 'page.applicationWorkflowDesigner.issue.nodeRequired',
  '节点 ID 不能重复': 'page.applicationWorkflowDesigner.issue.duplicateNodeIds',
  '连线 source/target 必须指向存在的节点': 'page.applicationWorkflowDesigner.issue.invalidEdges',
  'DAG 连线不能指向自身': 'page.applicationWorkflowDesigner.issue.selfLoop',
  'DAG 不能包含环路': 'page.applicationWorkflowDesigner.issue.cycle',
}

function ApplicationWorkflowCanvas({
  applicationId,
  binding,
  boundTemplate,
  initialName,
  sourceTemplate,
  draftSource: initialDraftSource,
  onVerify,
  verifying,
}: {
  applicationId: string
  binding: ApplicationEnvironment
  boundTemplate?: WorkflowTemplate
  initialName: string
  sourceTemplate?: WorkflowTemplate
  draftSource: boolean
  onVerify: () => unknown
  verifying: boolean
}) {
  const { t, localeCode } = useI18n()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const { message } = App.useApp()
  const [name, setName] = useState(initialName)
  const [originalName] = useState(initialName)
  const [draftSource] = useState(initialDraftSource)
  const [templateName] = useState(sourceTemplate?.name)
  const [environmentName] = useState(
    binding.alias || binding.environmentKey || binding.environmentId,
  )
  const [expectedRevision] = useState(boundTemplate?.revision)
  const [initialDefinition] = useState<ReleaseDagDefinition>(
    sourceTemplate ? normalizeReleaseDagDefinition(sourceTemplate.definition) : EMPTY_WORKFLOW,
  )
  const [definition, setDefinition] = useState<ReleaseDagDefinition>(initialDefinition)
  const dirty =
    draftSource ||
    name !== originalName ||
    JSON.stringify(definition) !== JSON.stringify(initialDefinition)
  const saveMutation = useMutation(deliveryMutations.environments.saveWorkflow(queryClient))
  const submitting = useRef(false)
  const uncertain = writeOutcomeUnknown(saveMutation.error)

  useEffect(() => {
    if (!dirty && !uncertain && !saveMutation.isPending) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty, uncertain, saveMutation.isPending])

  const returnToApplication = () => {
    if (saveMutation.isPending) return
    if (
      (dirty || uncertain) &&
      !window.confirm(
        uncertain
          ? localeText(
              localeCode,
              '保存结果尚未确认，请先核对环境记录。确认离开？',
              'The save outcome is unknown. Check the environment record before leaving. Leave now?',
            )
          : t(
              'page.applicationWorkflowDesigner.unsavedConfirm',
              '当前工作流有未保存更改，确认离开？',
            ),
      )
    )
      return
    navigate(`/applications/${encodeURIComponent(applicationId)}?tab=delivery`)
  }

  const save = async () => {
    if (submitting.current || uncertain) return
    const trimmedName = name.trim()
    if (!trimmedName) {
      message.error(t('page.applicationWorkflowDesigner.nameRequired', '请输入工作流名称'))
      return
    }
    const error = analyzeReleaseDagDefinition(definition).issues.find(
      (issue) => issue.severity === 'error',
    )
    if (error) {
      const key = RELEASE_DAG_ERROR_KEYS[error.message]
      message.error(
        key
          ? t(key, error.message)
          : t('page.applicationWorkflowDesigner.invalidDefinition', '工作流定义无效'),
      )
      return
    }
    submitting.current = true
    let outcomeUnknown = false
    try {
      await saveMutation.mutateAsync({
        applicationId,
        id: binding.id,
        payload: {
          expectedRevision,
          name: trimmedName,
          description:
            boundTemplate?.description ||
            t('page.applicationWorkflowDesigner.description', '应用内工作流'),
          definition,
          enabled: true,
        },
      })
      message.success(t('page.applicationWorkflowDesigner.saved', '工作流已保存'))
      navigate(`/applications/${encodeURIComponent(applicationId)}?tab=delivery`)
    } catch (error) {
      outcomeUnknown = writeOutcomeUnknown(error instanceof Error ? error : undefined)
    } finally {
      submitting.current = outcomeUnknown
    }
  }

  return (
    <TemplateDesignerShell
      className="soha-page soha-application-workflow-designer"
      designer={
        <>
          <OperationFeedback
            pending={saveMutation.isPending}
            error={saveMutation.error}
            label={t('page.applicationWorkflowDesigner.save', '保存工作流')}
            target={`${environmentName} · ${saveMutation.variables?.payload.name || name}`}
            receipt={
              uncertain
                ? {
                    message: localeText(
                      localeCode,
                      '工作流保存结果待核对',
                      'Workflow save outcome requires verification',
                    ),
                    evidence: (
                      <Space orientation="vertical">
                        <Text>
                          {localeText(localeCode, '环境当前记录', 'Current environment record')}:{' '}
                          {binding.workflowTemplate?.name || '—'} ·{' '}
                          {localeText(localeCode, '修订', 'Revision')}{' '}
                          {binding.workflowTemplate?.revision ?? '—'}
                        </Text>
                        <Button loading={verifying} onClick={() => void onVerify()}>
                          {localeText(localeCode, '核对环境记录', 'Check environment record')}
                        </Button>
                      </Space>
                    ),
                  }
                : undefined
            }
          />
          <div
            aria-busy={saveMutation.isPending}
            ref={(node) => {
              if (node) node.inert = saveMutation.isPending
            }}
          >
            <Suspense
              fallback={
                <ManagementState
                  kind="loading"
                  title={t('page.applicationWorkflowDesigner.canvasLoading', '正在加载画布')}
                />
              }
            >
              <ReleaseFlowDagEditor
                className="soha-application-workflow-designer__canvas"
                height="calc(100vh - 190px)"
                initialDefinition={initialDefinition}
                layout="palette-right-floating-inspector"
                variant="embedded"
                onChange={setDefinition}
              />
            </Suspense>
          </div>
        </>
      }
      designerClassName="soha-application-workflow-designer__designer"
      list={null}
      toolbar={
        <>
          <Space wrap>
            <Button
              icon={<ArrowLeftOutlined />}
              disabled={saveMutation.isPending}
              onClick={returnToApplication}
            >
              {t('page.applicationWorkflowDesigner.back', '返回工作流')}
            </Button>
            <span>
              <Text strong>{t('page.applicationWorkflowDesigner.title', '项目工作流设计')}</Text>
              <br />
              <Text type="secondary">
                {binding.alias || binding.environmentKey || binding.environmentId}
                {templateName
                  ? ` · ${t('page.applicationWorkflowDesigner.basedOn', '基于')} ${templateName}`
                  : ` · ${t('page.applicationWorkflowDesigner.blankCanvas', '空白画布')}`}
              </Text>
            </span>
          </Space>
          <Space wrap>
            <Input
              aria-label={t('page.applicationWorkflowDesigner.nameLabel', '工作流名称')}
              maxLength={80}
              style={{ width: 'clamp(160px, 35vw, 280px)' }}
              value={name}
              disabled={saveMutation.isPending}
              onChange={(event) => setName(event.target.value)}
            />
            <Tag color={dirty ? 'gold' : undefined}>
              {dirty
                ? t('page.applicationWorkflowDesigner.unsaved', '未保存')
                : t('page.applicationWorkflowDesigner.synced', '已同步')}
            </Tag>
            <Button
              type="primary"
              aria-label={t('page.applicationWorkflowDesigner.save', '保存工作流')}
              icon={<SaveOutlined />}
              loading={saveMutation.isPending}
              disabled={uncertain}
              onClick={() => void save()}
            >
              {t('page.applicationWorkflowDesigner.save', '保存工作流')}
            </Button>
          </Space>
        </>
      }
      workspaceClassName="soha-application-workflow-designer__workspace"
    />
  )
}

function ApplicationWorkflowDesignerSession({
  applicationId,
  searchParams,
}: {
  applicationId: string
  searchParams: URLSearchParams
}) {
  const { t, localeCode } = useI18n()
  const bindingId = searchParams.get('bindingId')?.trim() || ''
  const requestedTemplateId = searchParams.get('templateId')?.trim() || ''
  const source = searchParams.get('source')
  const applicationsQuery = useQuery(deliveryQueries.applications.list(Boolean(applicationId)))
  const bindingsQuery = useQuery(deliveryQueries.environments.list(Boolean(applicationId)))
  const templatesQuery = useQuery(deliveryQueries.workflowTemplates.list(Boolean(applicationId)))
  const applications = readableQueryData(applicationsQuery)
  const bindings = readableQueryData(bindingsQuery)
  const templates = readableQueryData(templatesQuery)
  const requestedTemplate = templates?.find((item) => item.id === requestedTemplateId)
  const [requestedVersion, setRequestedVersion] = useState(
    () => Number(searchParams.get('templateVersion')) || requestedTemplate?.publishedVersion || 0,
  )
  const requiresTemplate = source === 'template' && Boolean(requestedTemplateId)
  useEffect(() => {
    if (requiresTemplate && !requestedVersion && requestedTemplate?.publishedVersion) {
      setRequestedVersion(requestedTemplate.publishedVersion)
    }
  }, [requiresTemplate, requestedVersion, requestedTemplate?.publishedVersion])
  const sourceTemplateQuery = useQuery(
    deliveryQueries.workflowTemplates.version(
      requestedTemplateId,
      requestedVersion,
      requiresTemplate && Boolean(requestedVersion),
    ),
  )

  const sourceTemplateData = readableQueryData(sourceTemplateQuery)
  const feedback = (
    <>
      <ReadFeedback
        query={applicationsQuery}
        label={localeText(localeCode, '应用', 'Application')}
      />
      <ReadFeedback
        query={bindingsQuery}
        label={localeText(localeCode, '应用环境', 'Application environment')}
      />
      <ReadFeedback
        query={templatesQuery}
        label={localeText(localeCode, '工作流模板', 'Workflow templates')}
      />
      {requiresTemplate ? (
        <ReadFeedback
          query={sourceTemplateQuery}
          label={localeText(localeCode, '模板版本', 'Template version')}
        />
      ) : null}
    </>
  )
  const loading =
    applicationsQuery.isLoading ||
    bindingsQuery.isLoading ||
    templatesQuery.isLoading ||
    (requiresTemplate &&
      (sourceTemplateQuery.isLoading ||
        (!requestedVersion && Boolean(requestedTemplate?.publishedVersion))))
  if (
    !applications ||
    !bindings ||
    !templates ||
    (requiresTemplate && !sourceTemplateData && (requestedVersion > 0 || loading))
  ) {
    return (
      <>
        {feedback}
        {loading ? (
          <ManagementState
            kind="loading"
            title={t('page.applicationWorkflowDesigner.loading', '正在加载工作流')}
          />
        ) : null}
      </>
    )
  }

  const application = applications.find((item) => item.id === applicationId)
  const binding = bindings.find(
    (item) => item.id === bindingId && item.applicationId === applicationId,
  )
  if (!application || !binding) {
    return (
      <>
        {feedback}
        <ManagementState
          kind="not-found"
          title={t('page.applicationWorkflowDesigner.environmentNotFound', '未找到应用环境')}
        />
      </>
    )
  }

  const boundTemplate = binding.workflowTemplate
  if (source === 'template' && !sourceTemplateData) {
    return (
      <>
        {feedback}
        <ManagementState
          kind="not-found"
          title={t('page.applicationWorkflowDesigner.templateNotFound', '未找到所选工作流模板版本')}
        />
      </>
    )
  }
  const sourceTemplate =
    source === 'blank' ? undefined : source === 'template' ? sourceTemplateData : boundTemplate
  const initialName =
    searchParams.get('name')?.trim() ||
    boundTemplate?.name ||
    `${application.name}${t('page.applicationWorkflowDesigner.defaultNameSuffix', '工作流')}`

  return (
    <>
      {feedback}
      <ApplicationWorkflowCanvas
        applicationId={applicationId}
        binding={binding}
        boundTemplate={boundTemplate}
        initialName={initialName}
        sourceTemplate={sourceTemplate}
        draftSource={source === 'blank' || source === 'template' || !boundTemplate}
        onVerify={() => bindingsQuery.refetch()}
        verifying={bindingsQuery.isFetching}
      />
    </>
  )
}

export function ApplicationWorkflowDesignerPage() {
  const { applicationId = '' } = useParams()
  const [searchParams] = useSearchParams()
  return (
    <ApplicationWorkflowDesignerSession
      key={`${applicationId}:${searchParams.toString()}`}
      applicationId={applicationId}
      searchParams={searchParams}
    />
  )
}
