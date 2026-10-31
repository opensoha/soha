import { useEffect, useRef, useState } from 'react'
import { useAuthStore } from '@/stores/auth-store'
import { Alert, Button, Form, Input, Popconfirm, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  ManagedContentRecord,
  ManagedContentAuthorization,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { readableQueryData } from '@/utils/query-feedback'
import { destinationQuery, encodeContent, upload, paste, authorize } from './api'
import { managedContentKeys } from './keys'
import { useTransferContext } from './context'
import { TransferState, recordReceipt, uncertainRecord } from './feedback'

type Draft = {
  kind: 'upload' | 'paste'
  destinationId: string
  fileName: string
  contentBase64?: string
  text?: string
}
export function TransferForm({
  retryRecord,
  clearRetry,
}: {
  retryRecord?: ManagedContentRecord
  clearRetry: () => void
}) {
  const { viewer, scope, access, text } = useTransferContext()
  const client = useQueryClient()
  const query = useQuery(destinationQuery(viewer, scope, access.view))
  const targets = readableQueryData(query)
  const [kind, setKind] = useState<'upload' | 'paste'>('upload')
  const [destination, setDestination] = useState('')
  const [file, setFile] = useState<File>()
  const [name, setName] = useState('paste.txt')
  const [content, setContent] = useState('')
  const [last, setLast] = useState<ManagedContentRecord>()
  const [requestId, setRequestId] = useState('')
  const [selectionError, setSelectionError] = useState('')
  const live = useRef(true)
  const frozen = useRef<Draft | undefined>(undefined)
  const fileInput = useRef<HTMLInputElement>(null)
  useEffect(() => {
    live.current = true
    return () => {
      frozen.current = undefined
      live.current = false
    }
  }, [])
  useEffect(() => {
    if (!retryRecord) return
    frozen.current = undefined
    setLast(undefined)
    setFile(undefined)
    setContent('')
    setDestination(retryRecord.destinationId)
    setKind(retryRecord.action === 'paste' ? 'paste' : 'upload')
    setName(retryRecord.nameRedacted ? '' : retryRecord.fileName)
  }, [retryRecord])
  const choose = (next?: File) => {
    setSelectionError('')
    setFile(undefined)
    if (!next) return
    if (next.size > 262144) {
      setSelectionError(
        text(
          '文件超过256 KiB，本入口不会提交该内容。',
          'The file exceeds 256 KiB and will not be submitted.',
        ),
      )
      return
    }
    setFile(next)
    setKind('upload')
  }
  const snapshot = async (): Promise<Draft> => {
    if (!destination)
      throw new Error(text('请选择已发布的目的连接。', 'Select a published destination.'))
    if (kind === 'paste') {
      if (new TextEncoder().encode(content).length > 262144)
        throw new Error(text('文本超过256 KiB。', 'Text exceeds 256 KiB.'))
      return { kind, destinationId: destination, fileName: name, text: content }
    }
    if (!file) throw new Error(text('请选择原文件。', 'Select the original file.'))
    const bytes = new Uint8Array(await file.arrayBuffer())
    try {
      return {
        kind,
        destinationId: destination,
        fileName: file.name,
        contentBase64: encodeContent(bytes),
      }
    } finally {
      bytes.fill(0)
    }
  }
  const send = useOperationMutation<ManagedContentRecord, { record?: ManagedContentRecord }>(
    {
      retry: false,
      mutationFn: async ({ record }) => {
        const draft = record && frozen.current ? frozen.current : await snapshot()
        if (!live.current || useAuthStore.getState().user?.userId !== viewer)
          throw new Error(
            text(
              '登录上下文已变化，未提交内容。',
              'The login context changed; content was not submitted.',
            ),
          )
        frozen.current = draft
        let authorization: ManagedContentAuthorization | undefined
        if (record) {
          const issued = await authorize(
            viewer,
            record.id,
            record.appealStatus === 'approved' ? 'use_approval' : 'acknowledge',
          )
          authorization = { attemptId: record.id, token: issued.token }
        }
        const id = crypto.randomUUID()
        setRequestId(id)
        const common = {
          requestId: id,
          destinationId: draft.destinationId,
          fileName: draft.fileName,
          ...(authorization ? { authorization } : {}),
        }
        const result =
          draft.kind === 'upload'
            ? await upload(viewer, { ...common, contentBase64: draft.contentBase64 ?? '' })
            : await paste(viewer, { ...common, text: draft.text ?? '' })
        return result.record
      },
      onSuccess: (record) => {
        setLast(record)
        if (record.state === 'completed') {
          frozen.current = undefined
          setFile(undefined)
          setContent('')
          clearRetry()
        }
        void client.invalidateQueries({ queryKey: managedContentKeys.all })
      },
    },
    {
      label: ['提交受管内容', 'Submit managed content'],
      target: () => destination,
      resultError: uncertainRecord,
      receipt: (r) => recordReceipt(r, text),
    },
  )
  const held = last?.state === 'blocked' && Boolean(frozen.current)
  const locked = send.blocked || held
  const active = last ?? retryRecord
  const canRetry =
    active?.state === 'blocked' &&
    (active.decision?.requiredAction === 'acknowledge' || active.appealStatus === 'approved')
  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <ReadFeedback
        canRead={access.view}
        query={query}
        label={text('已发布目的连接', 'Published destinations')}
      />
      {targets?.length === 0 ? (
        <Alert
          type="info"
          title={text(
            '尚无可用目的连接，请联系管理员配置并发布。',
            'No approved destinations are available. Ask an administrator to configure and publish one.',
          )}
        />
      ) : null}
      {retryRecord ? (
        <Alert
          type="info"
          title={text(
            '使用既有记录重试原内容',
            'Retry the original content with an existing record',
          )}
          description={`${retryRecord.id} · ${retryRecord.fileName}${retryRecord.nameRedacted ? text('（名称已脱敏，请输入原文件名）', ' (name redacted; enter the original filename)') : ''} · ${text('请重新选择原文件或输入完全相同的文本及文件名；内容、目标、策略或连接变化都会重新校验。', 'Reselect the original file or enter exactly the same text and filename. Content, destination, policy and connection changes are revalidated.')}`}
        />
      ) : null}
      <Form layout="vertical" disabled={!access.create || locked} onFinish={() => send.mutate({})}>
        <Form.Item
          label={text('目的连接', 'Destination')}
          htmlFor="managed-transfer-destination"
          required
        >
          <Select
            id="managed-transfer-destination"
            value={destination || undefined}
            onChange={setDestination}
            options={(targets ?? []).map((d) => ({
              value: d.id,
              label: `${d.name} · ${d.kind} · v${d.publishedVersion} · ${d.mode}`,
            }))}
          />
        </Form.Item>
        <Form.Item label={text('内容来源', 'Content source')} htmlFor="managed-transfer-kind">
          <Select
            id="managed-transfer-kind"
            value={kind}
            onChange={(value) => {
              setKind(value)
              setSelectionError('')
            }}
            options={[
              { value: 'upload', label: text('选择或拖放文件', 'Select or drop a file') },
              { value: 'paste', label: text('粘贴纯文本', 'Paste plain text') },
            ]}
          />
        </Form.Item>
        {kind === 'upload' ? (
          <Form.Item label={text('文件（最多256 KiB）', 'File (up to 256 KiB)')}>
            <input
              ref={fileInput}
              type="file"
              aria-label={text('选择上传文件', 'Select a file for upload')}
              disabled={!access.create || locked}
              onChange={(event) => choose(event.target.files?.[0])}
            />
            <div
              onDragOver={(event) => event.preventDefault()}
              onDrop={(event) => {
                event.preventDefault()
                if (access.create && !locked) choose(event.dataTransfer.files[0])
              }}
            >
              <Typography.Paragraph type="secondary">
                {file
                  ? file.name
                  : text(
                      '也可将单个文件拖到这里；文件名和内容均由服务端检查。',
                      'You can also drop one file here. The server inspects both the filename and content.',
                    )}
              </Typography.Paragraph>
            </div>
          </Form.Item>
        ) : (
          <>
            <Form.Item label={text('文件名', 'Filename')} htmlFor="managed-paste-name" required>
              <Input
                id="managed-paste-name"
                maxLength={200}
                value={name}
                onChange={(event) => setName(event.target.value)}
              />
            </Form.Item>
            <Form.Item label={text('粘贴文本', 'Pasted text')} htmlFor="managed-paste-text">
              <Input.TextArea
                id="managed-paste-text"
                value={content}
                rows={8}
                onChange={(event) => {
                  setSelectionError('')
                  setContent(event.target.value)
                }}
                onPaste={(event) => {
                  if (event.clipboardData.files.length) {
                    event.preventDefault()
                    choose(event.clipboardData.files[0])
                  }
                }}
              />
            </Form.Item>
          </>
        )}
        <Button
          type="primary"
          htmlType="submit"
          disabled={
            !access.create || locked || query.isError || query.isFetching || Boolean(selectionError)
          }
          loading={send.isPending}
        >
          {text('检查并提交', 'Inspect and submit')}
        </Button>
      </Form>
      {selectionError ? <Alert type="error" title={selectionError} /> : null}
      {send.feedback}
      {requestId ? (
        <Typography.Text copyable>
          {text('本次请求ID', 'Request ID')}: {requestId}
        </Typography.Text>
      ) : null}
      {active?.decision?.requiredAction === 'appeal' ? (
        <Typography.Text>
          {text(
            '可在“我的记录”提交申诉，批准后从记录重新提供原内容。',
            'Submit an appeal in My records, then resume the original content after approval.',
          )}
        </Typography.Text>
      ) : null}
      {active ? (
        <>
          <TransferState record={active} />
          <Typography.Text copyable>{active.id}</Typography.Text>
          {canRetry ? (
            <Popconfirm
              title={text(
                '确认仅重试这条记录的原内容？',
                'Retry only the original content of this record?',
              )}
              description={text(
                '确认仅限一次；不会改变策略。',
                'Authorization is single-use and does not change policy.',
              )}
              onConfirm={() => send.mutate({ record: active })}
            >
              <Button disabled={!access.create || send.blocked} loading={send.isPending}>
                {active.appealStatus === 'approved'
                  ? text('使用已批准申诉重试', 'Retry with approved appeal')
                  : text('确认警告并重试原内容', 'Acknowledge and retry original content')}
              </Button>
            </Popconfirm>
          ) : null}
        </>
      ) : null}
      {held || retryRecord ? (
        <Button
          disabled={send.blocked}
          onClick={() => {
            frozen.current = undefined
            setLast(undefined)
            clearRetry()
            send.reset()
            if (fileInput.current) fileInput.current.value = ''
          }}
        >
          {text('更改内容或目的地（重新检查）', 'Change content or destination (inspect again)')}
        </Button>
      ) : null}
      <Typography.Paragraph type="secondary">
        {text(
          '仅完整UTF-8文本可完成内容检查。图片、Office/PDF、压缩/加密或其他未知格式不会显示为clean；审计模式可能放行并保留未检查状态。WebDAV凭据仍需当前用户的secret.use权限及目的连接绑定。',
          'Only complete UTF-8 text can be fully inspected. Images, Office/PDF, archives, encryption and unknown formats never become clean; audit may allow them with an uninspected status. WebDAV credentials still require the current user’s secret.use permission and destination connection binding.',
        )}
      </Typography.Paragraph>
    </Space>
  )
}
