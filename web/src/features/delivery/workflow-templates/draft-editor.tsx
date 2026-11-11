import { TemplateMigrationFields, migrationPolicyInput } from '../template-migration'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { isApiError } from '@/services/api-error'
import { writeOutcomeUnknown } from '@/utils/query-feedback'
import { lazy, Suspense, useCallback, useRef, useState } from 'react'
import {
  Alert,
  App,
  Button,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Switch,
  Tabs,
} from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { ManagementState } from '@/components/management-list'
import {
  analyzeReleaseDagDefinition,
  normalizeReleaseDagDefinition,
  type ReleaseDagDefinition,
} from '@/components/release-flow-dag-definition'
import { deliveryMutations } from '../mutations'
import { deliveryModeLabels } from '../batches/model'
import type { WorkflowTemplate } from '../types'
import { workflowTemplateDocument } from '../documents/model'
import { DeliveryDocumentSourceEditor, DocumentDiagnostics } from '../documents/source-editor'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import { downloadText } from '@/utils/download'
import { useUnsavedDocument } from '../documents/use-unsaved-document'
import { deliveryQueries } from '../queries'
import { DocumentSourcePanel } from '../template-sources/source-panel'

export function retainsWorkflowDefinition(original: unknown, represented: unknown): boolean {
  if (original && typeof original === 'object') {
    if (
      !represented ||
      typeof represented !== 'object' ||
      Array.isArray(original) !== Array.isArray(represented)
    )
      return false
    return Object.entries(original).every(([key, value]) =>
      retainsWorkflowDefinition(value, (represented as Record<string, unknown>)[key]),
    )
  }
  return original === represented
}

const DagEditor = lazy(() =>
  import('@/components/release-flow-dag-editor').then((module) => ({
    default: module.ReleaseFlowDagEditor,
  })),
)

