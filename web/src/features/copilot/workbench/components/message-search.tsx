import { useEffect, useRef, useState } from 'react'
import { useInfiniteQuery } from '@tanstack/react-query'
import { Button, Empty, Input, Modal, Spin, Typography, type InputRef } from 'antd'
import { CommentOutlined, SearchOutlined } from '@ant-design/icons'
import type { WorkbenchMessageSearchHit } from '@opensoha/contracts/gen/ts/sohaapi'
import type { WorkbenchSession } from '../types'
import { workbenchQueries } from '../queries'
import { displayWorkbenchSessionTitle } from '../model'

export function SearchHighlight({ text, query }: { text: string; query: string }) {
  const terms: string[] = query.toLocaleLowerCase().match(/[a-z0-9]+|[^\p{ASCII}\s\p{P}]+/gu) ?? []
  if (!terms.length) return <>{text}</>
  const escaped = terms.map((term) => term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
  const regex = new RegExp(`(${escaped.join('|')})`, 'giu')
  return (
    <>
      {text
        .split(regex)
        .map((part, index) =>
          terms.includes(part.toLocaleLowerCase()) ? <mark key={index}>{part}</mark> : part,
        )}
    </>
  )
}

export function readRecentVisits(owner?: string, personal = false): string[] {
  if (!owner) return []
  try {
    const value: unknown = JSON.parse(
      localStorage.getItem(`${personal ? 'heya' : 'soha-chat'}-visits:${owner}`) ?? '[]',
    )
    return Array.isArray(value)
      ? value.filter((id): id is string => typeof id === 'string').slice(0, 20)
      : []
  } catch {
    return []
  }
}
export function rememberChatVisit(owner: string, session: string, personal = false) {
  try {
    localStorage.setItem(
      `${personal ? 'heya' : 'soha-chat'}-visits:${owner}`,
      JSON.stringify(
        [session, ...readRecentVisits(owner, personal).filter((id) => id !== session)].slice(0, 20),
      ),
    )
  } catch {
    /* Browser storage can be disabled. */
  }
}

export function MessageSearch({
  owner,
  personal = false,
  sessions,
  onSelect,
  onClose,
}: {
  owner?: string
  personal?: boolean
  sessions: WorkbenchSession[]
  onSelect: (session: string, message?: string, query?: string) => void
  onClose: () => void
}) {
  const [input, setInput] = useState('')
  const [query, setQuery] = useState('')
  const [composing, setComposing] = useState(false)
  const [active, setActive] = useState(0)
  const inputRef = useRef<InputRef>(null)
  useEffect(() => {
    if (composing) return
    const timer = setTimeout(() => setQuery(input.trim()), 300)
    return () => clearTimeout(timer)
  }, [input, composing])
  const search = useInfiniteQuery(workbenchQueries.messageSearch(owner, query, true, personal))
  const hits = search.data?.pages.flatMap((page) => page.data?.items ?? []) ?? []
  const visited = readRecentVisits(owner, personal)
  const recent = visited
    .map((id) => sessions.find((session) => session.id === id))
    .filter((session): session is WorkbenchSession => !!session)
  const waitingForQuery = input.trim() !== query
  const rows = waitingForQuery ? [] : query ? hits : recent
  const select = (index: number) => {
    const row = rows[index]
    if (!row) return
    if (query) {
      const hit = row as WorkbenchMessageSearchHit
      onSelect(hit.sessionId, hit.messageId, query)
    } else onSelect((row as WorkbenchSession).id)
  }
  return (
    <Modal
      open
      title="搜索对话"
      footer={null}
      onCancel={onClose}
      width={560}
      destroyOnHidden
      className="soha-message-search"
      afterOpenChange={(open) => open && inputRef.current?.focus()}
    >
      <Input
        ref={inputRef}
        autoFocus
        aria-label="搜索消息"
        placeholder="搜索消息内容"
        prefix={<SearchOutlined />}
        maxLength={120}
        allowClear
        value={input}
        onChange={(event) => {
          setInput(event.target.value)
          setActive(0)
        }}
        onCompositionStart={() => setComposing(true)}
        onCompositionEnd={() => setComposing(false)}
        onKeyDown={(event) => {
          if (event.nativeEvent.isComposing) return
          if (event.key === 'ArrowDown') {
            event.preventDefault()
            setActive((value) => Math.max(0, Math.min(rows.length - 1, value + 1)))
          } else if (event.key === 'ArrowUp') {
            event.preventDefault()
            setActive((value) => Math.max(0, value - 1))
          } else if (event.key === 'Enter') {
            event.preventDefault()
            select(active)
          }
        }}
      />
      <div className="soha-message-search__results" aria-live="polite">
        {!query && !waitingForQuery && <Typography.Text type="secondary">最近访问</Typography.Text>}
        {(waitingForQuery || search.isFetching) && !search.isFetchingNextPage ? (
          <Spin size="small" />
        ) : null}
        {search.isError ? (
          <div>
            <Typography.Text type="danger">搜索失败</Typography.Text>
            <Button onClick={() => void search.refetch()}>重试</Button>
          </div>
        ) : null}
        {rows.map((row, index) => {
          const hit = query ? (row as WorkbenchMessageSearchHit) : undefined
          const session = !query ? (row as WorkbenchSession) : undefined
          return (
            <button
              type="button"
              key={hit?.messageId ?? session?.id}
              className={`soha-message-search__result${active === index ? ' is-active' : ''}`}
              onFocus={() => setActive(index)}
              onClick={() => select(index)}
            >
              <CommentOutlined />
              <span>
                <strong>
                  {displayWorkbenchSessionTitle(hit?.sessionTitle ?? session?.title ?? '')}
                </strong>
                {hit ? (
                  <span className="soha-message-search__snippet">
                    <SearchHighlight text={hit.snippet} query={query} />
                  </span>
                ) : null}
                <small>
                  {hit ? (hit.role === 'user' ? '你' : '助手') : '最近访问'} ·{' '}
                  {new Date(hit?.createdAt ?? session?.updatedAt ?? '').toLocaleString()}
                </small>
              </span>
            </button>
          )
        })}
        {!rows.length && !waitingForQuery && !search.isFetching && !search.isError ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description={
              query
                ? search.hasNextPage
                  ? '继续搜索更早消息'
                  : '没有找到相关消息'
                : '还没有访问记录'
            }
          />
        ) : null}
        {search.hasNextPage ? (
          <Button
            block
            loading={search.isFetchingNextPage}
            onClick={() => void search.fetchNextPage()}
          >
            搜索更早消息
          </Button>
        ) : null}
      </div>
    </Modal>
  )
}
