import { useState } from 'react'
import { Alert, App, Button, Card, Input, InputNumber, Modal, Space, Typography } from 'antd'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { deliveryApi } from '../api'
import { deliveryKeys } from '../keys'
import type {
  DeliveryDocumentFile,
  DeliveryDocumentImport,
  DeliveryDocumentPreview,
} from '../types'
import { documentKindLabels } from './model'
import { DocumentDiagnostics } from './source-editor'
import { useUnsavedDocument } from './use-unsaved-document'

export async function readDeliveryDocumentFiles(files: File[]): Promise<DeliveryDocumentFile[]> {
  if (!files.length || files.length > 100) throw new Error('请选择 1–100 个 YAML 或 JSON 文件。')
  if (
    files.some((file) => file.size > 1024 * 1024) ||
    files.reduce((sum, file) => sum + file.size, 0) > 2 * 1024 * 1024
  )
    throw new Error('单文件不能超过 1 MiB，全部文件不能超过 2 MiB。')
  const decoder = new TextDecoder('utf-8', { fatal: true })
  return Promise.all(
    files.map(async (file) => ({
      path: file.name,
      content: decoder.decode(await file.arrayBuffer()),
    })),
  )
}

export function DeliveryDocumentImportDialog({
  onClose,
  onImported,
}: {
  onClose: () => void
  onImported?: (result: DeliveryDocumentImport) => void
}) {
  const { message } = App.useApp()
  const client = useQueryClient()
  const [files, setFiles] = useState<DeliveryDocumentFile[]>([])
  useUnsavedDocument(files.length > 0)
  const [preview, setPreview] = useState<DeliveryDocumentPreview>()
  const [error, setError] = useState('')
  const [reading, setReading] = useState(false)
  const [key, setKey] = useState('')
  const previewMutation = useMutation({ mutationFn: deliveryApi.documents.preview })
  const applyMutation = useMutation({
    mutationFn: () => {
      if (!preview?.valid || !preview.id || !preview.candidateDigest)
        throw new Error('请先校验文档。')
      return deliveryApi.documents.apply(preview.id, {
        candidateDigest: preview.candidateDigest,
        idempotencyKey: key,
      })
    },
  })
  const busy = reading || previewMutation.isPending || applyMutation.isPending
  const changeFiles = (next: DeliveryDocumentFile[]) => {
    setFiles(next)
    setPreview(undefined)
    setKey('')
    setError('')
  }
  const patchFile = (index: number, patch: Partial<DeliveryDocumentFile>) =>
    changeFiles(files.map((file, offset) => (offset === index ? { ...file, ...patch } : file)))
  const validate = async () => {
    setPreview(undefined)
    setError('')
    try {
      const result = await previewMutation.mutateAsync({ files })
      setPreview(result)
      setKey(crypto.randomUUID())
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : '预览失败')
    }
  }
  const apply = async () => {
    setError('')
    try {
      const result = await applyMutation.mutateAsync()
      await client.invalidateQueries({ queryKey: deliveryKeys.all })
      message.success(`已导入 ${result.objects.length} 个对象`)
      onImported?.(result)
      onClose()
    } catch (failure) {
      setError(failure instanceof Error ? failure.message : '导入失败')
    }
  }
  return (
    <Modal
      open
      title="导入 Soha Delivery 文档"
      width={940}
      onCancel={() => {
        if (!busy && (!files.length || window.confirm('关闭并放弃当前导入内容？'))) onClose()
      }}
      footer={
        <Space>
          <Button
            onClick={() => void validate()}
            loading={previewMutation.isPending}
            disabled={busy || !files.length}
          >
            校验并预览
          </Button>
          <Button
            type="primary"
            onClick={() => void apply()}
            loading={applyMutation.isPending}
            disabled={busy || !preview?.valid}
          >
            确认导入
          </Button>
        </Space>
      }
    >
      <Space orientation="vertical" style={{ width: '100%' }}>
        <Typography.Paragraph type="secondary">
          支持构建模板、部署模板、流程模板和工作流。全部文件校验通过后一起导入；模板保存为草稿，工作流仅保存配置。
        </Typography.Paragraph>
        <Space wrap>
          <label>
            选择文件{' '}
            <input
              aria-label="选择交付文档文件"
              type="file"
              accept=".yaml,.yml,.json"
              multiple
              disabled={busy}
              onChange={(event) => {
                const selected = Array.from(event.target.files ?? [])
                event.target.value = ''
                if (!selected.length) return
                setReading(true)
                void readDeliveryDocumentFiles(selected)
                  .then(changeFiles)
                  .catch((failure: unknown) =>
                    setError(failure instanceof Error ? failure.message : '文件必须是有效 UTF-8'),
                  )
                  .finally(() => setReading(false))
              }}
            />
          </label>
          <Button
            disabled={busy || files.length >= 100}
            onClick={() =>
              changeFiles([...files, { path: `document-${files.length + 1}.yaml`, content: '' }])
            }
          >
            粘贴文档
          </Button>
        </Space>
        {error ? <Alert type="error" showIcon title={error} /> : null}
        {files.map((file, index) => (
          <Card
            key={index}
            size="small"
            title={file.path || `文件 ${index + 1}`}
            extra={
              <Button
                disabled={busy}
                onClick={() => changeFiles(files.filter((_, offset) => offset !== index))}
              >
                移除
              </Button>
            }
          >
            <Space orientation="vertical" style={{ width: '100%' }}>
              <Space wrap>
                <Input
                  aria-label={`文件 ${index + 1} 路径`}
                  value={file.path}
                  disabled={busy}
                  onChange={(event) => patchFile(index, { path: event.target.value })}
                />
                <Input
                  aria-label={`文件 ${index + 1} 更新对象 ID`}
                  placeholder="更新对象 ID（留空则新建）"
                  value={file.targetId}
                  disabled={busy}
                  onChange={(event) =>
                    patchFile(index, {
                      targetId: event.target.value || undefined,
                      expectedRevision: undefined,
                    })
                  }
                />
                {file.targetId ? (
                  <InputNumber
                    aria-label={`文件 ${index + 1} 当前版本`}
                    placeholder="当前 revision / version"
                    min={1}
                    precision={0}
                    value={file.expectedRevision}
                    disabled={busy}
                    onChange={(value) => patchFile(index, { expectedRevision: value ?? undefined })}
                  />
                ) : null}
              </Space>
              <Input.TextArea
                aria-label={`文件 ${index + 1} 内容`}
                value={file.content}
                rows={files.length === 1 ? 16 : 5}
                disabled={busy}
                spellCheck={false}
                style={{ fontFamily: 'monospace' }}
                onChange={(event) => patchFile(index, { content: event.target.value })}
              />
            </Space>
          </Card>
        ))}
        {preview ? <DeliveryDocumentPreviewView preview={preview} /> : null}
      </Space>
    </Modal>
  )
}

