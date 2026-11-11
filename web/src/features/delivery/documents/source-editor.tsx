import { useState } from 'react'
import { Alert, Button, Input, Segmented, Space, Typography } from 'antd'
import { useMutation, useQuery } from '@tanstack/react-query'
import { stringify } from 'yaml'
import { useI18n } from '@/i18n'
import { downloadText } from '@/utils/download'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import type { DeliveryDocument, DeliveryDocumentDiagnostic, DeliveryDocumentKind } from '../types'
import { useUnsavedDocument } from './use-unsaved-document'

export function DocumentDiagnostics({ items }: { items: DeliveryDocumentDiagnostic[] }) {
  const english = useI18n().localeCode === 'en_US'
  const reasons: Record<string, string> = {
    delivery_service_build_source_missing: '服务尚未绑定构建源，请先在应用中配置服务的构建源。',
    delivery_target_environment_mismatch: '所选环境不属于该应用，请重新选择应用环境。',
    delivery_target_binding_mismatch: '部署目标与所选服务或环境不一致，请重新选择部署目标。',
    delivery_target_bundle_mismatch: '所选版本包不属于该应用或服务，请重新选择版本包。',
  }
  return items.length ? (
    <Alert
      type="error"
      showIcon
      title={english ? 'Document validation failed' : '文档校验失败'}
      description={
        <ul>
          {items.map((item, index) => (
            <li key={index}>
              {item.path}
              {item.line ? `:${item.line}:${item.column ?? 1}` : ''} · {item.pointer || '/'}：
              {!english && reasons[item.code] ? reasons[item.code] : item.message}
            </li>
          ))}
        </ul>
      }
    />
  ) : null
}

export function DeliveryDocumentSourceEditor({
  value,
  kind,
  targetId,
  expectedRevision,
  disabled,
  onValidated,
  onDirtyChange,
}: {
  value: unknown
  kind: DeliveryDocumentKind
  targetId?: string
  expectedRevision?: number
  disabled?: boolean
  onValidated: (document: DeliveryDocument) => void
  onDirtyChange: (dirty: boolean) => void
}) {
  const [format, setFormat] = useState<'yaml' | 'json'>('yaml')
  const [draft, setDraft] = useState<string>()
  useUnsavedDocument(draft !== undefined)
  const [diagnostics, setDiagnostics] = useState<DeliveryDocumentDiagnostic[]>([])
  const [error, setError] = useState('')
  const [synced, setSynced] = useState(false)
  const preview = useMutation({ mutationFn: deliveryApi.documents.preview })
  const content = draft ?? (format === 'yaml' ? stringify(value) : JSON.stringify(value, null, 2))
  const reset = () => {
    setDraft(undefined)
    setDiagnostics([])
    setError('')
    setSynced(false)
    onDirtyChange(false)
  }
  const validate = async () => {
    setError('')
    setDiagnostics([])
    try {
      const result = await preview.mutateAsync({
        validateOnly: true,
        files: [{ path: `document.${format}`, content, targetId, expectedRevision }],
      })
      setDiagnostics(result.diagnostics)
      const candidate = result.candidates[0]
      if (!result.valid || !candidate) return
      if (candidate.document.kind !== kind) {
        setError('文档类型与当前编辑对象不一致。')
        return
      }
      onValidated(candidate.document)
      setDraft(undefined)
      onDirtyChange(false)
      setSynced(true)
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : '文档校验失败')
    }
  }
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Space wrap>
        <Segmented
          value={format}
          options={['yaml', 'json']}
          disabled={draft !== undefined || preview.isPending}
          onChange={(next) => setFormat(next as 'yaml' | 'json')}
        />
        <Button onClick={() => void validate()} loading={preview.isPending} disabled={disabled}>
          校验并同步表单
        </Button>
        <Button onClick={reset} disabled={draft === undefined || preview.isPending}>
          还原源码
        </Button>
        <Typography.Text type="secondary">
          {draft !== undefined
            ? '源码有待校验的修改'
            : synced
              ? '已同步到表单，尚未保存'
              : 'YAML / JSON 表示同一份定义'}
        </Typography.Text>
      </Space>
      {error ? <Alert type="error" showIcon title={error} /> : null}
      <DocumentDiagnostics items={diagnostics} />
      <Input.TextArea
        aria-label="Soha Delivery 文档源码"
        value={content}
        rows={22}
        spellCheck={false}
        disabled={disabled || preview.isPending}
        style={{ fontFamily: 'monospace' }}
        onChange={(event) => {
          setDraft(event.target.value)
          setDiagnostics([])
          setError('')
          setSynced(false)
          onDirtyChange(true)
        }}
      />
    </Space>
  )
}

export function DeliveryDocumentReadView({ value }: { value: unknown }) {
  const [format, setFormat] = useState<'yaml' | 'json'>('yaml')
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Segmented
        value={format}
        options={['yaml', 'json']}
        onChange={(next) => setFormat(next as 'yaml' | 'json')}
      />
      <pre className="soha-json-block">
        {format === 'yaml' ? stringify(value) : JSON.stringify(value, null, 2)}
      </pre>
    </Space>
  )
}

export function DeliveryDocumentExportView({
  kind,
  id,
  version,
  legacy,
  initialFormat = 'yaml',
}: {
  kind: DeliveryDocumentKind
  id: string
  version?: number
  legacy?: unknown
  initialFormat?: 'yaml' | 'json'
}) {
  const [format, setFormat] = useState(initialFormat)
  const query = useQuery({
    queryKey: deliveryKeys.documents.export(kind, id, version, format),
    queryFn: () => deliveryApi.documents.export(kind, id, format, version),
  })
  return (
    <Space orientation="vertical" style={{ width: '100%' }}>
      <Space wrap>
        <Segmented
          value={format}
          options={['yaml', 'json']}
          onChange={(next) => setFormat(next as 'yaml' | 'json')}
        />
        <Button
          disabled={!query.data}
          onClick={() => {
            if (query.data)
              downloadText(
                `${query.data.document.metadata.name}.soha.${format}`,
                query.data.content,
              )
          }}
        >
          导出文件
        </Button>
      </Space>
      {query.isError ? (
        <Alert
          type="warning"
          showIcon
          title="暂时无法导出文档"
          description={query.error.message}
          action={<Button onClick={() => void query.refetch()}>重试</Button>}
        />
      ) : null}
      {query.isPending ? <Typography.Text>正在读取定义…</Typography.Text> : null}
      {query.data ? <pre className="soha-json-block">{query.data.content}</pre> : null}
      {query.isError && legacy !== undefined ? (
        <>
          <Typography.Text type="secondary">
            以下为只读的原模板定义，保留旧格式中的全部字段。
          </Typography.Text>
          <Button
            onClick={() => downloadText(`${id}.legacy.json`, JSON.stringify(legacy, null, 2))}
          >
            导出旧格式
          </Button>
          <pre className="soha-json-block">{JSON.stringify(legacy, null, 2)}</pre>
        </>
      ) : null}
    </Space>
  )
}
