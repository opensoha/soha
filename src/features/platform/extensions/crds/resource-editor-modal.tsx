import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { useYamlDraft } from '@/components/use-yaml-draft'
import { hasAllowedAction } from '@/features/auth'
import {
  platformResourceTarget,
  usePlatformMutation,
} from '@/features/platform/shared/use-platform-mutation'
import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Alert, App, Button, Card, Modal, Spin } from 'antd'
import { lazy, Suspense, useEffect, useState, type ReactNode } from 'react'
import { crdMutations } from './mutations'
import { crdQueries } from './queries'
import type { CRD, CRDResourceInstance, CustomResourceTarget } from './types'
import { buildDefaultCustomResourceTemplate, isNamespacedCRD } from './utils'
import type { CRDEditorState } from './use-navigation-guard'

const K8sYamlEditor = lazy(async () => {
  const module = await import('@/components/k8s-yaml-editor')
  return { default: module.K8sYamlEditor }
})

export interface CRDResourceEditorModalProps {
  crd: CRD
  namespaceOverride?: string | null
  customResourceCapabilityReason?: string
  customResourceMutationsDisabled?: boolean
  mode: 'create' | 'edit'
  onClose: () => void
  onLeave?: (action: () => void) => void
  onStateChange?: (state: CRDEditorState) => void
  resource?: CRDResourceInstance | null
}

export function CRDResourceEditor({
  crd,
  namespaceOverride,
  customResourceCapabilityReason,
  customResourceMutationsDisabled = false,
  mode,
  onClose,
  resource,
  editable = true,
  header,
  onStateChange,
}: Omit<CRDResourceEditorModalProps, 'onClose'> & {
  onClose?: () => void
  editable?: boolean
  header?: ReactNode
  onStateChange?: (state: { dirty: boolean; busy: boolean }) => void
}) {
  const { t, localeCode } = useI18n()
  const { message } = App.useApp()
  const { clusterId, namespace } = usePlatformScopeStore()
  const listNamespace = namespaceOverride === undefined ? namespace : namespaceOverride
  const queryClient = useQueryClient()
  const readOnly =
    mode === 'edit' &&
    (!editable ||
      !hasAllowedAction(resource?.allowedActions, 'update') ||
      Boolean(resource?.deletingAt))
  const effectiveNamespace = isNamespacedCRD(crd)
    ? (resource?.namespace ?? listNamespace ?? '')
    : ''
  const target: CustomResourceTarget | null =
    clusterId && mode === 'edit' && resource
      ? {
          clusterId,
          crd,
          namespace: effectiveNamespace,
          resourceName: resource.name,
        }
      : null
  const draftStorageKey =
    target && !readOnly
      ? `soha:crd-yaml:${target.clusterId}:${crd.name}:${effectiveNamespace}:${target.resourceName}`
      : null
  const yamlQuery = useQuery(crdQueries.yaml(target, mode === 'edit'))
  const visibleYamlQueryData = readableQueryData(yamlQuery)
  const applyMutation = usePlatformMutation(crdMutations.apply(queryClient), {
    label: ['应用自定义资源', 'Apply custom resource'],
    target: platformResourceTarget,
  })
  const platformWriteBlocked = applyMutation.blocked

  const yamlState = useYamlDraft(
    JSON.stringify([mode, clusterId, crd.name, effectiveNamespace, resource?.name]),
    mode === 'create'
      ? buildDefaultCustomResourceTemplate(crd, listNamespace)
      : visibleYamlQueryData?.content,
    draftStorageKey,
  )
  const { value: draft, setValue: setDraft } = yamlState
  useEffect(() => {
    onStateChange?.({
      dirty: !readOnly && yamlState.original !== undefined && draft !== yamlState.original,
      busy: applyMutation.blocked,
    })
  }, [onStateChange, readOnly, draft, yamlState.original, applyMutation.blocked])
  useEffect(() => () => onStateChange?.({ dirty: false, busy: false }), [onStateChange])

  const apply = () => {
    if (
      !clusterId ||
      readOnly ||
      customResourceMutationsDisabled ||
      applyMutation.blocked ||
      applyMutation.isSuccess ||
      (mode === 'edit' && yamlQuery.isError) ||
      yamlState.serverChanged ||
      !draft.trim()
    )
      return
    applyMutation.mutate(
      {
        clusterId,
        content: draft,
        crd,
        mode,
        namespace: isNamespacedCRD(crd) ? effectiveNamespace : null,
        resourceName: resource?.name,
      },
      {
        onSuccess: (yaml) => {
          if (draftStorageKey && typeof window !== 'undefined') {
            window.localStorage.removeItem(draftStorageKey)
          }

          yamlState.applied(yaml.content)
        },
      },
    )
  }

  const bannerDescription = isNamespacedCRD(crd)
    ? localeCode === 'zh_CN'
      ? effectiveNamespace
        ? `当前资源遵循命名空间 scope。请求默认带上 namespace=${effectiveNamespace}，也可在 YAML 中覆盖 metadata.namespace。`
        : '当前为全部命名空间视图，请在 YAML 中显式填写 metadata.namespace。'
      : effectiveNamespace
        ? `This resource is namespaced. Requests default to namespace=${effectiveNamespace}; you can still override metadata.namespace in YAML.`
        : 'The current view spans all namespaces, so set metadata.namespace explicitly in YAML.'
    : localeCode === 'zh_CN'
      ? '当前资源为 cluster scope，命名空间选择不会参与请求。'
      : 'This resource is cluster-scoped, so the namespace selector is ignored for requests.'

  return (
    <>
      <ReadFeedback canRead={yamlQuery.isEnabled} query={yamlQuery} label="YAML" />

      {applyMutation.feedback}

      {!clusterId ? (
        <ManagementState
          bordered={false}
          compact
          kind="select-scope"
          title={localeCode === 'zh_CN' ? '请先选择集群' : 'Select a cluster first'}
        />
      ) : mode === 'edit' && yamlQuery.isLoading ? (
        <Card className="soha-detail-card">
          {header}
          <div style={{ height: 520, display: 'grid', placeItems: 'center' }}>
            <Spin size="large" />
          </div>
        </Card>
      ) : mode === 'edit' && yamlQuery.isError && !visibleYamlQueryData ? (
        header ? (
          <Card className="soha-detail-card">{header}</Card>
        ) : null
      ) : (
        <Suspense
          fallback={
            <div
              style={{
                height: 520,
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'center',
              }}
            >
              <Spin size="large" />
            </div>
          }
        >
          {mode === 'create' ? (
            <Alert banner showIcon type="info" description={bannerDescription} />
          ) : null}
          <div>
            {yamlQuery.isError && visibleYamlQueryData === undefined ? null : (
              <K8sYamlEditor
                header={header}
                key={yamlState.resourceKey}
                value={readOnly ? (visibleYamlQueryData?.content ?? '') : draft}
                original={mode === 'edit' && !readOnly ? yamlState.original : undefined}
                readOnly={readOnly || applyMutation.isSuccess}
                error={mode === 'edit' && yamlQuery.isError ? yamlQuery.error : undefined}
                serverChanged={mode === 'edit' && yamlState.serverChanged}
                onCompareLatest={yamlState.compareLatest}
                onChange={(value) => {
                  setDraft(value)
                  if (!readOnly && yamlState.original !== undefined && value !== yamlState.original)
                    onStateChange?.({ dirty: true, busy: applyMutation.blocked })
                }}
                onReset={() => {
                  yamlState.reset()
                  void message.success(t('yamlEditor.resetSuccess', 'YAML draft reset'))
                }}
                onSave={() => {
                  if (!draftStorageKey || typeof window === 'undefined') return
                  window.localStorage.setItem(draftStorageKey, draft)
                  void message.success(t('yamlEditor.saveSuccess', 'YAML draft saved locally'))
                }}
                onApply={apply}
                saveDisabled={!draftStorageKey}
                applyDisabled={
                  platformWriteBlocked ||
                  customResourceMutationsDisabled ||
                  !draft.trim() ||
                  applyMutation.isPending
                }
                applying={applyMutation.isPending}
              />
            )}
          </div>
          {customResourceCapabilityReason ? (
            <Alert
              showIcon
              type="warning"
              style={{ marginTop: 12 }}
              title={
                localeCode === 'zh_CN'
                  ? '当前连接模式限制自定义资源写入'
                  : 'Custom resource writes limited'
              }
              description={customResourceCapabilityReason}
            />
          ) : null}
          {onClose && (!header || applyMutation.isSuccess) ? (
            <div style={{ marginTop: 12, textAlign: 'right' }}>
              <Button disabled={applyMutation.blocked} onClick={onClose}>
                {applyMutation.isSuccess
                  ? localeCode === 'zh_CN'
                    ? '完成'
                    : 'Done'
                  : t('common.cancel', 'Cancel')}
              </Button>
            </div>
          ) : null}
        </Suspense>
      )}
    </>
  )
}