export function DeliveryDocumentPreviewView({
  preview,
  showImportHint = true,
}: {
  preview: DeliveryDocumentPreview
  showImportHint?: boolean
}) {
  return (
    <>
      <DocumentDiagnostics items={preview.diagnostics} />
      {preview.valid ? (
        <Alert
          type="success"
          showIcon
          title="全部文档校验通过"
          description={
            showImportHint ? '请检查下方差异后确认导入。修改内容或目标后需重新校验。' : undefined
          }
        />
      ) : null}
      {preview.candidates.map((candidate) => (
        <Card
          key={candidate.path}
          size="small"
          title={`${candidate.path} · ${documentKindLabels[candidate.document.kind]}`}
        >
          <Typography.Text strong>
            {candidate.document.metadata.name} ·{' '}
            {{ create: '新增', update: '更新草稿 / 配置', unchanged: '无变化' }[candidate.action]}
          </Typography.Text>
          <ul>
            {candidate.changedPaths.map((path) => (
              <li key={path}>
                <code>{path || '/'}</code>
              </li>
            ))}
          </ul>
          <details>
            <summary>查看规范定义</summary>
            <pre className="soha-json-block">{JSON.stringify(candidate.document, null, 2)}</pre>
          </details>
        </Card>
      ))}
    </>
  )
}
