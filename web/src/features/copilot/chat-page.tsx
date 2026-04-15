import { useState, useRef, useEffect } from 'react'
import { Input, Button, Empty, Spin, Typography, Avatar } from '@douyinfe/semi-ui'
import { IconSend, IconPlus, IconComment } from '@douyinfe/semi-icons'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { hasPermission, usePermissionSnapshot } from '@/features/auth/permission-snapshot'
import { PageHeader } from '@/components/page-header'
import { useI18n } from '@/i18n'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'

const { Title, Text, Paragraph } = Typography

interface ChatSession {
  id: string
  title: string
  updatedAt: string
}

interface ChatMessage {
  id: string
  role: 'user' | 'assistant'
  content: string
  createdAt: string
}

export function ChatPage() {
  const { t, localeCode } = useI18n()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [activeSession, setActiveSession] = useState<string | null>(null)
  const [inputValue, setInputValue] = useState('')
  const messagesEndRef = useRef<HTMLDivElement>(null)
  const canUseChat = hasPermission(permissionSnapshotQuery.data?.data, 'observe.ai.chat')

  const sessionsQuery = useQuery({
    queryKey: ['copilot-sessions'],
    queryFn: () => api.get<ApiResponse<ChatSession[]>>('/copilot/sessions'),
  })

  const messagesQuery = useQuery({
    queryKey: ['copilot-messages', activeSession],
    queryFn: () => api.get<ApiResponse<ChatMessage[]>>(`/copilot/sessions/${activeSession}/messages`),
    enabled: !!activeSession,
  })

  const createSessionMutation = useMutation({
    mutationFn: () => api.post<ApiResponse<ChatSession>>('/copilot/sessions', { title: localeCode === 'zh_CN' ? '新对话' : 'New Session' }),
    onSuccess: (data) => {
      queryClient.invalidateQueries({ queryKey: ['copilot-sessions'] })
      setActiveSession(data.data.id)
    },
  })

  const sendMessageMutation = useMutation({
    mutationFn: (content: string) =>
      api.post<ApiResponse<ChatMessage>>(`/copilot/sessions/${activeSession}/messages`, { content }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['copilot-messages', activeSession] })
      setInputValue('')
    },
  })

  const sessions = sessionsQuery.data?.data ?? []
  const messages = messagesQuery.data?.data ?? []

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [messages])

  const handleSend = () => {
    const trimmed = inputValue.trim()
    if (!trimmed || !activeSession) return
    sendMessageMutation.mutate(trimmed)
  }

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      handleSend()
    }
  }

  return (
    <div className="kc-page" style={{ minHeight: '100%' }}>
      <PageHeader
        title={t('page.ai.chat.title', 'AI Chat')}
        description={t('page.ai.chat.desc', 'Run context-aware analysis conversations to investigate issues, validate hypotheses, and capture conclusions.')}
        actions={
          canUseChat ? (
            <Button
              icon={<IconPlus />}
              theme="solid"
              onClick={() => createSessionMutation.mutate()}
              loading={createSessionMutation.isPending}
            >
              {localeCode === 'zh_CN' ? '新对话' : 'New Session'}
            </Button>
          ) : null
        }
      />

      <div
        style={{
          display: 'grid',
          gridTemplateColumns: '280px minmax(0, 1fr)',
          gap: 0,
          flex: 1,
          minHeight: 'calc(100vh - 230px)',
          border: '1px solid var(--semi-color-border)',
          borderRadius: 12,
          overflow: 'hidden',
          background: 'var(--semi-color-bg-1)',
        }}
      >
        <div style={{ borderRight: '1px solid var(--semi-color-border)', minWidth: 0 }}>
          <div className="p-3" style={{ borderBottom: '1px solid var(--semi-color-border)' }}>
            <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '会话列表' : 'Sessions'}</Text>
          </div>
          <div className="flex-1 overflow-auto">
            {sessionsQuery.isLoading ? (
              <div className="flex justify-center py-8"><Spin /></div>
            ) : sessions.length === 0 ? (
              <div className="p-4 text-center">
                <Text type="tertiary" size="small">{localeCode === 'zh_CN' ? '暂无对话' : 'No sessions'}</Text>
              </div>
            ) : (
              sessions.map((s) => (
                <div
                  key={s.id}
                  className={`px-3 py-3 cursor-pointer border-b hover:bg-gray-100 ${activeSession === s.id ? 'bg-blue-50' : ''}`}
                  style={{ borderColor: 'var(--semi-color-border)' }}
                  onClick={() => setActiveSession(s.id)}
                >
                  <Text ellipsis={{ showTooltip: true }} style={{ width: '100%' }}>{s.title}</Text>
                  <Text type="tertiary" size="small">{s.updatedAt}</Text>
                </div>
              ))
            )}
          </div>
        </div>

        <div className="flex flex-col" style={{ minWidth: 0 }}>
          {!activeSession ? (
            <div className="flex-1 flex items-center justify-center">
              <div className="text-center">
                <IconComment size="extra-large" style={{ fontSize: 48, color: 'var(--semi-color-text-2)' }} />
                <Title heading={5} type="tertiary" style={{ marginTop: 16 }}>{t('page.ai.chat.title', 'AI Chat')}</Title>
                <Paragraph type="tertiary">{localeCode === 'zh_CN' ? '选择一个对话或创建新对话开始聊天' : 'Select a session or create a new one to start chatting'}</Paragraph>
              </div>
            </div>
          ) : (
            <>
              <div className="flex-1 overflow-auto p-4 space-y-4">
                {messagesQuery.isLoading ? (
                  <div className="flex justify-center py-8"><Spin /></div>
                ) : messages.length === 0 ? (
                  <Empty description={localeCode === 'zh_CN' ? '发送消息开始对话' : 'Send a message to start the conversation'} />
                ) : (
                  messages.map((msg) => (
                    <div key={msg.id} className={`flex gap-3 ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                      {msg.role === 'assistant' && (
                        <Avatar size="small" style={{ backgroundColor: 'var(--semi-color-primary)' }}>AI</Avatar>
                      )}
                      <div
                        className={`max-w-[70%] rounded-lg px-4 py-2 ${
                          msg.role === 'user'
                            ? 'bg-blue-500 text-white'
                            : 'bg-gray-100'
                        }`}
                      >
                        <Paragraph style={{ margin: 0, color: msg.role === 'user' ? 'white' : undefined, whiteSpace: 'pre-wrap' }}>
                          {msg.content}
                        </Paragraph>
                      </div>
                      {msg.role === 'user' && (
                        <Avatar size="small" style={{ backgroundColor: 'var(--semi-color-success)' }}>U</Avatar>
                      )}
                    </div>
                  ))
                )}
                <div ref={messagesEndRef} />
              </div>

              <div className="border-t p-4" style={{ borderColor: 'var(--semi-color-border)' }}>
                <div className="flex gap-2">
                  <Input
                    placeholder={localeCode === 'zh_CN' ? '输入消息... (Enter 发送)' : 'Type a message... (Enter to send)'}
                    value={inputValue}
                    onChange={setInputValue}
                    onKeyDown={handleKeyDown}
                    disabled={!canUseChat || sendMessageMutation.isPending}
                    size="large"
                  />
                  <Button
                    icon={<IconSend />}
                    theme="solid"
                    size="large"
                    onClick={handleSend}
                    loading={sendMessageMutation.isPending}
                    disabled={!canUseChat || !inputValue.trim()}
                  />
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  )
}
