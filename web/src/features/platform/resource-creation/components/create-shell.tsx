import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StepFormModal } from '@/components/step-form-modal'
import { clusterQueries } from '@/features/platform/clusters/queries'
import { usePlatformMutation } from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { readableQueryData } from '@/utils/query-feedback'
import { createUUID } from '@/utils/uuid'
import { CheckCircleOutlined, FileTextOutlined, FormOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Collapse, Spin, Tabs, Typography } from 'antd'
import { lazy, Suspense, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { PreparedResourceManifest, ResourceFormDefinition } from '../forms'
import {
  buildResourceCreateRequest,
  isPreflightCurrent,
  resolveResourceCreateDefaultNamespace,
  resourceCreateRequestFingerprint,
} from '../model'
import { resourceCreationMutations } from '../mutations'
import { resourceCreationQueries } from '../queries'
import { hasResourceCreateForm, loadResourceCreateForm } from '../registry'
import '../styles.css'
import type { ResourceCreateContext, ResourceCreateResult } from '../types'
import { ResourcePreflightTable } from './preflight-table'
import { ResourceCreateResultTable } from './result-table'

const { Text, Title } = Typography

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

export function resourceCreationErrorMessage(error: unknown, isChinese: boolean) {
  const detail = error instanceof Error ? error.message : String(error)
  const requestId = isApiError(error) ? error.requestId : undefined
  const requestSuffix = requestId
    ? isChinese
      ? `（请求 ID：${requestId}）`
      : ` (request ID: ${requestId})`
    : ''
  return `${detail}${requestSuffix}`
}

export interface ResourceCreateFormAdapter {
  readonly content: ReactNode
  readonly manifest: string
  readonly valid: boolean
}

export interface CreateShellProps {
  readonly context: ResourceCreateContext
  readonly defaultTemplate: string
  readonly form?: ResourceCreateFormAdapter
  readonly initialMode?: 'form' | 'yaml'
  readonly label?: string
  readonly onBlockedChange?: (blocked: boolean) => void
  readonly onClose?: () => void
  readonly onCreated?: (result: ResourceCreateResult) => void
  readonly open?: boolean
  readonly presentation?: 'modal' | 'page'
  readonly formSupported?: boolean
  readonly title?: ReactNode
}

function ShellBody({
  context: initialContext,
  defaultTemplate,
  form,
  formSupported,
  initialMode = 'yaml',
  onClose,
  onBlockedChange,
  onCreated,
  visible,
}: CreateShellProps & { visible: boolean }) {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const isChinese = localeCode === 'zh_CN'
  const queryClient = useQueryClient()
  const registryFormSupported = Boolean(
    formSupported ?? (!form && hasResourceCreateForm(initialContext.expectedKind)),
  )
  const [mode, setMode] = useState<'form' | 'yaml'>(
    form || registryFormSupported ? initialMode : 'yaml',
  )
  const [formDefinition, setFormDefinition] = useState<ResourceFormDefinition>()
  const [formValues, setFormValues] = useState<unknown>()
  const [draft, setDraft] = useState(defaultTemplate)
  const [preparedManifest, setPreparedManifest] = useState<PreparedResourceManifest | null>(null)
  const [preparationError, setPreparationError] = useState<string | null>(null)
  const [preparingManifest, setPreparingManifest] = useState(false)
  const formRevision = useRef(0)
  const [preflightRequestFingerprint, setPreflightRequestFingerprint] = useState<string | null>(
    null,
  )
  const [idempotencyKey, setIdempotencyKey] = useState<string | null>(null)
  const clustersQuery = useQuery({
    ...clusterQueries.list(),
    enabled: visible,
  })
  const visibleClustersQueryData = visible ? readableQueryData(clustersQuery) : undefined
  const namespacesQuery = useQuery({
    ...resourceCreationQueries.namespaces(initialContext.clusterId),
    enabled: visible && registryFormSupported && Boolean(initialContext.clusterId.trim()),
  })
  const visibleNamespacesQueryData =
    visible && registryFormSupported && Boolean(initialContext.clusterId.trim())
      ? readableQueryData(namespacesQuery)
      : undefined
  const preflightMutation = usePlatformMutation(resourceCreationMutations.preflight(), {
    label: ['资源预检', 'Resource preflight'],
    target: (v) =>
      [v.clusterId, v.request.defaultNamespace, initialContext.expectedKind || 'Kubernetes']
        .filter(Boolean)
        .join(' / '),
    readOnly: true,
  })
  const executeMutation = usePlatformMutation(resourceCreationMutations.execute(queryClient), {
    label: ['创建资源', 'Create resources'],
    target: (v) =>
      [v.clusterId, v.request.defaultNamespace, initialContext.expectedKind || 'Kubernetes']
        .filter(Boolean)
        .join(' / '),
  })
  const platformWriteBlocked = executeMutation.blocked
  useEffect(() => onBlockedChange?.(platformWriteBlocked), [onBlockedChange, platformWriteBlocked])
  const registryResource = useMemo(() => {
    if (!formDefinition || formValues == null) return undefined
    return formDefinition.buildManifest(formValues)
  }, [formDefinition, formValues])
  const registryManifest = useMemo(
    () => (registryResource ? JSON.stringify(registryResource, null, 2) : ''),
    [registryResource],
  )
  const content =
    mode === 'form' ? (form?.manifest ?? preparedManifest?.content ?? registryManifest) : draft
  const context = initialContext
  const clusterName =
    visibleClustersQueryData?.find((cluster) => cluster.id === initialContext.clusterId)?.name || ''
  const defaultNamespace = resolveResourceCreateDefaultNamespace({
    contextNamespace: context.defaultNamespace,
    formNamespace: registryResource?.metadata.namespace,
    mode,
  })
  const request = useMemo(
    () => buildResourceCreateRequest(context, content, mode, defaultNamespace, preparedManifest),
    [content, context, defaultNamespace, mode, preparedManifest],
  )
  const requestFingerprint = useMemo(
    () => resourceCreateRequestFingerprint(context.clusterId, request),
    [context.clusterId, request],
  )
  const namespaceRequired =
    initialContext.scopeMode === 'namespace' &&
    initialContext.source !== 'global_yaml' &&
    !request.defaultNamespace
  const preflightCurrent = isPreflightCurrent(
    requestFingerprint,
    preflightRequestFingerprint,
    preflightMutation.data,
  )
  const canExecute = Boolean(preflightCurrent && preflightMutation.data?.ready)
  const formBusy = preparingManifest || preflightMutation.isPending

  useEffect(() => {
    if (!visible) return
    setMode(form || registryFormSupported ? initialMode : 'yaml')
    setDraft(defaultTemplate)
    setPreparedManifest(null)
    setPreparationError(null)
    setPreparingManifest(false)
    formRevision.current += 1
    setPreflightRequestFingerprint(null)
    setIdempotencyKey(null)
    preflightMutation.reset()
    executeMutation.reset()
  }, [
    defaultTemplate,
    form,
    initialContext.clusterId,
    initialContext.defaultNamespace,
    initialContext.expectedApiVersion,
    initialContext.expectedKind,
    initialContext.resourceGroup,
    initialContext.scopeMode,
    initialContext.source,
    initialMode,
    registryFormSupported,
    visible,
  ])

  useEffect(() => {
    const kind = initialContext.expectedKind
    if (!visible || form || !registryFormSupported || !kind) return
    let cancelled = false
    void loadResourceCreateForm(kind).then((definition) => {
      if (cancelled || !definition) return
      setFormDefinition(definition)
      setFormValues(
        definition.defaultValues({
          clusterId: initialContext.clusterId,
          namespace: initialContext.defaultNamespace || null,
        }),
      )
    })
    return () => {
      cancelled = true
    }
  }, [
    form,
    initialContext.defaultNamespace,
    initialContext.expectedKind,
    registryFormSupported,
    visible,
  ])

  function invalidatePreflight() {
    if (platformWriteBlocked) return
    formRevision.current += 1
    setPreparedManifest(null)
    setPreparationError(null)
    setPreparingManifest(false)
    setPreflightRequestFingerprint(null)
    setIdempotencyKey(null)
    preflightMutation.reset()
    executeMutation.reset()
  }

  async function runPreflight(submittedValues?: unknown) {
    if (platformWriteBlocked || preflightMutation.isPending || preparingManifest) return
    const revision = formRevision.current
    setPreflightRequestFingerprint(null)
    setIdempotencyKey(null)
    setPreparationError(null)
    preflightMutation.reset()
    executeMutation.reset()

    let nextContent = content
    let nextDefaultNamespace = defaultNamespace
    let nextPreparedManifest = preparedManifest
    try {
      if (mode === 'form' && formDefinition && submittedValues != null) {
        const nextResource = formDefinition.buildManifest(submittedValues)
        nextDefaultNamespace = resolveResourceCreateDefaultNamespace({
          contextNamespace: context.defaultNamespace,
          formNamespace: nextResource.metadata.namespace,
          mode,
        })
        setFormValues(submittedValues)
        if (formDefinition.prepareManifest) {
          setPreparingManifest(true)
          nextPreparedManifest = await formDefinition.prepareManifest(submittedValues, {
            clusterId: context.clusterId,
            namespace: nextDefaultNamespace || null,
          })
          if (revision !== formRevision.current) return
          nextContent = nextPreparedManifest.content
          setPreparedManifest(nextPreparedManifest)
        } else {
          nextContent = JSON.stringify(nextResource, null, 2)
        }
      }
    } catch (error) {
      if (revision !== formRevision.current) return
      const detail = error instanceof Error ? error.message : String(error)
      setPreparationError(detail)
      void message.error(detail)
      return
    } finally {
      if (revision === formRevision.current) setPreparingManifest(false)
    }

    const nextRequest = buildResourceCreateRequest(
      context,
      nextContent,
      mode,
      nextDefaultNamespace,
      nextPreparedManifest,
    )
    const nextFingerprint = resourceCreateRequestFingerprint(context.clusterId, nextRequest)
    preflightMutation.mutate(
      { clusterId: context.clusterId, request: nextRequest },
      {
        onSuccess: () => {
          if (revision !== formRevision.current) return
          setPreflightRequestFingerprint(nextFingerprint)
          setIdempotencyKey(createUUID())
        },
      },
    )
  }

  function execute() {
    if (!canExecute || platformWriteBlocked) return
    executeMutation.mutate(
      {
        clusterId: context.clusterId,
        idempotencyKey: idempotencyKey || createUUID(),
        request,
      },
      {
        onSuccess: (result) => {
          onCreated?.(result)
        },
      },
    )
  }

  if (!context.clusterId) {
    return (
      <ManagementState
        compact
        description={isChinese ? '请先选择集群。' : 'Select a cluster first.'}
        kind="select-scope"
      />
    )
  }

  return (
    <div className="soha-resource-create-shell">
      <ReadFeedback canRead={clustersQuery.isEnabled} query={clustersQuery} label="集群目录" />
      <ReadFeedback canRead={namespacesQuery.isEnabled} query={namespacesQuery} label="命名空间" />

      {preflightMutation.feedback}
      {executeMutation.feedback}

      {form || registryFormSupported ? (
        <div className="soha-resource-create-toolbar">
          <Tabs
            activeKey={mode}
            className="soha-resource-tabs is-header-only"
            items={[
              { icon: <FormOutlined />, key: 'form', label: isChinese ? '表单创建' : 'Form' },
              { icon: <FileTextOutlined />, key: 'yaml', label: 'YAML' },
            ]}
            onChange={(value) => {
              setMode(value as 'form' | 'yaml')
              invalidatePreflight()
            }}
          />
          <Text className="soha-resource-create-cluster-hint" type="secondary">
            {isChinese ? '集群' : 'Cluster'}:{' '}
            {clustersQuery.isLoading ? (isChinese ? '加载中' : 'Loading') : clusterName || '-'}
          </Text>
        </div>
      ) : null}

      {mode === 'form' && (form || registryFormSupported) ? (
        <div className="soha-resource-create-form">
          {form?.content ??
            (formDefinition && formValues != null ? (
              formDefinition.renderForm({
                clusterId: context.clusterId,
                loading: formBusy,
                disabled: platformWriteBlocked,
                localeCode,
                namespaceLoading: namespacesQuery.isLoading,
                namespaceOptions: (visibleNamespacesQueryData ?? []).map((item) => item.name),
                onChange: (value) => {
                  setFormValues(value)
                  invalidatePreflight()
                },
                onSubmit: (values) => void runPreflight(values),
                submitText: isChinese ? '生成 Manifest 并预检' : 'Build manifest and preflight',
                value: formValues,
              })
            ) : (
              <div className="soha-resource-create-loading">
                <Spin size="large" />
              </div>
            ))}
        </div>
      ) : (
        <Suspense
          fallback={
            <div className="soha-resource-create-loading">
              <Spin size="large" />
            </div>
          }
        >
          <K8sYamlEditor
            applyDisabled={platformWriteBlocked || !content.trim() || namespaceRequired || formBusy}
            applyDisabledReason={
              namespaceRequired
                ? isChinese
                  ? '列表创建必须选择命名空间。'
                  : 'Select a namespace for list creation.'
                : undefined
            }
            applyLabel={isChinese ? '预检' : 'Preflight'}
            applying={formBusy}
            editorHeight={
              initialContext.source === 'global_yaml' ? 'min(44vh, 460px)' : 'min(46vh, 480px)'
            }
            onApply={() => void runPreflight()}
            onChange={(value) => {
              setDraft(value)
              invalidatePreflight()
            }}
            onReset={() => {
              setDraft(defaultTemplate)
              invalidatePreflight()
            }}
            onSave={() => undefined}
            saveDisabled
            value={draft}
          />
        </Suspense>
      )}

      {mode === 'form' && form ? (
        <div className="soha-resource-create-form-actions">
          <Button
            disabled={!form.valid || namespaceRequired}
            loading={formBusy}
            onClick={() => void runPreflight()}
            type="primary"
          >
            {isChinese ? '生成 Manifest 并预检' : 'Build manifest and preflight'}
          </Button>
        </div>
      ) : null}

      {preparationError ? (
        <Alert
          description={preparationError}
          showIcon
          title={isChinese ? 'Manifest 生成失败' : 'Manifest generation failed'}
          type="error"
        />
      ) : null}

      {preflightMutation.isError ? (
        <Alert
          description={resourceCreationErrorMessage(preflightMutation.error, isChinese)}
          showIcon
          title={isChinese ? '预检失败' : 'Preflight failed'}
          type="error"
        />
      ) : null}

      {preflightMutation.data ? (
        <section className="soha-resource-create-section">
          <div className="soha-resource-create-section-heading">
            <Title level={5}>{isChinese ? '预检结果' : 'Preflight result'}</Title>
            <StatusSummary ready={preflightMutation.data.ready} />
          </div>
          <Collapse
            className="soha-resource-create-manifest-collapse"
            defaultActiveKey={['manifest']}
            items={[
              {
                key: 'manifest',
                label: 'Manifest YAML',
                children: (
                  <pre className="soha-resource-create-manifest-preview">
                    <code>{request.content}</code>
                  </pre>
                ),
              },
            ]}
            size="small"
          />
          <ResourcePreflightTable items={preflightMutation.data.items} />
          <div className="soha-resource-create-submit">
            <Button
              disabled={!canExecute || platformWriteBlocked || executeMutation.isSuccess}
              icon={<CheckCircleOutlined />}
              loading={executeMutation.isPending}
              onClick={execute}
              type="primary"
            >
              {isChinese ? '确认创建' : 'Create resources'}
            </Button>
          </div>
        </section>
      ) : null}

      {executeMutation.data ? (
        <section className="soha-resource-create-section">
          <div className="soha-resource-create-section-heading">
            <Title level={5}>{isChinese ? '创建结果' : 'Creation result'}</Title>
            <Text type="secondary">Operation {executeMutation.data.operationId}</Text>
          </div>
          <ResourceCreateResultTable items={executeMutation.data.items} />
          {onClose ? (
            <Button type="primary" onClick={onClose}>
              {isChinese ? '完成' : 'Done'}
            </Button>
          ) : null}
        </section>
      ) : null}
    </div>
  )
}

function StatusSummary({ ready }: { ready: boolean }) {
  const { localeCode } = useI18n()
  return (
    <Text type={ready ? 'success' : 'danger'}>
      {ready
        ? localeCode === 'zh_CN'
          ? '全部通过'
          : 'Ready'
        : localeCode === 'zh_CN'
          ? '存在阻止项'
          : 'Blocked'}
    </Text>
  )
}

export function CreateShell(props: CreateShellProps) {
  const { localeCode } = useI18n()
  const [blocked, setBlocked] = useState(false)
  const title =
    props.title ??
    (localeCode === 'zh_CN'
      ? `新增${props.label ? ` ${props.label}` : ' Kubernetes 资源'}`
      : `Create${props.label ? ` ${props.label}` : ' Kubernetes resources'}`)
  if (props.presentation === 'page') {
    return <ShellBody {...props} visible />
  }
  return (
    <StepFormModal
      bodyClassName="soha-resource-create-modal__body"
      onClose={() => {
        if (!blocked) props.onClose?.()
      }}
      open={Boolean(props.open)}
      title={title}
      width="min(1120px, calc(100vw - 32px))"
    >
      <ShellBody {...props} onBlockedChange={setBlocked} visible={Boolean(props.open)} />
    </StepFormModal>
  )
}
