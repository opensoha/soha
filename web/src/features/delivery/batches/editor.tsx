import { OperationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { lazy, Suspense, useRef, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Checkbox,
  Drawer,
  Form,
  Input,
  InputNumber,
  Select,
  Space,
  Tabs,
  Typography,
} from 'antd'
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CopyOutlined,
  DeleteOutlined,
  PlusOutlined,
} from '@ant-design/icons'
import { useMutation, useQueries, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { AdminTable } from '@/components/admin-table'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { deliveryQueries } from '../queries'
import { deliveryMutations } from '../mutations'
import type {
  DeliveryBatchInput,
  DeliveryTargetInput,
  DeliveryWorkflow,
  DeliveryWorkflowDefinition,
} from '../types'
import {
  deliveryTargetActionLabel,
  deliveryModeLabels,
  deliveryServiceCount,
  newDeliveryTarget,
  validateDeliveryEditor,
} from './model'
import { DeliveryTargetEditor } from './target-editor'
import { ManagementState } from '@/components/management-list'
import { workflowDocument } from '../documents/model'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import { DocumentSourcePanel } from '../template-sources/source-panel'

const TriggerManager = lazy(() =>
  import('../triggers/manager').then((module) => ({ default: module.DeliveryTriggerManager })),
)

const SourceEditor = lazy(() =>
  import('../documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentSourceEditor,
  })),
)
const ExportView = lazy(() =>
  import('../documents/source-editor').then((module) => ({
    default: module.DeliveryDocumentExportView,
  })),
)