export function CRDResourceEditorModal(props: CRDResourceEditorModalProps) {
  const { localeCode } = useI18n()
  const { modal } = App.useApp()
  const [editorState, setEditorState] = useState({ dirty: false, busy: false })
  useEffect(() => {
    props.onStateChange?.(editorState)
    return () => props.onStateChange?.({ dirty: false, busy: false })
  }, [editorState, props.onStateChange])
  const readOnly =
    props.mode === 'edit' &&
    (!hasAllowedAction(props.resource?.allowedActions, 'update') ||
      Boolean(props.resource?.deletingAt))
  const requestClose = () => {
    if (editorState.busy) return
    if (props.onLeave) return props.onLeave(props.onClose)
    if (!editorState.dirty) props.onClose()
    else
      modal.confirm({
        title: localeCode === 'zh_CN' ? '放弃未应用的更改？' : 'Discard unapplied changes?',
        content:
          localeCode === 'zh_CN'
            ? '已保存的本地草稿仍会保留。'
            : 'Saved local drafts are retained.',
        okText: localeCode === 'zh_CN' ? '放弃并关闭' : 'Discard and close',
        onOk: props.onClose,
      })
  }
  return (
    <Modal
      className="soha-crd-editor-modal"
      title={`${localeCode === 'zh_CN' ? (readOnly ? '查看' : props.mode === 'create' ? '新建' : '编辑') : readOnly ? 'View' : props.mode === 'create' ? 'Create' : 'Edit'} ${props.crd.kind}`}
      open
      onCancel={requestClose}
      closable={!editorState.busy}
      keyboard={!editorState.busy}
      style={{ top: 32, maxWidth: 'calc(100vw - 32px)' }}
      styles={{ body: { maxHeight: 'calc(100dvh - 160px)', overflowY: 'auto' } }}
      footer={null}
      width={1080}
      destroyOnHidden
      mask={{ closable: false }}
    >
      <CRDResourceEditor {...props} onClose={requestClose} onStateChange={setEditorState} />
    </Modal>
  )
}
