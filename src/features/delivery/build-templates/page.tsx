import { TemplateMigrationFields, migrationPolicyInput } from '../template-migration'
import type { TemplateMigrationPolicy } from '@opensoha/contracts/gen/ts/sohaapi'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import './styles.css'
import {
  App,
  Button,
  Form,
  Input,
  Popconfirm,
  Select,
  Space,
  Switch,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import {
  CopyOutlined,
  UndoOutlined,
  UploadOutlined,
  DeleteOutlined,
  EditOutlined,
  PlusOutlined,
  SaveOutlined,
} from '@ant-design/icons'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementSearchableListPane,
  ManagementState,
  TemplateDesignerShell,
} from '@/components/management-list'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { isApiError } from '@/services/api-error'
import { BooleanTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { TemplateUsageImpactPanel } from '../template-usage-impact'
import { TemplatePublicationStatus, TemplateVersionHistory } from '../template-versions'
import { TemplateUpdateEntry } from '../template-updates/entry'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import type { BuildTemplate } from '../types'
import { buildTemplateDocument } from '../documents/model'
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

const { Text } = Typography

type JsonObject = Record<string, unknown>

export interface BuildTemplateFormValues {
  migrationPolicy?: TemplateMigrationPolicy
  key?: string
  name?: string
  description?: string
  builderKind?: string
  dockerfileTemplate?: string
  buildCommandsText?: string
  originalBuildCommands?: string[]
  variableSchemaText?: string
  defaultVariablesText?: string
  variables?: BuildTemplateVariableFormValue[]
  enabled?: boolean
}

export interface BuildTemplatePayload {
  migrationPolicy?: TemplateMigrationPolicy
  key?: string
  name?: string
  description?: string
  builderKind?: string
  dockerfileTemplate?: string
  buildCommands: string[]
  variableSchema: JsonObject
  defaultVariables: JsonObject
  enabled?: boolean
}

export interface BuildTemplateVariableFormValue {
  key?: string
  label?: string
  type?: string
  required?: boolean
  defaultValue?: string
  description?: string
}

type BuildTemplateListItem = {
  builderKind?: string
  commandCount: number
  description?: string
  enabled: boolean
  id: string
  isDraft?: boolean
  key: string
  name: string
  template?: BuildTemplate
  updatedAt?: string
  variableCount: number
}

function parseJSONObject(raw: unknown, field: string): JsonObject {
  const value = typeof raw === 'string' ? raw.trim() : ''
  if (!value) return {}
  try {
    const parsed = JSON.parse(value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
      throw new Error('invalid')
    }
    return parsed
  } catch {
    throw new Error(`${field} 需要是合法 JSON 对象`)
  }
}

function splitLines(raw: unknown) {
  return String(raw || '')
    .split('\n')
    .map((item) => item.trim())
    .filter(Boolean)
}

function trimFormString(raw: unknown) {
  return String(raw ?? '').trim()
}

function buildTemplateVariableSchema(
  variables: BuildTemplateVariableFormValue[] | undefined,
  original: JsonObject,
) {
  const schema: JsonObject = {}
  for (const item of variables ?? []) {
    const key = trimFormString(item.key)
    if (!key) continue
    const previous = original[key]
    schema[key] = {
      ...(previous && typeof previous === 'object' && !Array.isArray(previous) ? previous : {}),
      type: item.type || 'string',
      title: trimFormString(item.label) || key,
      description: trimFormString(item.description),
      required: Boolean(item.required),
    }
  }
  return schema
}

function buildTemplateDefaultVariables(
  variables: BuildTemplateVariableFormValue[] | undefined,
  original: JsonObject,
  originalSchema: JsonObject,
) {
  const defaults: JsonObject = Object.fromEntries(
    Object.entries(original).filter(([key]) => !(key in originalSchema)),
  )
  for (const item of variables ?? []) {
    const key = trimFormString(item.key)
    if (!key) continue
    const raw = item.defaultValue
    if (raw === undefined || (raw === '' && original[key] !== '')) continue
    if (item.type === 'number' || item.type === 'integer') {
      const parsed = Number(raw)
      if (
        !raw.trim() ||
        !Number.isFinite(parsed) ||
        (item.type === 'integer' && !Number.isSafeInteger(parsed))
      )
        throw new Error(`${key} 默认值必须是有效${item.type === 'integer' ? '整数' : '数字'}`)
      defaults[key] = parsed
      continue
    }
    if (item.type === 'boolean') {
      if (raw !== 'true' && raw !== 'false') throw new Error(`${key} 默认值必须是 true 或 false`)
      defaults[key] = raw === 'true'
      continue
    }
    defaults[key] = raw
  }
  return defaults
}

function extractBuildTemplateVariables(
  template: Pick<BuildTemplate, 'variableSchema' | 'defaultVariables'> | undefined,
): BuildTemplateVariableFormValue[] {
  if (
    !template?.variableSchema ||
    typeof template.variableSchema !== 'object' ||
    Array.isArray(template.variableSchema)
  )
    return []
  return Object.entries(template.variableSchema).map(([key, value]) => {
    const spec =
      value && typeof value === 'object' && !Array.isArray(value) ? (value as JsonObject) : {}
    const defaultValue = template.defaultVariables?.[key]
    return {
      key,
      label: String(spec.title || spec.label || key),
      type: String(spec.type || 'string'),
      required: Boolean(spec.required),
      defaultValue: defaultValue === undefined ? '' : String(defaultValue),
      description: String(spec.description || ''),
    }
  })
}

function defaultBuildTemplateValues(
  key = `build-template-${Date.now().toString(36)}`,
): BuildTemplateFormValues {
  return {
    key,
    name: '新建构建模板',
    description: '',
    builderKind: 'docker',
    dockerfileTemplate:
      'FROM node:22-alpine\nWORKDIR /app\nCOPY package*.json ./\nRUN npm ci\nCOPY . .\nRUN npm run build\n',
    buildCommandsText: 'npm ci\nnpm run build',
    variableSchemaText: '{}',
    defaultVariablesText: '{}',
    variables: [
      {
        key: 'imageTag',
        label: '镜像 Tag',
        type: 'string',
        required: true,
        defaultValue: 'latest',
        description: '默认镜像标签',
      },
    ],
    enabled: true,
  }
}

function buildTemplateToFormValues(
  template: Omit<BuildTemplate, 'id' | 'createdAt' | 'updatedAt'>,
): BuildTemplateFormValues {
  return {
    key: template.key,
    name: template.name,
    description: template.description ?? '',
    builderKind: template.builderKind ?? 'docker',
    dockerfileTemplate: template.dockerfileTemplate ?? '',
    buildCommandsText: (template.buildCommands ?? []).join('\n'),
    originalBuildCommands: template.buildCommands,
    variableSchemaText: JSON.stringify(template.variableSchema ?? {}, null, 2),
    defaultVariablesText: JSON.stringify(template.defaultVariables ?? {}, null, 2),
    migrationPolicy: template.migrationPolicy,
    variables: extractBuildTemplateVariables(template),
    enabled: template.enabled,
  }
}

export function buildBuildTemplatePayloadFromDesigner(
  values: BuildTemplateFormValues,
): BuildTemplatePayload {
  const variables = values.variables ?? []
  const hasStructuredVariables = values.variables !== undefined
  return {
    key: values.key,
    name: values.name,
    description: values.description,
    builderKind: values.builderKind,
    dockerfileTemplate: values.dockerfileTemplate,
    buildCommands:
      values.originalBuildCommands &&
      values.buildCommandsText === values.originalBuildCommands.join('\n')
        ? values.originalBuildCommands
        : splitLines(values.buildCommandsText),
    variableSchema: hasStructuredVariables
      ? buildTemplateVariableSchema(
          variables,
          parseJSONObject(values.variableSchemaText, '变量 Schema'),
        )
      : parseJSONObject(values.variableSchemaText, '变量 Schema'),
    defaultVariables: hasStructuredVariables
      ? buildTemplateDefaultVariables(
          variables,
          parseJSONObject(values.defaultVariablesText, '默认变量'),
          parseJSONObject(values.variableSchemaText, '变量 Schema'),
        )
      : parseJSONObject(values.defaultVariablesText, '默认变量'),
    enabled: values.enabled,
    migrationPolicy: migrationPolicyInput(values.migrationPolicy),
  }
}

export function buildBuildTemplatePayload(values: BuildTemplateFormValues): BuildTemplatePayload {
  return {
    key: values.key,
    name: values.name,
    description: values.description,
    builderKind: values.builderKind,
    dockerfileTemplate: values.dockerfileTemplate,
    buildCommands: splitLines(values.buildCommandsText),
    variableSchema: parseJSONObject(values.variableSchemaText, '变量 Schema'),
    defaultVariables: parseJSONObject(values.defaultVariablesText, '默认变量'),
    enabled: values.enabled,
    migrationPolicy: migrationPolicyInput(values.migrationPolicy),
  }
}

export function BuildTemplatesPage() {
  const { message } = App.useApp()
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreate = hasPermission(permissionSnapshot, 'delivery.build-templates.create')
  const canUpdate = hasPermission(permissionSnapshot, 'delivery.build-templates.update')
  const canDelete = hasPermission(permissionSnapshot, 'delivery.build-templates.delete')
  const [searchParams, setSearchParams] = useSearchParams()
  const [form] = Form.useForm<BuildTemplateFormValues>()
  const [selectedTemplateId, setSelectedTemplateId] = useState('')
  const [searchText, setSearchText] = useState('')
  const [activeTabKey, setActiveTabKey] = useState('basic')
  const [isDirty, setIsDirty] = useState(false)
  const [sourceDirty, setSourceDirty] = useState(false)
  const [sourceGeneration, setSourceGeneration] = useState(0)
  const [importing, setImporting] = useState(false)
  const [formSnapshot, setFormSnapshot] = useState<BuildTemplateFormValues>({})
  const creationBaseline = useRef<Set<string>>()
  const copiedFromRef = useRef<{ id: string; revision: number }>()
  const loadedRevisionRef = useRef<number>()
  const loadedQueryIdRef = useRef<string | null>()

  const templatesQuery = useQuery(deliveryQueries.buildTemplates.list())
  const templates = readableQueryData(templatesQuery) ?? []
  const selectedTemplate =
    selectedTemplateId && selectedTemplateId !== 'new'
      ? (templates.find((item) => item.id === selectedTemplateId) ?? null)
      : null
  const selectedTemplateUsageQuery = useQuery(
    deliveryQueries.buildTemplates.usage(selectedTemplate?.id ?? '', Boolean(selectedTemplate?.id)),
  )
  const selectedTemplateUsage = readableQueryData(selectedTemplateUsageQuery)
  const isNewDraft = selectedTemplateId === 'new'
  const source = useQuery(
    deliveryQueries.documents.source('BuildTemplate', selectedTemplate?.id ?? ''),
  )
  const sourceWritable = source.isSuccess && !source.data.association
  const hasSelection = isNewDraft || !!selectedTemplate
  const canSave =
    (isNewDraft ? canCreate : canUpdate && sourceWritable) &&
    selectedTemplate?.publicationState !== 'deprecated'

  const createOptions = deliveryMutations.buildTemplates.create(queryClient)
  const createMutation = useMutation({
    ...createOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void createOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('构建模板创建成功')
    },
  })
  const updateOptions = deliveryMutations.buildTemplates.update(queryClient)
  const updateMutation = useMutation({
    ...updateOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void updateOptions.onSuccess?.(result, variables, onMutateResult, context)
      message.success('构建模板更新成功')
    },
  })
  const publishOptions = deliveryMutations.buildTemplates.publish(queryClient)
  const publishMutation = useMutation({
    ...publishOptions,
    onSuccess: (result, variables, onMutateResult, context) => {
      void publishOptions.onSuccess?.(result, variables, onMutateResult, context)
      loadTemplate(result)
      message.success(`已发布 v${result.publishedVersion}`)
    },
  })
  const deleteOptions = deliveryMutations.buildTemplates.delete(queryClient)
  const deleteMutation = useMutation({
    ...deleteOptions,
    onSuccess: (result, deletedId, onMutateResult, context) => {
      void deleteOptions.onSuccess?.(result, deletedId, onMutateResult, context)
      message.success('构建模板已废弃，已发布版本继续保留')
      if (selectedTemplateId === deletedId) {
        const nextTemplate = templates.find((item) => item.id !== deletedId)
        if (nextTemplate) {
          loadTemplate(nextTemplate)
        } else {
          form.resetFields()
          setSelectedTemplateId('')
          setFormSnapshot({})
          setIsDirty(false)
          updateTemplateSearchParam()
        }
      }
    },
  })

  const submitting = useRef(false)
  const locked = [createMutation, updateMutation, publishMutation, deleteMutation].some(
    (mutation) => mutation.isPending || writeOutcomeUnknown(mutation.error),
  )
  const changePublication = async (action: 'publish' | 'deprecate') => {
    if (
      submitting.current ||
      locked ||
      !selectedTemplate ||
      templatesQuery.isError ||
      source.isError
    )
      return
    if (
      (action === 'publish' && (!canUpdate || !loadedRevisionRef.current)) ||
      (action === 'deprecate' && !canDelete)
    )
      return
    submitting.current = true
    try {
      if (action === 'publish')
        await publishMutation.mutateAsync({
          id: selectedTemplate.id,
          expectedRevision: loadedRevisionRef.current!,
        })
      else await deleteMutation.mutateAsync(selectedTemplate.id)
    } catch {
      /* The mutation receipt retains the original target. */
    } finally {
      submitting.current = false
    }
  }

  const updateTemplateSearchParam = useCallback(
    (templateId?: string) => {
      setSearchParams(
        (current) => {
          const next = new URLSearchParams(current)
          if (templateId) {
            next.set('templateId', templateId)
          } else {
            next.delete('templateId')
          }
          return next
        },
        { replace: true },
      )
    },
    [setSearchParams],
  )

  const confirmDiscardChanges = useCallback(() => {
    if (!isDirty && !sourceDirty) return true
    return window.confirm('当前构建模板有未保存更改，确认放弃？')
  }, [isDirty, sourceDirty])

  const applyFormValues = useCallback(
    (values: BuildTemplateFormValues, dirtyAfterApply: boolean) => {
      setFormSnapshot(values)
      form.setFieldsValue(values)
      setIsDirty(dirtyAfterApply)
      setSourceDirty(false)
      setSourceGeneration((current) => current + 1)
    },
    [form],
  )

  const loadTemplate = useCallback(
    (
      template: BuildTemplate,
      options?: {
        dirtyAfterLoad?: boolean
        formOverrides?: BuildTemplateFormValues
        tabKey?: string
      },
    ) => {
      const values = {
        ...buildTemplateToFormValues(template),
        ...options?.formOverrides,
      }
      copiedFromRef.current = undefined
      loadedRevisionRef.current = template.revision
      setSelectedTemplateId(template.id)
      setActiveTabKey(options?.tabKey ?? 'basic')
      applyFormValues(values, Boolean(options?.dirtyAfterLoad))
      updateTemplateSearchParam(template.id)
    },
    [applyFormValues, updateTemplateSearchParam],
  )

  useEffect(() => {
    if (!templates.length || isDirty || sourceDirty) return
    const queryTemplateId = searchParams.get('templateId')
    // Router transitions can leave the old URL visible after a local selection.
    const queryChanged = queryTemplateId !== loadedQueryIdRef.current
    loadedQueryIdRef.current = queryTemplateId
    const queryTemplate = queryTemplateId
      ? templates.find((item) => item.id === queryTemplateId)
      : undefined
    if (queryChanged && queryTemplate && queryTemplate.id !== selectedTemplateId) {
      loadTemplate(queryTemplate)
      return
    }
    if (!selectedTemplateId) {
      loadTemplate(queryTemplate ?? templates[0])
    }
  }, [isDirty, sourceDirty, loadTemplate, searchParams, selectedTemplateId, templates])

  useEffect(() => {
    if (!isDirty && !sourceDirty) return undefined
    const beforeUnload = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = ''
    }
    window.addEventListener('beforeunload', beforeUnload)
    return () => window.removeEventListener('beforeunload', beforeUnload)
  }, [isDirty, sourceDirty])

  const listItems = useMemo<BuildTemplateListItem[]>(() => {
    const fromValues = (
      values: BuildTemplateFormValues,
      id: string,
      isDraft: boolean,
      template?: BuildTemplate,
    ): BuildTemplateListItem => ({
      id,
      key: values.key || 'new-build-template',
      name: values.name || '新建构建模板草稿',
      description: values.description,
      builderKind: values.builderKind || 'docker',
      commandCount: splitLines(values.buildCommandsText).length,
      variableCount: (values.variables ?? []).filter((item) => trimFormString(item.key)).length,
      enabled: values.enabled !== false,
      isDraft,
      template,
      updatedAt: template?.updatedAt,
    })
    const items = templates.map((template) => {
      if (template.id === selectedTemplateId) {
        return fromValues(formSnapshot, template.id, false, template)
      }
      return {
        id: template.id,
        key: template.key,
        name: template.name,
        description: template.description,
        builderKind: template.builderKind || 'docker',
        commandCount: template.buildCommands?.length ?? 0,
        variableCount: Object.keys(template.variableSchema ?? {}).length,
        enabled: template.enabled,
        template,
        updatedAt: template.updatedAt,
      }
    })
    if (isNewDraft) {
      return [fromValues(formSnapshot, 'new', true), ...items]
    }
    return items
  }, [formSnapshot, isNewDraft, selectedTemplateId, templates])

  const visibleListItems = useMemo(() => {
    const keyword = searchText.trim().toLowerCase()
    if (!keyword) return listItems
    return listItems.filter((item) =>
      [item.name, item.key, item.description, item.builderKind].some((value) =>
        String(value || '')
          .toLowerCase()
          .includes(keyword),
      ),
    )
  }, [listItems, searchText])

  const previewState = useMemo(() => {
    if (!hasSelection) return { error: '', json: '' }
    try {
      return {
        error: '',
        json: JSON.stringify(
          buildTemplateDocument(buildBuildTemplatePayloadFromDesigner(formSnapshot)),
          null,
          2,
        ),
      }
    } catch (error) {
      return {
        error: error instanceof Error ? error.message : '构建模板预览生成失败',
        json: '',
      }
    }
  }, [formSnapshot, hasSelection])

  const handleNewTemplate = () => {
    if (!confirmDiscardChanges()) return
    copiedFromRef.current = undefined
    const values = defaultBuildTemplateValues()
    setSelectedTemplateId('new')
    setActiveTabKey('basic')
    applyFormValues(values, true)
    updateTemplateSearchParam()
  }

  const handleSelectListItem = (item: BuildTemplateListItem) => {
    if (item.id === selectedTemplateId) return
    if (!confirmDiscardChanges()) return
    if (item.isDraft) {
      setSelectedTemplateId('new')
      return
    }
    if (item.template) {
      loadTemplate(item.template)
    }
  }

  const handleCancelChanges = () => {
    if (selectedTemplate) {
      loadTemplate(selectedTemplate)
      return
    }
    const firstTemplate = templates[0]
    if (firstTemplate) {
      loadTemplate(firstTemplate)
      return
    }
    form.resetFields()
    setSelectedTemplateId('')
    setFormSnapshot({})
    setIsDirty(false)
    updateTemplateSearchParam()
  }

  const handleSave = async () => {
    if (submitting.current || locked || sourceDirty || !canSave) return
    submitting.current = true
    try {
      const values = { ...formSnapshot, ...(await form.validateFields()) }
      const payload = buildBuildTemplatePayloadFromDesigner(values)
      if (selectedTemplate) {
        const updated = await updateMutation.mutateAsync({
          id: selectedTemplate.id,
          payload: { ...payload, publish: false, expectedRevision: loadedRevisionRef.current },
        })
        loadedRevisionRef.current = updated.revision
        setFormSnapshot(values)
        setIsDirty(false)
        return
      }
      creationBaseline.current = templatesQuery.isSuccess
        ? new Set(templates.map((item) => item.id))
        : undefined
      const createdTemplate = await createMutation.mutateAsync({
        ...payload,
        publish: false,
        copiedFrom: copiedFromRef.current,
      })
      copiedFromRef.current = undefined
      setFormSnapshot(values)
      setIsDirty(false)
      if (createdTemplate?.id) {
        loadedRevisionRef.current = createdTemplate.revision
        setSelectedTemplateId(createdTemplate.id)
        updateTemplateSearchParam(createdTemplate.id)
      }
    } catch (error) {
      if (error instanceof Error && !isApiError(error)) message.error(error.message)
    } finally {
      submitting.current = false
    }
  }

  const handleTemplateEnabledChange = (item: BuildTemplateListItem, enabled: boolean) => {
    if (!item.isDraft && (item.id !== selectedTemplateId || !sourceWritable)) return
    if (item.id !== selectedTemplateId) {
      if (!confirmDiscardChanges()) return
      if (item.template) {
        loadTemplate(item.template, {
          dirtyAfterLoad: true,
          formOverrides: { enabled },
          tabKey: 'basic',
        })
      }
      return
    }
    form.setFieldsValue({ enabled })
    setFormSnapshot((current) => ({ ...current, enabled }))
    setIsDirty(true)
  }

  const designerTabs = [
    {
      key: 'origin',
      label: '来源',
      children: <DocumentSourcePanel kind="BuildTemplate" id={selectedTemplate?.id ?? ''} />,
    },
    {
      key: 'basic',
      label: '基础信息',
      children: (
        <div className="soha-build-template-form-grid">
          <Form.Item
            name="key"
            label="模板 Key"
            rules={[{ required: true, message: '请输入模板 Key' }]}
          >
            <Input placeholder="docker-node" />
          </Form.Item>
          <Form.Item
            name="name"
            label="模板名称"
            rules={[{ required: true, message: '请输入模板名称' }]}
          >
            <Input placeholder="Node Docker 标准构建" />
          </Form.Item>
          <TemplateMigrationFields />
          <Form.Item name="builderKind" label="Builder Kind">
            <Select
              options={[
                { value: 'docker', label: 'docker' },
                { value: 'buildx', label: 'buildx' },
                { value: 'kaniko', label: 'kaniko' },
                { value: 'custom', label: 'custom' },
              ]}
            />
          </Form.Item>
          <Form.Item
            className="soha-build-template-switch-field"
            name="enabled"
            label="启用"
            valuePropName="checked"
          >
            <Switch />
          </Form.Item>
          <Form.Item
            className="soha-build-template-form-grid__wide"
            name="description"
            label="描述"
          >
            <Input.TextArea rows={3} placeholder="说明适用语言、构建器、缓存策略和制品输出约定" />
          </Form.Item>
        </div>
      ),
    },
    {
      key: 'dockerfile',
      label: 'Dockerfile',
      children: (
        <div className="soha-build-template-editor-pane">
          <Text type="secondary">
            构建时生成独立 Dockerfile；命令留空时按构建执行器构建并推送镜像。使用 {'{{变量名}}'}{' '}
            引用简单值。
          </Text>
          <Form.Item name="dockerfileTemplate" label="Dockerfile 模板">
            <Input.TextArea
              className="soha-build-template-code-area"
              rows={18}
              spellCheck={false}
            />
          </Form.Item>
        </div>
      ),
    },
    {
      key: 'commands',
      label: '构建命令',
      children: (
        <div className="soha-build-template-editor-pane">
          <Text type="secondary">每行一条命令，执行器会按顺序生成构建步骤。</Text>
          <Form.Item name="buildCommandsText" label="命令列表">
            <Input.TextArea
              className="soha-build-template-code-area"
              rows={14}
              placeholder="npm ci&#10;npm run build"
              spellCheck={false}
            />
          </Form.Item>
        </div>
      ),
    },
    {
      key: 'variables',
      label: '变量',
      children: (
        <Form.List name="variables">
          {(fields, { add, remove }) => (
            <div className="soha-build-template-variable-list">
              <div className="soha-build-template-variable-list__toolbar">
                <Text type="secondary">
                  服务可覆盖默认值。命令中的自由文本使用带引号的
                  "$SOHA_BUILD_变量名"；凭据使用密钥租约。
                </Text>
                <Button
                  icon={<PlusOutlined />}
                  onClick={() =>
                    add({
                      key: '',
                      label: '',
                      type: 'string',
                      required: false,
                      defaultValue: '',
                      description: '',
                    })
                  }
                >
                  添加变量
                </Button>
              </div>
              {fields.length === 0 ? (
                <ManagementState
                  bordered={false}
                  compact
                  kind="empty"
                  title="暂无变量"
                  description="可添加构建参数，或在 YAML / JSON 中编辑参数约束。"
                />
              ) : null}
              {fields.map((field, index) => (
                <div className="soha-build-template-variable-item" key={field.key}>
                  <div className="soha-build-template-variable-item__head">
                    <strong>{`变量 ${index + 1}`}</strong>
                    <Button
                      danger
                      icon={<DeleteOutlined />}
                      size="small"
                      onClick={() => remove(field.name)}
                    >
                      删除
                    </Button>
                  </div>
                  <div className="soha-build-template-form-grid">
                    <Form.Item
                      name={[field.name, 'key']}
                      label="变量 Key"
                      rules={[{ required: true, message: '请输入变量 Key' }]}
                    >
                      <Input placeholder="imageTag" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'label']} label="显示名称">
                      <Input placeholder="镜像 Tag" />
                    </Form.Item>
                    <Form.Item name={[field.name, 'type']} label="类型">
                      <Select
                        options={[
                          { value: 'string', label: 'string' },
                          { value: 'number', label: 'number' },
                          { value: 'integer', label: 'integer' },
                          { value: 'boolean', label: 'boolean' },
                        ]}
                      />
                    </Form.Item>
                    <Form.Item name={[field.name, 'defaultValue']} label="默认值">
                      <Input placeholder="latest" />
                    </Form.Item>
                    <Form.Item
                      className="soha-build-template-switch-field"
                      name={[field.name, 'required']}
                      label="必填"
                      valuePropName="checked"
                    >
                      <Switch />
                    </Form.Item>
                    <Form.Item
                      className="soha-build-template-form-grid__wide"
                      name={[field.name, 'description']}
                      label="说明"
                    >
                      <Input.TextArea rows={2} placeholder="变量用途、默认策略或允许值说明" />
                    </Form.Item>
                  </div>
                </div>
              ))}
            </div>
          )}
        </Form.List>
      ),
    },
    {
      key: 'advanced',
      label: 'YAML / JSON',
      children: (
        <div className="soha-build-template-advanced">
          <Form.Item name="variableSchemaText" hidden>
            <Input />
          </Form.Item>
          <Form.Item name="defaultVariablesText" hidden>
            <Input />
          </Form.Item>
          {previewState.error ? <Text type="danger">{previewState.error}</Text> : null}
          {previewState.json ? (
            <Suspense fallback={<ManagementState kind="loading" />}>
              <SourceEditor
                key={sourceGeneration}
                kind="BuildTemplate"
                value={JSON.parse(previewState.json)}
                targetId={selectedTemplate?.id}
                expectedRevision={loadedRevisionRef.current}
                disabled={!canSave || createMutation.isPending || updateMutation.isPending}
                onDirtyChange={setSourceDirty}
                onValidated={(document) => {
                  if (document.kind !== 'BuildTemplate') return
                  applyFormValues(
                    buildTemplateToFormValues({
                      ...document.spec,
                      key: document.metadata.name,
                      name: document.metadata.displayName || document.metadata.name,
                      description: document.metadata.description,
                      enabled: document.spec.enabled ?? true,
                    }),
                    true,
                  )
                }}
              />
            </Suspense>
          ) : null}
        </div>
      ),
    },
  ]

  const templateToolbar = (
    <header className="soha-build-template-toolbar" aria-label="当前构建模板操作">
      {hasSelection ? (
        isDirty || sourceDirty ? (
          <Tag color="gold">未保存</Tag>
        ) : (
          <Tag>已保存</Tag>
        )
      ) : null}
      <Space wrap>
        <ManagementIconButton
          icon={<CopyOutlined />}
          aria-label="复制模板"
          tooltip="复制模板"
          disabled={!selectedTemplate || !canCreate}
          onClick={() => {
            if (!selectedTemplate || !confirmDiscardChanges()) return
            const values = buildTemplateToFormValues(selectedTemplate)
            copiedFromRef.current = {
              id: selectedTemplate.id,
              revision: selectedTemplate.revision!,
            }
            setSelectedTemplateId('new')
            setActiveTabKey('basic')
            applyFormValues(
              {
                ...values,
                key: `${selectedTemplate.key}-copy`,
                name: `${selectedTemplate.name} 副本`,
              },
              true,
            )
            updateTemplateSearchParam()
          }}
        />
        <Button
          icon={<SaveOutlined />}
          type="primary"
          disabled={
            locked ||
            !hasSelection ||
            !canSave ||
            sourceDirty ||
            selectedTemplate?.publicationState === 'deprecated' ||
            publishMutation.isPending
          }
          loading={createMutation.isPending || updateMutation.isPending}
          onClick={() => void handleSave()}
        >
          保存草稿
        </Button>
        <Popconfirm
          title="发布当前草稿为新版本？"
          description="固定版本保持不变；已订阅的应用按采用策略更新后续构建。"
          disabled={locked}
          onConfirm={() => changePublication('publish')}
        >
          <ManagementIconButton
            aria-label="发布版本"
            tooltip="发布版本"
            icon={<UploadOutlined />}
            disabled={
              locked ||
              templatesQuery.isError ||
              source.isError ||
              !selectedTemplate ||
              !canUpdate ||
              isDirty ||
              sourceDirty ||
              selectedTemplate.publicationState !== 'draft' ||
              !loadedRevisionRef.current ||
              createMutation.isPending ||
              updateMutation.isPending
            }
            loading={publishMutation.isPending}
          />
        </Popconfirm>
        <TemplateVersionHistory kind="build" templateId={selectedTemplate?.id ?? ''} iconOnly />
        <TemplateUpdateEntry kind="build" templateId={selectedTemplate?.id ?? ''} iconOnly />
        <ManagementIconButton
          icon={<UndoOutlined />}
          aria-label="取消更改"
          tooltip="取消更改"
          disabled={!hasSelection || (!isDirty && !sourceDirty)}
          onClick={handleCancelChanges}
        />
        <Popconfirm
          title="废弃当前构建模板？已有服务与历史版本将保留。"
          disabled={locked}
          onConfirm={() => changePublication('deprecate')}
        >
          <ManagementIconButton
            aria-label="废弃"
            tooltip="废弃"
            danger
            icon={<DeleteOutlined />}
            disabled={
              locked ||
              templatesQuery.isError ||
              source.isError ||
              !selectedTemplate ||
              !canDelete ||
              selectedTemplate.publicationState === 'deprecated'
            }
            loading={deleteMutation.isPending}
          />
        </Popconfirm>
      </Space>
    </header>
  )

  const templateList = (
    <ManagementSearchableListPane
      activeKey={selectedTemplateId}
      className="soha-build-template-list"
      emptyDescription="新建模板后，可在右侧维护 Dockerfile、命令和变量。"
      emptyTitle="暂无构建模板"
      getItemKey={(item) => item.id}
      isError={templatesQuery.isError && !readableQueryData(templatesQuery)}
      isLoading={templatesQuery.isLoading}
      itemClassName="soha-build-template-list__item"
      items={visibleListItems}
      searchPlaceholder="搜索构建模板"
      searchValue={searchText}
      searchActions={
        <>
          <ManagementIconButton
            aria-label="新建模板"
            tooltip="新建模板"
            icon={<PlusOutlined />}
            color="primary"
            variant="solid"
            disabled={!canCreate}
            onClick={handleNewTemplate}
          />
          <ManagementIconButton
            aria-label="导入文件"
            tooltip="导入文件"
            icon={<UploadOutlined />}
            disabled={!canCreate && !canUpdate}
            onClick={() => {
              if (confirmDiscardChanges()) setImporting(true)
            }}
          />
          <TemplateSourcesButton iconOnly />

          <ManagementRefreshButton
            tooltip="刷新"
            aria-label="刷新"
            loading={templatesQuery.isFetching}
            onClick={() => {
              if (confirmDiscardChanges())
                void templatesQuery.refetch().then(({ data, error }) => {
                  if (error) return
                  const createdMatches = writeOutcomeUnknown(createMutation.error)
                    ? (data?.filter(
                        (item) =>
                          item.key === createMutation.variables?.key &&
                          creationBaseline.current &&
                          !creationBaseline.current.has(item.id),
                      ) ?? [])
                    : []
                  const fresh =
                    selectedTemplateId === 'new' && createdMatches.length === 1
                      ? createdMatches[0]
                      : data?.find((item) => item.id === selectedTemplateId)
                  if (fresh) {
                    loadTemplate(fresh)
                    updateMutation.reset()
                    if (createdMatches.length === 1 && fresh.id === createdMatches[0].id)
                      createMutation.reset()
                    if (
                      fresh.id === publishMutation.variables?.id &&
                      fresh.publicationState === 'published'
                    )
                      publishMutation.reset()
                    if (
                      fresh.id === deleteMutation.variables &&
                      fresh.publicationState === 'deprecated'
                    )
                      deleteMutation.reset()
                  }
                })
            }}
          />
        </>
      }
      onItemSelect={handleSelectListItem}
      onRetry={() => void templatesQuery.refetch()}
      onSearchChange={setSearchText}
      renderItemActions={(item) => (
        <span className="soha-build-template-list__item-actions">
          <Switch
            checked={item.enabled}
            disabled={
              (item.isDraft ? !canCreate : !canUpdate) ||
              (!item.isDraft && (item.id !== selectedTemplateId || !sourceWritable)) ||
              item.template?.publicationState === 'deprecated'
            }
            size="small"
            onChange={(checked) => handleTemplateEnabledChange(item, checked)}
          />
          <ManagementIconButton
            aria-label="编辑构建模板"
            icon={<EditOutlined />}
            size="small"
            tooltip="编辑"
            onClick={() => {
              handleSelectListItem(item)
              setActiveTabKey('basic')
            }}
          />
        </span>
      )}
      renderItem={(item) => (
        <>
          <span className="soha-build-template-list__item-head">
            <span className="soha-build-template-list__item-main">
              <strong>{item.name}</strong>
              <Text type="secondary">{item.key}</Text>
            </span>
          </span>
          <span className="soha-build-template-list__item-meta">
            <Tag>{item.builderKind || 'docker'}</Tag>
            <Tag>{`命令 ${item.commandCount}`}</Tag>
            <Tag>{`变量 ${item.variableCount}`}</Tag>
            <BooleanTag value={item.enabled} />
            <TemplatePublicationStatus template={item.template} />
          </span>
          <Text type="secondary" className="text-xs">
            {item.updatedAt ? formatDateTime(item.updatedAt) : '尚未保存'}
          </Text>
        </>
      )}
    />
  )

  const templateDesigner = hasSelection ? (
    <Form
      className="soha-build-template-form"
      disabled={!canSave}
      form={form}
      layout="vertical"
      onValuesChange={(_changedValues, allValues) => {
        setFormSnapshot((current) => ({ ...current, ...allValues }))
        setIsDirty(true)
      }}
    >
      <TemplateUsageImpactPanel
        loading={selectedTemplateUsageQuery.isFetching && !!selectedTemplate}
        onNavigate={navigate}
        usage={selectedTemplateUsage}
      />
      <Tabs
        activeKey={activeTabKey}
        className="soha-build-template-tabs"
        destroyOnHidden={false}
        items={designerTabs}
        onChange={(next) => {
          if (!sourceDirty || next === 'advanced') setActiveTabKey(next)
          else message.info('请先校验并同步源码，或还原源码。')
        }}
      />
    </Form>
  ) : (
    <ManagementState
      bordered={false}
      kind="select-scope"
      title="选择或新建构建模板"
      description="左侧选择模板后，在右侧维护 Dockerfile、构建命令和变量。"
    />
  )

  return (
    <>
      <TemplateDesignerShell
        className="soha-page soha-build-template-page"
        designer={
          <>
            {templateToolbar}
            <ReadFeedback query={templatesQuery} label="构建模板" />
            <ReadFeedback query={source} label="模板来源" />
            <ReadFeedback query={selectedTemplateUsageQuery} label="引用与使用" />
            <MutationFeedback
              operations={[
                {
                  mutation: createMutation,
                  label: '新建构建模板',
                  target: createMutation.variables?.name,
                },
                {
                  mutation: updateMutation,
                  label: '保存构建模板',
                  target: updateMutation.variables?.id,
                },
                {
                  mutation: publishMutation,
                  label: '发布模板版本',
                  target: publishMutation.variables?.id,
                  receipt: { message: '模板版本已发布，已有服务保留固定版本。' },
                },
                {
                  mutation: deleteMutation,
                  label: '废弃构建模板',
                  target: deleteMutation.variables,
                  receipt: { message: '模板已废弃，历史版本保留。' },
                },
              ]}
            />
            {templateDesigner}
          </>
        }
        designerClassName="soha-build-template-designer"
        list={templateList}
        workspaceClassName="soha-build-template-workspace"
      />
      {importing ? (
        <Suspense fallback={<ManagementState kind="loading" />}>
          <ImportDialog
            onClose={() => setImporting(false)}
            onImported={() => {
              setSourceDirty(false)
              setIsDirty(false)
              void templatesQuery.refetch().then(({ data, error }) => {
                if (error) return
                const current = data?.find((item) => item.id === selectedTemplateId)
                if (current) loadTemplate(current)
              })
            }}
          />
        </Suspense>
      ) : null}
    </>
  )
}
