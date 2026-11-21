import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { api } from '@/services/api-client'
import { queryAccessDenied, readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import { CommentOutlined, ReloadOutlined } from '@ant-design/icons'
import type {
  AssistantMeshDiscoveredAssistant,
  AssistantMeshInquiry,
  AssistantMeshInquiryEnvelope,
  AssistantMeshInquiryInput,
  AssistantMeshInquiryListEnvelope,
  AssistantMeshQuestionPreview,
  AssistantMeshQuestionPreviewEnvelope,
  AssistantMeshReceptionProfileEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Button,
  Drawer,
  Empty,
  Flex,
  Input,
  List,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'

import { AssistantDelegations } from './assistant-delegations'

const { Paragraph, Text, Title } = Typography

const MESH_BASE = '/copilot/assistant-mesh'

type ReceptionTab = 'find' | 'sent' | 'inbox' | 'profile' | 'delegations'

const STATUS_LABEL: Record<AssistantMeshInquiry['status'], string> = {
  answered: '已自动答复',
  processing: '正在查找可见资料',
  pending: '已转交本人',
  replied: '同事已回复',
  expired: '已过期',
  invalidated: '已失效',
}

function receptionBaseKey(owner: string) {
  return ['assistant-mesh', owner, 'reception'] as const
}

function errorText(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试'
}

function observedAt(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN')
}

export function useReceptionProfile(owner: string) {
  const queryClient = useQueryClient()
  const queryKey = useMemo(() => [...receptionBaseKey(owner), 'profile'], [owner])
  const query = useQuery({
    queryKey,
    queryFn: async () =>
      (await api.getEnvelope<AssistantMeshReceptionProfileEnvelope>(`${MESH_BASE}/profile`)).data,
    enabled: Boolean(owner),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: true,
  })
  const denied = queryAccessDenied(query)
  useEffect(() => {
    if (denied) queryClient.setQueryData(queryKey, undefined)
  }, [denied, queryClient, queryKey])
  return denied ? { ...query, data: undefined } : query
}

export function AssistantReception({
  owner,
  mention,
}: {
  owner: string
  mention?: { request: number; query: string; question: string }
}) {
  const queryClient = useQueryClient()
  const profile = useReceptionProfile(owner)
  const [open, setOpen] = useState(false)
  const lastMention = useRef(0)
  const [tab, setTab] = useState<ReceptionTab>('find')
  const [search, setSearch] = useState('')
  const [searchTerm, setSearchTerm] = useState('')
  const [selected, setSelected] = useState<AssistantMeshDiscoveredAssistant>()
  const [seedQuestion, setSeedQuestion] = useState('')
  const [question, setQuestion] = useState('')
  const [preview, setPreview] = useState<AssistantMeshQuestionPreview>()
  const [previewFor, setPreviewFor] = useState('')
  const [sentReceiptId, setSentReceiptId] = useState('')
  const [profileOpen, setProfileOpen] = useState(false)
  const [profileAway, setProfileAway] = useState(false)
  const [profileIntro, setProfileIntro] = useState('')
  const [profileSaved, setProfileSaved] = useState(false)
  const [replyTarget, setReplyTarget] = useState('')
  const [replyText, setReplyText] = useState('')

  const baseKey = useMemo(() => receptionBaseKey(owner), [owner])
  const searchKey = useMemo(() => [...baseKey, 'search', searchTerm], [baseKey, searchTerm])
  const sentKey = useMemo(() => [...baseKey, 'sent'], [baseKey])
  const inboxKey = useMemo(() => [...baseKey, 'inbox'], [baseKey])
  const available = profile.isSuccess && Boolean(profile.data)

  const searchQuery = useQuery({
    queryKey: searchKey,
    queryFn: async () => {
      const body = searchTerm.trim() ? { query: searchTerm.trim() } : {}
      // api.post normalizes an `items` envelope to `data`.
      const response = await api.post<{ data?: AssistantMeshDiscoveredAssistant[] }>(
        `${MESH_BASE}/assistants/search`,
        body,
      )
      return response.data ?? []
    },
    enabled: Boolean(owner) && open && available && tab === 'find',
    retry: false,
    staleTime: 30_000,
  })
  const sentQuery = useQuery({
    queryKey: sentKey,
    queryFn: async () =>
      (
        await api.getEnvelope<AssistantMeshInquiryListEnvelope>(
          `${MESH_BASE}/inquiries?direction=sent`,
        )
      ).items,
    enabled: Boolean(owner) && open && available && (tab === 'sent' || Boolean(sentReceiptId)),
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: 5_000,
  })
  const inboxQuery = useQuery({
    queryKey: inboxKey,
    queryFn: async () =>
      (
        await api.getEnvelope<AssistantMeshInquiryListEnvelope>(
          `${MESH_BASE}/inquiries?direction=inbox`,
        )
      ).items,
    enabled: Boolean(owner) && open && available && tab === 'inbox',
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: true,
    refetchInterval: tab === 'inbox' ? 10_000 : false,
  })

  const searched = readableQueryData(searchQuery) ?? []
  const sent = (readableQueryData(sentQuery) ?? []).filter((item) => item.callerId === owner)
  const inbox = (readableQueryData(inboxQuery) ?? []).filter((item) => item.ownerId === owner)
  const sentResult = sentQuery.isError ? undefined : sent.find((item) => item.id === sentReceiptId)

  useEffect(() => {
    if (!profile.data) return
    setProfileOpen(profile.data.open)
    setProfileAway(profile.data.away ?? false)
    setProfileIntro(profile.data.intro ?? '')
  }, [profile.data?.open, profile.data?.away, profile.data?.intro])

  useEffect(() => {
    if (!mention || !available || mention.request <= lastMention.current) return
    lastMention.current = mention.request
    setOpen(true)
    setTab('find')
    setSearch(mention.query)
    setSearchTerm(mention.query)
    setSelected(undefined)
    setSeedQuestion(mention.question)
    setQuestion('')
    setPreview(undefined)
    setPreviewFor('')
    setSentReceiptId('')
  }, [available, mention?.request])

  useEffect(() => {
    if (!available) setOpen(false)
  }, [available])

  useEffect(() => {
    const redact = (key: readonly unknown[], query: { error: Error | null }) => {
      if (queryAccessDenied(query)) queryClient.setQueryData(key, undefined)
    }
    redact(searchKey, searchQuery)
    redact(sentKey, sentQuery)
    redact(inboxKey, inboxQuery)
  }, [inboxKey, inboxQuery, queryClient, searchKey, searchQuery, sentKey, sentQuery])

  const invalidateInquiries = () => {
    void queryClient.invalidateQueries({ queryKey: sentKey })
    void queryClient.invalidateQueries({ queryKey: inboxKey })
  }

  const previewMutation = useMutation({
    mutationFn: async (value: string) =>
      (
        await api.post<AssistantMeshQuestionPreviewEnvelope>(`${MESH_BASE}/inquiries/preview`, {
          question: value,
        })
      ).data,
    onSuccess: (item, value) => {
      setPreview(item)
      setPreviewFor(value)
    },
  })

  // Submission is idempotent. A lost response can be retried with the same frozen tuple.
  const send = useMutation({
    mutationFn: async (input: AssistantMeshInquiryInput) =>
      (await api.post<AssistantMeshInquiryEnvelope>(`${MESH_BASE}/inquiries`, input)).data,
    onSuccess: (item) => {
      setSentReceiptId(item.id)
      setPreview(undefined)
      setPreviewFor('')
      setQuestion('')
      invalidateInquiries()
    },
  })

  const reply = useOperationMutation(
    {
      mutationFn: async (input: { id: string; answer: string }) =>
        (
          await api.post<AssistantMeshInquiryEnvelope>(
            `${MESH_BASE}/inquiries/${encodeURIComponent(input.id)}/reply`,
            { answer: input.answer },
          )
        ).data,
      onSuccess: () => {
        setReplyTarget('')
        setReplyText('')
        invalidateInquiries()
      },
    },
    {
      label: ['仅回复该同事', 'Reply privately'],
      target: () => inbox.find((item) => item.id === replyTarget)?.callerName ?? '提问同事',
    },
  )

  const saveProfile = useOperationMutation(
    {
      mutationFn: async () =>
        (
          await api.put<AssistantMeshReceptionProfileEnvelope>(`${MESH_BASE}/profile`, {
            open: profileOpen,
            away: profileAway,
            intro: profileIntro,
          })
        ).data,
      onSuccess: (item) => {
        queryClient.setQueryData([...baseKey, 'profile'], item)
        setProfileSaved(true)
      },
    },
    {
      label: ['保存助手名片', 'Save assistant card'],
      target: () => owner,
    },
  )

  const close = () => {
    setOpen(false)
    setTab('find')
    setSearch('')
    setSearchTerm('')
    setSelected(undefined)
    setSeedQuestion('')
    setQuestion('')
    setPreview(undefined)
    setPreviewFor('')
    setSentReceiptId('')
    setReplyTarget('')
    setReplyText('')
  }

  const choose = (assistant: AssistantMeshDiscoveredAssistant) => {
    setSelected(assistant)
    setQuestion(seedQuestion)
    setSeedQuestion('')
    setPreview(undefined)
    setPreviewFor('')
    setSentReceiptId('')
  }

  const editQuestion = (value: string) => {
    setQuestion(value)
    setPreview(undefined)
    setPreviewFor('')
    setSentReceiptId('')
  }

  const runPreview = () => {
    const value = question.trim()
    if (!selected || !value || previewMutation.isPending) return
    previewMutation.mutate(value)
  }

  const confirmSend = () => {
    if (!selected || !preview || preview.blocked || previewFor !== question.trim()) return
    if (send.isPending || send.isError) return
    send.mutate({
      ownerId: selected.ownerId,
      requestId: crypto.randomUUID(),
      question: preview.question,
    })
  }

  if (!available) return null

  const findPane = (
    <Space orientation="vertical" size={14} style={{ width: '100%' }}>
      <Input.Search
        aria-label="按姓名或公开主题查找助手"
        value={search}
        allowClear
        placeholder="按姓名或公开主题查找助手"
        enterButton="查找"
        loading={searchQuery.isFetching}
        onChange={(event) => setSearch(event.target.value)}
        onSearch={(value) => setSearchTerm(value)}
      />
      <ReadFeedback query={searchQuery} label="可问询助手" />
      {searchQuery.isPending ? (
        <Spin />
      ) : searchQuery.isError && !queryAccessDenied(searchQuery) ? (
        <ContextFeedback
          type="error"
          title="助手列表读取失败"
          description={errorText(searchQuery.error)}
        />
      ) : queryAccessDenied(searchQuery) ? (
        <ContextFeedback type="warning" title="没有查找同事助手的权限" />
      ) : searched.length === 0 ? (
        <Empty description="没有匹配的同事助手" />
      ) : (
        <List
          dataSource={searched}
          renderItem={(assistant) => (
            <List.Item>
              <Button
                type="text"
                block
                aria-pressed={selected?.ownerId === assistant.ownerId}
                style={{ height: 'auto', padding: 12, textAlign: 'left', whiteSpace: 'normal' }}
                onClick={() => choose(assistant)}
              >
                <Flex vertical gap={6} style={{ width: '100%' }}>
                  <Flex align="center" gap={8} wrap="wrap">
                    <Text strong>{assistant.name}</Text>
                    {assistant.away ? <Tag color="gold">可能延迟回复</Tag> : null}
                  </Flex>
                  {assistant.intro ? <Text type="secondary">{assistant.intro}</Text> : null}
                  <Space size={[4, 4]} wrap>
                    {assistant.topics.map((topic) => (
                      <Tag key={topic}>{topic}</Tag>
                    ))}
                  </Space>
                </Flex>
              </Button>
            </List.Item>
          )}
        />
      )}

      {selected ? (
        <Space orientation="vertical" size={10} style={{ width: '100%' }}>
          <Title level={5} style={{ margin: 0 }}>
            向 {selected.name} 提问
          </Title>
          <Text type="secondary">
            问询内容可能被审计，关闭后保留 {profile.data?.retentionDays ?? 0} 天。
          </Text>
          <Input.TextArea
            aria-label="问题内容"
            value={question}
            autoSize={{ minRows: 4, maxRows: 8 }}
            maxLength={2000}
            placeholder="写下要问询的问题"
            onChange={(event) => editQuestion(event.target.value)}
          />
          <Flex gap={8} wrap="wrap">
            <Button
              onClick={runPreview}
              loading={previewMutation.isPending}
              disabled={!question.trim() || send.isPending}
            >
              预览发送内容
            </Button>
            <Button
              type="primary"
              onClick={confirmSend}
              disabled={
                !preview ||
                preview.blocked ||
                previewFor !== question.trim() ||
                send.isPending ||
                send.isError
              }
            >
              确认发送
            </Button>
          </Flex>
          {previewMutation.isError ? (
            <ContextFeedback
              type="error"
              title="预览失败"
              description={errorText(previewMutation.error)}
            />
          ) : null}
          {preview ? (
            <ContextFeedback
              type={preview.blocked ? 'error' : 'info'}
              title={preview.blocked ? '内容包含敏感信息，暂不能发送' : '即将发送的问题'}
              description={
                preview.blocked ? '请修改问题并重新预览。' : '请核对以下纯文本，确认后才会发送。'
              }
            />
          ) : null}
          {preview && !preview.blocked ? (
            <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{preview.question}</Paragraph>
          ) : null}
        </Space>
      ) : null}
      {sentResult ? <InquiryResult inquiry={sentResult} /> : null}
    </Space>
  )

  const inquiryList = (
    query: typeof sentQuery,
    items: AssistantMeshInquiry[],
    direction: 'sent' | 'inbox',
  ) => (
    <Space orientation="vertical" size={12} style={{ width: '100%' }}>
      <Flex justify="flex-end">
        <Button
          icon={<ReloadOutlined />}
          loading={query.isFetching}
          onClick={() => void query.refetch()}
        >
          刷新
        </Button>
      </Flex>
      <ReadFeedback query={query} label={direction === 'sent' ? '已发出问询' : '待办收件箱'} />
      {queryAccessDenied(query) ? (
        <ContextFeedback type="warning" title="没有读取问询的权限" />
      ) : query.isPending ? (
        <Spin />
      ) : query.isError ? (
        <ContextFeedback type="error" title="问询读取失败" description={errorText(query.error)} />
      ) : items.length === 0 ? (
        <Empty description={direction === 'sent' ? '还没有发出问询' : '暂无待办问询'} />
      ) : (
        <List
          dataSource={items}
          renderItem={(inquiry) => (
            <List.Item style={{ display: 'block' }}>
              <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                <Flex gap={8} align="center" wrap="wrap">
                  <Tag color={inquiry.status === 'pending' ? 'gold' : 'blue'}>
                    {STATUS_LABEL[inquiry.status]}
                  </Tag>
                  <Text strong>
                    {direction === 'sent' ? inquiry.ownerName : inquiry.callerName}
                  </Text>
                  <Text type="secondary">{observedAt(inquiry.createdAt)}</Text>
                </Flex>
                <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>
                  {inquiry.question}
                </Paragraph>
                {inquiry.status === 'pending' && direction === 'inbox' ? (
                  replyTarget === inquiry.id ? (
                    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                      <Input.TextArea
                        aria-label="仅回复该同事的内容"
                        value={replyText}
                        autoSize={{ minRows: 3, maxRows: 8 }}
                        maxLength={4000}
                        placeholder="写下只回复给这位同事的内容"
                        onChange={(event) => setReplyText(event.target.value)}
                      />
                      <Flex gap={8} wrap="wrap">
                        <Button
                          type="primary"
                          loading={reply.isPending}
                          disabled={!replyText.trim() || reply.blocked}
                          onClick={() => reply.mutate({ id: inquiry.id, answer: replyText.trim() })}
                        >
                          仅回复该同事
                        </Button>
                        <Button
                          onClick={() => {
                            setReplyTarget('')
                            setReplyText('')
                          }}
                        >
                          取消
                        </Button>
                      </Flex>
                    </Space>
                  ) : (
                    <Button
                      onClick={() => {
                        setReplyTarget(inquiry.id)
                        setReplyText('')
                      }}
                    >
                      回复该同事
                    </Button>
                  )
                ) : (
                  <InquiryResult inquiry={inquiry} />
                )}
                {replyTarget === inquiry.id ? reply.feedback : null}
              </Space>
            </List.Item>
          )}
        />
      )}
    </Space>
  )

  const profilePane = (
    <Space orientation="vertical" size={14} style={{ width: '100%' }}>
      <ReadFeedback query={profile} label="助手名片" />
      <ContextFeedback
        type="info"
        title="同事只会在你开放助手名片后看到可问询入口，并可看到当前是否暂离。"
      />
      <Flex justify="space-between" align="center" gap={12} wrap="wrap">
        <Text strong>开放同事问询</Text>
        <Switch
          aria-label="开放同事问询"
          checked={profileOpen}
          onChange={(value) => {
            setProfileOpen(value)
            setProfileSaved(false)
          }}
        />
      </Flex>
      <Flex justify="space-between" align="center" gap={12} wrap="wrap">
        <Text strong>暂离，可能延迟回复</Text>
        <Switch
          aria-label="暂离"
          checked={profileAway}
          onChange={(value) => {
            setProfileAway(value)
            setProfileSaved(false)
          }}
        />
      </Flex>
      <Input.TextArea
        aria-label="助手简介"
        value={profileIntro}
        autoSize={{ minRows: 3, maxRows: 6 }}
        maxLength={280}
        placeholder="介绍你可以被同事问询的范围"
        onChange={(event) => {
          setProfileIntro(event.target.value)
          setProfileSaved(false)
        }}
      />
      <Text type="secondary">问询关闭后保留 {profile.data?.retentionDays ?? 0} 天。</Text>
      <Flex gap={8} align="center" wrap="wrap">
        <Button
          type="primary"
          loading={saveProfile.isPending}
          disabled={saveProfile.blocked}
          onClick={() => saveProfile.mutate()}
        >
          保存名片设置
        </Button>
        {profileSaved ? <Text type="success">已保存</Text> : null}
      </Flex>
      {saveProfile.feedback}
    </Space>
  )

  return (
    <>
      <Tooltip title="助手问询">
        <Button
          aria-label="助手问询"
          className="soha-ai-rail-action soha-icon-trigger"
          icon={<CommentOutlined />}
          onClick={() => setOpen(true)}
        />
      </Tooltip>
      <Drawer destroyOnHidden size={760} open={open} onClose={close} title="助手问询">
        {send.isError && send.variables ? (
          <ContextFeedback
            type="error"
            title="问询提交失败"
            description={errorText(send.error)}
            action={
              <Space>
                <Button onClick={() => send.mutate(send.variables!)}>重试原问询</Button>
                {!writeOutcomeUnknown(send.error) ? (
                  <Button onClick={() => send.reset()}>关闭提示</Button>
                ) : null}
              </Space>
            }
          />
        ) : null}
        <Tabs
          activeKey={tab}
          onChange={(key) => setTab(key as ReceptionTab)}
          items={[
            { key: 'find', label: '查找', children: findPane },
            { key: 'sent', label: '已发出', children: inquiryList(sentQuery, sent, 'sent') },
            { key: 'inbox', label: '收件箱', children: inquiryList(inboxQuery, inbox, 'inbox') },
            { key: 'profile', label: '我的名片', children: profilePane },
            { key: 'delegations', label: '本机委托', children: open && available && tab === 'delegations' ? <AssistantDelegations owner={owner} /> : null },
          ]}
        />
      </Drawer>
    </>
  )
}

function InquiryResult({ inquiry }: { inquiry: AssistantMeshInquiry }) {
  if (['processing', 'pending', 'expired', 'invalidated'].includes(inquiry.status)) {
    return <Text type="secondary">{STATUS_LABEL[inquiry.status]}</Text>
  }
  return (
    <Space orientation="vertical" size={6} style={{ width: '100%' }}>
      <Text strong>{inquiry.status === 'answered' ? '自动答复' : '同事回复'}</Text>
      <Paragraph style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{inquiry.answer}</Paragraph>
      {inquiry.citations?.length ? (
        <Space orientation="vertical" size={2}>
          <Text type="secondary">引用来源</Text>
          {inquiry.citations.map((citation) => (
            <Text key={`${citation.assetId}:${citation.version}`} type="secondary">
              {citation.title} · v{citation.version} · {observedAt(citation.observedAt)}
            </Text>
          ))}
        </Space>
      ) : null}
    </Space>
  )
}