export function WorkflowTemplateDraftEditor({
  template,
  copiedFrom,
  onClose,
  onSaved,
  onReload,
}: {
  template?: WorkflowTemplate
  copiedFrom?: { id: string; revision: number; version?: number }
  onClose: () => void
  onSaved: (template: WorkflowTemplate) => void
  onReload: (template: WorkflowTemplate) => void
}) {
  const { message } = App.useApp()
  const client = useQueryClient()
  const source = useQuery(deliveryQueries.documents.source('WorkflowTemplate', template?.id ?? ''))
  const readOnly = Boolean(template?.id && (!source.isSuccess || source.data.association))
  const reload = useQuery(deliveryQueries.workflowTemplates.detail(template?.id || '', false))
  const create = useMutation(deliveryMutations.workflowTemplates.create(client))
  const update = useMutation(deliveryMutations.workflowTemplates.update(client))
  const [form] = Form.useForm()
  const values = Form.useWatch([], form)
  const [tab, setTab] = useState('form')
  const [sourceDirty, setSourceDirty] = useState(false)
  const [definition, setDefinition] = useState<Record<string, unknown>>(() =>
    structuredClone(
      template?.definition ?? {
        mode: 'delivery_batch',
        schemaVersion: 1,
        stages: ['build', 'plan', 'deploy', 'health'],
        executionMode: 'build_all_then_deploy',
        stopOnFailure: true,
        maxConcurrency: 4,
      },
    ),
  )
  const [dirty, setDirty] = useState(!template?.id)
  useUnsavedDocument(dirty || sourceDirty)
  const [initialDag, setInitialDag] = useState(() =>
    normalizeReleaseDagDefinition(template?.definition),
  )
  const [dagGeneration, setDagGeneration] = useState(0)
  const compatibility = useQuery({
    queryKey: deliveryKeys.workflowTemplates.documentCompatibility(
      template?.id,
      template?.revision,
      template?.key,
    ),
    queryFn: () =>
      deliveryApi.documents.preview({
        validateOnly: true,
        files: [
          {
            path: 'template.json',
            content: JSON.stringify(workflowTemplateDocument(template!)),
            targetId: template?.id || undefined,
            expectedRevision: template?.id ? template.revision : undefined,
          },
        ],
      }),
    enabled: Boolean(template && template.definition?.mode !== 'delivery_batch'),
    retry: false,
  })
  const checkingLegacy = Boolean(template && template.definition?.mode !== 'delivery_batch')
  const compatible = !checkingLegacy || compatibility.data?.valid === true
  const changeDag = useCallback(
    (value: ReleaseDagDefinition) => {
      setDefinition({ ...value })
      if (JSON.stringify(value) !== JSON.stringify(initialDag)) setDirty(true)
    },
    [initialDag],
  )
  const recipe = definition.mode === 'delivery_batch'
  const submitting = useRef(false)
  const unknown = writeOutcomeUnknown(create.error) || writeOutcomeUnknown(update.error)
  const busy = create.isPending || update.isPending
  const save = async () => {
    if (submitting.current || busy || unknown || sourceDirty || !compatible || readOnly) return
    submitting.current = true
    try {
      const values = await form.validateFields()
      if (!recipe) {
        const failure = analyzeReleaseDagDefinition(definition).issues.find(
          (issue) => issue.severity === 'error',
        )
        if (failure) {
          message.error(failure.message)
          return
        }
      }
      const payload = {
        ...values,
        migrationPolicy: migrationPolicyInput(values.migrationPolicy),
        definition,
        publish: false,
      }
      const result = template?.id
        ? await update.mutateAsync({
            id: template.id,
            payload: { ...payload, expectedRevision: template.revision },
          })
        : await create.mutateAsync({ ...payload, copiedFrom })
      message.success('草稿已保存')
      onSaved(result)
    } catch {
      /* Field and mutation feedback retain the draft. */
    } finally {
      submitting.current = false
    }
  }
  const patch = (value: Record<string, unknown>) => {
    setDefinition((current) => ({ ...current, ...value }))
    setDirty(true)
  }
  return (
    <Modal
      open
      title={template?.id ? '编辑流程模板草稿' : '新建流程模板'}
      width={recipe ? 760 : 1180}
      okText="保存草稿"
      confirmLoading={busy}
      okButtonProps={{
        'aria-label': '保存草稿',
        disabled: unknown || sourceDirty || !compatible || readOnly,
      }}
      cancelButtonProps={{ disabled: busy }}
      onOk={() => void save()}
      onCancel={() => {
        if (!busy && (!(dirty || sourceDirty) || window.confirm('放弃未保存的模板修改？')))
          onClose()
      }}
    >
      <ReadFeedback query={reload} label="当前模板草稿" />
      {template?.id &&
      (writeOutcomeUnknown(update.error) ||
        (isApiError(update.error) && update.error.status === 409)) ? (
        <Alert
          type="warning"
          showIcon
          title="草稿已保留，请核对当前模板"
          description="重新加载会放弃本次修改，载入服务器上的当前草稿和版本。"
          action={
            <Button
              loading={reload.isFetching}
              onClick={async () => {
                if (!window.confirm('放弃本次修改，重新加载当前模板草稿？')) return
                const result = await reload.refetch()
                if (!result.error && result.data) onReload(result.data)
              }}
            >
              重新加载草稿
            </Button>
          }
        />
      ) : null}
      {writeOutcomeUnknown(create.error) ? (
        <Button onClick={onClose}>返回模板列表核对</Button>
      ) : null}
      <ReadFeedback query={source} label="模板来源" />
      <ReadFeedback query={compatibility} label="兼容性检查" />
      <MutationFeedback
        operations={[
          {
            mutation: create,
            label: '新建模板草稿',
            target: create.variables?.name ? String(create.variables.name) : undefined,
          },
          { mutation: update, label: '保存模板草稿', target: update.variables?.id },
        ]}
      />
      {template?.id && readOnly ? (
        <DocumentSourcePanel kind="WorkflowTemplate" id={template.id} />
      ) : null}
      {checkingLegacy && compatibility.isPending ? <ManagementState kind="loading" /> : null}
      {checkingLegacy && !compatible && !compatibility.isPending ? (
        <Space orientation="vertical" style={{ width: '100%' }}>
          <Alert
            type="warning"
            showIcon
            title="此模板暂以只读方式保留"
            description={
              compatibility.error?.message || '当前定义不能完整映射为新文档格式，原字段保持不变。'
            }
          />
          <DocumentDiagnostics items={compatibility.data?.diagnostics ?? []} />
          <Button onClick={() => void compatibility.refetch()}>重新检查</Button>
          <Button
            onClick={() =>
              downloadText(`${template?.key}.legacy.json`, JSON.stringify(template, null, 2))
            }
          >
            导出旧格式
          </Button>
        </Space>
      ) : null}
      <Tabs
        activeKey={tab}
        onChange={(next) => {
          if (sourceDirty && next !== 'source') return
          setTab(next)
        }}
        items={[
          { key: 'form', label: '流程与参数', disabled: sourceDirty },
          { key: 'source', label: 'YAML / JSON' },
        ]}
      />
      {sourceDirty ? (
        <Alert type="info" showIcon title="请校验并同步源码，或还原源码后继续编辑表单。" />
      ) : null}
      <div style={{ display: tab === 'form' ? undefined : 'none' }}>
        <Form
          form={form}
          layout="vertical"
          disabled={busy || !compatible || readOnly}
          initialValues={{
            key: template?.key,
            name: template?.name,
            description: template?.description,
            migrationPolicy: template?.migrationPolicy,
            category: template?.category || 'release',
            enabled: template?.enabled ?? true,
          }}
          onValuesChange={() => setDirty(true)}
        >
          <Form.Item
            name="key"
            label="模板 Key"
            rules={[{ required: true, whitespace: true, message: '请输入模板 Key' }]}
          >
            <Input />
          </Form.Item>
          <Form.Item
            name="name"
            label="模板名称"
            rules={[{ required: true, whitespace: true, message: '请输入模板名称' }]}
          >
            <Input />
          </Form.Item>
          <TemplateMigrationFields />
          <Form.Item name="description" label="描述">
            <Input.TextArea rows={2} />
          </Form.Item>
          <Space align="start">
            <Form.Item name="category" label="分类">
              <Select
                style={{ width: 180 }}
                options={[
                  { value: 'release', label: '发布' },
                  { value: 'verification', label: '验证' },
                  { value: 'promotion', label: '环境推进' },
                ]}
              />
            </Form.Item>
            <Form.Item name="enabled" label="启用" valuePropName="checked">
              <Switch />
            </Form.Item>
          </Space>
          {recipe ? (
            <Space orientation="vertical" style={{ width: '100%' }}>
              <Form.Item label="执行方式">
                <Select
                  value={String(definition.executionMode)}
                  options={Object.entries(deliveryModeLabels).map(([value, label]) => ({
                    value,
                    label,
                  }))}
                  onChange={(value) => patch({ executionMode: value })}
                />
              </Form.Item>
              <Space>
                <Form.Item label="失败后停止">
                  <Switch
                    checked={definition.stopOnFailure !== false}
                    onChange={(value) => patch({ stopOnFailure: value })}
                  />
                </Form.Item>
                <Form.Item label="最大并行数">
                  <InputNumber
                    min={1}
                    max={32}
                    precision={0}
                    value={Number(definition.maxConcurrency) || 4}
                    onChange={(value) => {
                      if (value) patch({ maxConcurrency: value })
                    }}
                  />
                </Form.Item>
              </Space>
            </Space>
          ) : compatible &&
            !readOnly &&
            retainsWorkflowDefinition(definition, normalizeReleaseDagDefinition(definition)) ? (
            <Suspense fallback={<ManagementState kind="loading" />}>
              <DagEditor
                key={dagGeneration}
                height={520}
                initialDefinition={initialDag}
                variant="embedded"
                onChange={changeDag}
              />
            </Suspense>
          ) : (
            <pre className="soha-json-block">{JSON.stringify(definition, null, 2)}</pre>
          )}
        </Form>
      </div>
      {tab === 'source' ? (
        <DeliveryDocumentSourceEditor
          value={workflowTemplateDocument({
            key: values?.key ?? '',
            name: values?.name ?? '',
            description: values?.description,
            category: values?.category,
            enabled: values?.enabled ?? true,
            definition,
          })}
          kind="WorkflowTemplate"
          targetId={template?.id || undefined}
          expectedRevision={template?.id ? template.revision : undefined}
          disabled={busy || !compatible || readOnly}
          onDirtyChange={setSourceDirty}
          onValidated={(document) => {
            if (document.kind !== 'WorkflowTemplate') return
            form.setFieldsValue({
              key: document.metadata.name,
              name: document.metadata.displayName || document.metadata.name,
              description: document.metadata.description ?? '',
              category: document.spec.category,
              enabled: document.spec.enabled ?? true,
            })
            setDefinition(document.spec.definition)
            setInitialDag(normalizeReleaseDagDefinition(document.spec.definition))
            setDagGeneration((value) => value + 1)
            setDirty(true)
          }}
        />
      ) : null}
    </Modal>
  )
}