export function DeliveryBatchEditor({
  workflow,
  initialDefinition,
  quick = false,
  selectTargets = false,
  retryOfBatchId,
  onSaved,
  onClose,
}: {
  workflow?: DeliveryWorkflow
  initialDefinition?: DeliveryWorkflowDefinition
  quick?: boolean
  selectTargets?: boolean
  retryOfBatchId?: string
  onSaved?: (workflow: DeliveryWorkflow) => void
  onClose: () => void
}) {
  const { message, modal } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissions = usePermissionSnapshot()
  const canTrigger = hasPermission(permissions.data?.data, 'delivery.workflows.trigger')
  const [saved, setSaved] = useState(workflow)
  const source = useQuery(deliveryQueries.documents.source('Workflow', saved?.id ?? ''))
  const sourceReadOnly = Boolean(saved && (!source.isSuccess || source.data.association))
  const canEdit = canTrigger && !sourceReadOnly
  const [tab, setTab] = useState('form')
  const [sourceDirty, setSourceDirty] = useState(false)
  const [sourceGeneration, setSourceGeneration] = useState(0)
  const [definition, setDefinition] = useState<DeliveryWorkflowDefinition>(() =>
    structuredClone(
      workflow?.definition ??
        initialDefinition ?? {
          name: '',
          mode: 'service_serial',
          stopOnFailure: true,
          maxConcurrency: 4,
          targets: [],
        },
    ),
  )
  const [editing, setEditing] = useState<DeliveryTargetInput>()
  useUnsavedDocument(
    sourceDirty ||
      JSON.stringify(saved?.definition ?? initialDefinition) !== JSON.stringify(definition),
  )
  const [error, setError] = useState<string>()
  const [selectingRecipe, setSelectingRecipe] = useState(false)
  const startRequest = useRef<{ signature: string; key: string } | undefined>(undefined)
  const save = useMutation(deliveryMutations.deliveryWorkflows.save(queryClient))
  const create = useMutation(deliveryMutations.batches.create(queryClient))
  const submitting = useRef(false)
  const unknown = writeOutcomeUnknown(save.error) || writeOutcomeUnknown(create.error)
  const busy = save.isPending || create.isPending || selectingRecipe
  const templates = useQuery(
    deliveryQueries.workflowTemplates.list(
      !quick && hasPermission(permissions.data?.data, 'delivery.workflow-templates.view'),
    ),
  )
  const versions = useQuery(
    deliveryQueries.workflowTemplates.versions(definition.workflowTemplateId ?? ''),
  )
  const appIds = [...new Set(definition.targets.map((item) => item.applicationId).filter(Boolean))]
  const applications = useQueries({
    queries: appIds.map((id) => deliveryQueries.applications.detail(id)),
  })
  const services = useQueries({
    queries: appIds.map((id) => deliveryQueries.applications.services(id)),
  })
  const targetLabel = (target: DeliveryTargetInput) => {
    const i = appIds.indexOf(target.applicationId)
    const app = applications[i]?.data
    const service = services[i]?.data?.find((item) => item.id === target.serviceId)
    const environment = app?.bindings?.find(
      (item) => item.applicationEnvironmentId === target.applicationEnvironmentId,
    )
    return {
      application: app?.application.name || target.applicationId,
      service: service?.name || target.serviceId,
      environment:
        environment?.environmentName ||
        environment?.environmentKey ||
        target.applicationEnvironmentId ||
        '无环境',
    }
  }
  const update = (patch: Partial<DeliveryWorkflowDefinition>) => {
    if (!canEdit) return
    setDefinition((current) => ({ ...current, ...patch }))
    setError(undefined)
  }
  const updateTarget = (target: DeliveryTargetInput) =>
    update({
      targets: definition.targets.some((item) => item.id === target.id)
        ? definition.targets.map((item) => (item.id === target.id ? target : item))
        : [...definition.targets, target],
    })
  const selectRecipe = async (id?: string, version?: number) => {
    if (!id) {
      update({ workflowTemplateId: undefined, workflowTemplateVersion: undefined })
      return
    }
    setSelectingRecipe(true)
    try {
      const selectedVersion =
        version ??
        readableQueryData(templates)?.find((item) => item.id === id)?.publishedVersion ??
        0
      const template = await queryClient.fetchQuery(
        deliveryQueries.workflowTemplates.version(id, selectedVersion),
      )
      const recipe = template.definition
      if (recipe?.mode !== 'delivery_batch') throw new Error('请选择发布流程模板')
      update({
        workflowTemplateId: id,
        workflowTemplateVersion: selectedVersion,
        mode: recipe.executionMode as DeliveryWorkflowDefinition['mode'],
        stopOnFailure: recipe.stopOnFailure as boolean,
        maxConcurrency: recipe.maxConcurrency as number,
      })
    } catch (failure) {
      setError((failure as Error).message)
    } finally {
      setSelectingRecipe(false)
    }
  }
  const submit = async (execute: boolean) => {
    if (submitting.current || unknown || busy || sourceDirty || (!execute && !canEdit)) return
    const validation = validateDeliveryEditor(definition)
    if (validation) {
      setError(validation)
      return
    }
    setError(undefined)
    submitting.current = true
    try {
      if (!execute) {
        const result = await save.mutateAsync({
          id: saved?.id,
          payload: { definition, expectedVersion: saved?.version },
        })
        setSaved(result)
        setDefinition(result.definition)
        setSourceGeneration((value) => value + 1)
        message.success('工作流已保存')
        onSaved?.(result)
        return
      }
      const input: Omit<DeliveryBatchInput, 'idempotencyKey'> = {
        ...(saved && JSON.stringify(saved.definition) === JSON.stringify(definition)
          ? { workflowId: saved.id, workflowVersion: saved.version }
          : { definition }),
        retryOfBatchId,
      }
      const signature = JSON.stringify(input)
      if (startRequest.current?.signature !== signature)
        startRequest.current = { signature, key: crypto.randomUUID() }
      const batch = await create.mutateAsync({ ...input, idempotencyKey: startRequest.current.key })
      onClose()
      navigate(`/delivery/batches/${encodeURIComponent(batch.id)}`)
    } catch {
      /* Mutation feedback retains the submitted definition. */
    } finally {
      submitting.current = false
    }
  }
  const close = () => {
    if (busy) return
    if (
      !quick &&
      (sourceDirty ||
        JSON.stringify(saved?.definition ?? initialDefinition) !== JSON.stringify(definition))
    )
      modal.confirm({ title: '关闭并放弃未保存的修改？', onOk: onClose })
    else onClose()
  }
  return (
    <Drawer
      open
      title={
        quick
          ? '快速部署'
          : retryOfBatchId
            ? '重新检查并交付'
            : saved
              ? '编辑工作流'
              : selectTargets
                ? '选择服务执行'
                : '新建工作流'
      }
      size={1180}
      onClose={close}
      mask={{ closable: !busy }}
      closable={!busy}
      footer={
        <Space>
          <Button onClick={close} disabled={busy}>
            关闭
          </Button>
          {!quick && canTrigger ? (
            <Button
              loading={save.isPending}
              disabled={busy || unknown || sourceDirty || !canEdit}
              onClick={() => void submit(false)}
            >
              保存工作流
            </Button>
          ) : null}
          {saved && canTrigger ? (
            <Button
              disabled={busy}
              onClick={() => {
                setSaved(undefined)
                setDefinition((current) => ({ ...current, name: `${current.name} 副本` }))
                setSourceDirty(false)
                setSourceGeneration((value) => value + 1)
              }}
            >
              复制为新工作流
            </Button>
          ) : null}
          {canTrigger ? (
            <Button
              type="primary"
              loading={create.isPending}
              disabled={busy || unknown || sourceDirty}
              onClick={() => void submit(true)}
            >
              开始交付
            </Button>
          ) : null}
        </Space>
      }
    >
      {selectTargets ? (
        <Typography.Paragraph type="secondary">
          可为多个应用添加服务和环境目标。开始交付后，批次会按目标开展预检并生成部署计划，需要审批的目标会等待审批。
        </Typography.Paragraph>
      ) : null}
      <ReadFeedback query={source} label="工作流来源" />
      <ReadFeedback query={templates} label="流程模板" />
      <ReadFeedback query={versions} label="模板版本" />
      <OperationFeedback
        pending={save.isPending || create.isPending}
        error={create.error || save.error}
        label={create.submittedAt > save.submittedAt ? '开始交付' : '保存工作流'}
        target={definition.name}
        receipt={
          unknown
            ? {
                message: '',
                evidence: (
                  <Button
                    onClick={() =>
                      navigate(
                        writeOutcomeUnknown(create.error)
                          ? '/execution-history'
                          : saved?.id
                            ? `/release-board?workflowId=${encodeURIComponent(saved.id)}`
                            : '/release-board',
                      )
                    }
                  >
                    {writeOutcomeUnknown(create.error) ? '核对执行记录' : '核对工作流'}
                  </Button>
                ),
              }
            : undefined
        }
      />
      {!quick ? (
        <Tabs
          activeKey={tab}
          onChange={(next) => {
            if (!sourceDirty) setTab(next)
          }}
          items={[
            { key: 'form', label: '目标与流程', disabled: sourceDirty },
            { key: 'source', label: 'YAML / JSON' },
            ...(saved ? [{ key: 'export', label: '已保存配置', disabled: sourceDirty }] : []),
            ...(saved
              ? [
                  { key: 'origin', label: '来源', disabled: sourceDirty },
                  { key: 'triggers', label: '触发器', disabled: sourceDirty },
                ]
              : []),
          ]}
        />
      ) : null}
      {tab === 'triggers' && saved ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <TriggerManager
            key={saved.id}
            targetKind="workflow"
            targetId={saved.id}
            workflowVersion={saved.version}
            webhookRefs={saved.definition.targets
              .filter((target) => target.action === 'build' || target.action === 'build_deploy')
              .flatMap((target) =>
                (target.repositoryRefs ?? []).flatMap((ref) =>
                  ref.refType === 'branch' || ref.refType === 'tag'
                    ? [
                        {
                          provider: 'gitlab_standard' as const,
                          repositoryId: ref.repositoryId,
                          refType: ref.refType,
                          refValue: ref.refName,
                        },
                      ]
                    : [],
                ),
              )}
          />
        </Suspense>
      ) : null}
      {tab === 'origin' && saved ? (
        <DocumentSourcePanel kind="Workflow" id={saved.id} version={saved.version} />
      ) : null}
      {tab === 'source' ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <SourceEditor
            key={sourceGeneration}
            kind="Workflow"
            value={workflowDocument(definition, saved?.id)}
            targetId={saved?.id}
            expectedRevision={saved?.version}
            disabled={busy || unknown || !canEdit}
            onDirtyChange={setSourceDirty}
            onValidated={(document) => {
              if (document.kind === 'Workflow') setDefinition(document.spec.definition)
            }}
          />
        </Suspense>
      ) : null}
      {tab === 'export' && saved ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <ExportView key={saved.version} kind="Workflow" id={saved.id} />
        </Suspense>
      ) : null}
      <Space
        orientation="vertical"
        size={20}
        style={{ width: '100%', display: tab === 'form' ? undefined : 'none' }}
      >
        {error ? <Alert type="error" showIcon title={error} /> : null}
        {retryOfBatchId ? (
          <Alert
            type="info"
            showIcon
            title="创建新的交付记录，重新检查当前配置和环境；保留原记录。"
          />
        ) : null}
        <Form layout="vertical" disabled={busy || unknown || !canEdit}>
          <Form.Item label="名称" required>
            <Input
              aria-label="工作流名称"
              value={definition.name}
              maxLength={160}
              onChange={(event) => update({ name: event.target.value })}
            />
          </Form.Item>
          {!quick ? (
            <>
              <Space wrap align="start">
                <Form.Item label="流程模板">
                  <Select
                    aria-label="流程模板"
                    style={{ width: 240 }}
                    allowClear
                    placeholder="自定义配置"
                    value={definition.workflowTemplateId}
                    loading={templates.isLoading || selectingRecipe}
                    options={readableQueryData(templates)
                      ?.filter(
                        (item) =>
                          item.enabled &&
                          item.publicationState !== 'deprecated' &&
                          (item.publishedVersion ?? 0) > 0 &&
                          item.definition?.mode === 'delivery_batch',
                      )
                      .map((item) => ({ value: item.id, label: item.name }))}
                    onChange={(id) => void selectRecipe(id)}
                  />
                </Form.Item>
                {definition.workflowTemplateId ? (
                  <Form.Item label="固定版本">
                    <Select
                      aria-label="流程模板版本"
                      style={{ width: 140 }}
                      value={definition.workflowTemplateVersion}
                      loading={versions.isLoading}
                      options={readableQueryData(versions)?.map((item) => ({
                        value: item.publishedVersion,
                        label: `v${item.publishedVersion}`,
                      }))}
                      onChange={(version) =>
                        void selectRecipe(definition.workflowTemplateId, version)
                      }
                    />
                  </Form.Item>
                ) : null}
                <Form.Item label="执行方式">
                  <Select
                    aria-label="执行方式"
                    style={{ width: 220 }}
                    value={definition.mode ?? 'service_serial'}
                    options={Object.entries(deliveryModeLabels).map(([value, label]) => ({
                      value,
                      label,
                    }))}
                    onChange={(mode) => update({ mode })}
                  />
                </Form.Item>
                {definition.mode === 'build_all_then_deploy' ? (
                  <Form.Item label="最大并发数">
                    <InputNumber
                      aria-label="最大并发数"
                      min={1}
                      max={32}
                      precision={0}
                      value={definition.maxConcurrency ?? 4}
                      onChange={(maxConcurrency) => update({ maxConcurrency: maxConcurrency ?? 4 })}
                    />
                  </Form.Item>
                ) : null}
              </Space>
              <Checkbox
                checked={definition.stopOnFailure ?? true}
                onChange={(event) => update({ stopOnFailure: event.target.checked })}
              >
                任一服务失败时停止本次发布
              </Checkbox>
            </>
          ) : null}
        </Form>
        {definition.mode === 'build_all_then_deploy' ? (
          <Typography.Text type="secondary">
            先完成全部构建，再按分组部署；同组并行，后组等待前组结束。成功依赖始终要求被依赖项完成。
          </Typography.Text>
        ) : null}
        <AdminTable
          rowKey="id"
          localSorting={false}
          pagination={false}
          viewportScroll={false}
          enableColumnSelection={false}
          toolbar={
            <Typography.Text>
              {deliveryServiceCount(definition.targets)} 个服务 / {definition.targets.length}{' '}
              个环境目标
            </Typography.Text>
          }
          toolbarExtra={
            !quick && canTrigger ? (
              <Button
                icon={<PlusOutlined />}
                disabled={busy || !canEdit || definition.targets.length >= 200}
                onClick={() => setEditing(newDeliveryTarget())}
              >
                添加目标
              </Button>
            ) : null
          }
          dataSource={definition.targets}
          scroll={{ x: 1020 }}
          columns={[
            {
              title: '顺序',
              key: 'order',
              width: 68,
              render: (_: unknown, _item: DeliveryTargetInput, index: number) => index + 1,
            },
            {
              title: '应用 / 服务',
              key: 'service',
              width: 210,
              render: (_: unknown, item: DeliveryTargetInput) => {
                const label = targetLabel(item)
                return (
                  <Space orientation="vertical" size={0}>
                    <Typography.Text type="secondary">{label.application}</Typography.Text>
                    <Button
                      type="link"
                      disabled={busy || unknown || !canEdit}
                      onClick={() => setEditing(item)}
                    >
                      {label.service}
                    </Button>
                  </Space>
                )
              },
            },
            {
              title: '环境',
              key: 'environment',
              width: 150,
              render: (_: unknown, item: DeliveryTargetInput) => targetLabel(item).environment,
            },
            {
              title: '方式',
              dataIndex: 'action',
              width: 140,
              render: (_: unknown, target: DeliveryTargetInput) =>
                deliveryTargetActionLabel(target),
            },
            ...(definition.mode === 'build_all_then_deploy'
              ? [
                  {
                    title: '分组',
                    key: 'group',
                    width: 90,
                    render: (_: unknown, item: DeliveryTargetInput) => (
                      <InputNumber
                        aria-label={`${targetLabel(item).service} 分组`}
                        min={1}
                        max={200}
                        precision={0}
                        disabled={busy || unknown || !canEdit}
                        value={(item.group ?? 0) + 1}
                        onChange={(group) => updateTarget({ ...item, group: (group ?? 1) - 1 })}
                      />
                    ),
                  },
                ]
              : []),
            ...(!quick
              ? [
                  {
                    title: '成功依赖',
                    key: 'dependencies',
                    width: 220,
                    render: (_: unknown, item: DeliveryTargetInput) => (
                      <Select
                        aria-label={`${targetLabel(item).service} 成功依赖`}
                        mode="multiple"
                        style={{ width: '100%' }}
                        value={item.dependsOn ?? []}
                        disabled={busy || unknown || !canEdit}
                        placeholder="无"
                        options={definition.targets
                          .filter((other) => other.id !== item.id)
                          .map((other) => ({
                            value: other.id,
                            label: `${targetLabel(other).service} / ${targetLabel(other).environment}`,
                          }))}
                        onChange={(dependsOn) => updateTarget({ ...item, dependsOn })}
                      />
                    ),
                  },
                ]
              : []),
            {
              title: '操作',
              key: 'actions',
              width: quick ? 90 : 180,
              render: (_: unknown, item: DeliveryTargetInput, index: number) => (
                <Space>
                  {quick ? (
                    <Button disabled={busy || unknown || !canEdit} onClick={() => setEditing(item)}>
                      调整
                    </Button>
                  ) : (
                    <>
                      {[-1, 1].map((direction) => (
                        <Button
                          key={direction}
                          icon={direction < 0 ? <ArrowUpOutlined /> : <ArrowDownOutlined />}
                          aria-label={`${direction < 0 ? '上移' : '下移'}第 ${index + 1} 项`}
                          disabled={
                            busy ||
                            !canEdit ||
                            index + direction < 0 ||
                            index + direction >= definition.targets.length
                          }
                          onClick={() => {
                            const targets = [...definition.targets]
                            ;[targets[index], targets[index + direction]] = [
                              targets[index + direction],
                              targets[index],
                            ]
                            update({ targets })
                          }}
                        />
                      ))}
                      <Button
                        icon={<CopyOutlined />}
                        aria-label={`复制第 ${index + 1} 项到其他环境`}
                        disabled={busy || !canEdit || definition.targets.length >= 200}
                        onClick={() =>
                          setEditing({
                            ...structuredClone(item),
                            id: crypto.randomUUID(),
                            applicationEnvironmentId: undefined,
                            releaseTargetId: undefined,
                            dependsOn: [],
                          })
                        }
                      />
                      <Button
                        icon={<DeleteOutlined />}
                        aria-label={`移除第 ${index + 1} 项`}
                        disabled={busy || unknown || !canEdit}
                        onClick={() =>
                          update({
                            targets: definition.targets
                              .filter((other) => other.id !== item.id)
                              .map((other) => ({
                                ...other,
                                dependsOn: other.dependsOn?.filter((id) => id !== item.id),
                              })),
                          })
                        }
                      />
                    </>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Space>
      {editing ? (
        <DeliveryTargetEditor
          key={editing.id}
          target={editing}
          fixed={quick}
          blockObserveOnly={selectTargets}
          onClose={() => setEditing(undefined)}
          onSave={(item) => {
            updateTarget(item)
            setEditing(undefined)
          }}
        />
      ) : null}
    </Drawer>
  )
}
