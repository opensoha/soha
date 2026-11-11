import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Alert, Button, Input, Space, Typography } from 'antd'
import { workbenchQueries } from '../queries'
import { buildSessionReference, validSessionReferenceId } from '../session-reference'
import type { WorkbenchContextReference } from '@opensoha/contracts/gen/ts/sohaapi'

export function SessionReferenceReader({
  currentSessionId,
  initialId = '',
  onInsert,
}: {
  currentSessionId: string
  initialId?: string
  onInsert: (reference: WorkbenchContextReference) => void
}) {
  const [value, setValue] = useState(initialId)
  const [readId, setReadId] = useState<string | undefined>(
    validSessionReferenceId(initialId) && initialId !== currentSessionId ? initialId : undefined,
  )
  const [inputError, setInputError] = useState(
    initialId && !validSessionReferenceId(initialId)
      ? '请输入有效的会话 ID。'
      : initialId === currentSessionId
        ? '请选择其他会话，当前会话无需引用。'
        : '',
  )
  const session = useQuery({
    ...workbenchQueries.sessions.detail(readId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  // ponytail: the existing API returns at most 100 messages; use cursor reads when pagination is available.
  const messages = useQuery({
    ...workbenchQueries.sessions.messages(readId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  const loading = Boolean(readId) && (session.isFetching || messages.isFetching)
  const failed = session.isError || messages.isError
  const reference = useMemo(() => {
    if (!readId || loading || failed || !session.data?.data || !messages.data?.data)
      return undefined
    if (session.data.data.id !== readId) return undefined
    return buildSessionReference(session.data.data, messages.data.data, new Date().toISOString())
  }, [readId, loading, failed, session.data, messages.data])

  const read = () => {
    const id = value.trim()
    if (!validSessionReferenceId(id)) {
      setInputError('请输入有效的会话 ID。')
      return
    }
    if (id === currentSessionId) {
      setInputError('请选择其他会话，当前会话无需引用。')
      return
    }
    setInputError('')
    if (id === readId) {
      void session.refetch()
      void messages.refetch()
    } else setReadId(id)
  }

  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      <Typography.Paragraph type="secondary">
        粘贴另一个会话的 ID，读取并预览后插入当前草稿。只能读取当前账号有权访问的会话。
      </Typography.Paragraph>
      <label htmlFor="referenced-session-id">来源会话 ID</label>
      <Input
        id="referenced-session-id"
        value={value}
        placeholder="粘贴会话 ID"
        onChange={(event) => {
          setValue(event.target.value)
          setReadId(undefined)
          setInputError('')
        }}
        onPressEnter={read}
        status={inputError ? 'error' : undefined}
      />
      {inputError ? <Alert type="error" title={inputError} /> : null}
      <Button onClick={read} loading={loading}>
        读取会话
      </Button>
      {failed && readId ? (
        <Alert
          type="error"
          showIcon
          title="无法读取此会话"
          description="会话可能不存在、已归档、无访问权限，或服务暂时不可用。可以重试。"
        />
      ) : null}
      {reference ? (
        <>
          <Typography.Text strong>
            {reference.snapshot.title || reference.snapshot.sessionId}
          </Typography.Text>
          <Typography.Text type="secondary">
            读取 {reference.snapshot.returnedMessageCount} 条，引用{' '}
            {reference.snapshot.quotedMessageCount} 条。
          </Typography.Text>
          <Alert
            type="info"
            showIcon
            title="引用为当前读取的快照，不会自动同步"
            description={`本次最多读取 100 条消息，引用最多 20 条、12,000 字符。${reference.snapshot.truncated ? '当前内容已截取。' : ''}${reference.snapshot.returnedMessageCount >= 100 ? '该会话可能还有未读取的消息。' : ''}`}
          />
          <div className="soha-ai-workbench__reference-preview">
            {reference.snapshot.messages.map((item) => (
              <div key={item.id}>
                <Typography.Text strong>
                  {item.role === 'user' ? '用户' : '助手'} · {item.createdAt}
                </Typography.Text>
                <Typography.Paragraph>{item.content}</Typography.Paragraph>
              </div>
            ))}
          </div>
          <Button
            type="primary"
            disabled={!reference.snapshot.quotedMessageCount}
            onClick={() =>
              onInsert({
                kind: 'session',
                sessionId: reference.snapshot.sessionId,
                name: reference.snapshot.title,
                messageIds: reference.snapshot.messages.map((item) => item.id),
              })
            }
          >
            插入会话引用
          </Button>
          {!reference.snapshot.quotedMessageCount ? (
            <Typography.Text type="secondary">此会话暂无可引用的用户或助手消息。</Typography.Text>
          ) : null}
        </>
      ) : null}
    </Space>
  )
}
