import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import {
  appHostingQueries,
  hostedAppKindLabel,
  resolveSafeHostedAppUrl,
  type HostedAppKind,
  type PortalHostedAppAuthorSummary,
  type PortalHostedAppItem,
} from '@/features/app-hosting'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  ArrowLeftOutlined,
  ExportOutlined,
  ReloadOutlined,
  SearchOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Avatar, Button, Card, Empty, Input, Segmented, Skeleton, Space, Typography } from 'antd'
import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import '../provider-portal-pages.css'
import { PortalAccountMenu } from '../shared/account-menu'
import { formatPortalDateTime } from '../shared/formatters'
import { PortalViewNav } from '../shared/portal-view-nav'

const { Paragraph, Text, Title } = Typography

type ColleagueKindFilter = 'all' | HostedAppKind

function AuthorSummaryCard({
  author,
  onSelect,
}: {
  author: PortalHostedAppAuthorSummary
  onSelect: (userId: string) => void
}) {
  return (
    <button
      type="button"
      className="soha-portal-author-card"
      aria-label={`查看 ${author.displayName} 的作品（${author.visibleAppCount} 个）`}
      onClick={() => onSelect(author.userId)}
    >
      <Avatar size={40} src={author.avatarUrl || undefined} icon={<UserOutlined />} />
      <div className="soha-portal-author-card-body">
        <div className="soha-portal-author-card-head">
          <Text strong ellipsis title={author.displayName}>
            {author.displayName}
          </Text>
          <MetadataTag tone="blue" label={`${author.visibleAppCount} 个作品`} />
        </div>
        <div className="soha-portal-author-card-teams">
          {author.teamNames.length > 0 ? (
            author.teamNames.slice(0, 3).map((team) => (
              <MetadataTag key={team} label={team} />
            ))
          ) : (
            <Text type="secondary" style={{ fontSize: 12 }}>
              未标注部门
            </Text>
          )}
        </div>
      </div>
    </button>
  )
}

function ColleagueWorkCard({
  item,
  localeCode,
  onOpenAuthor,
}: {
  item: PortalHostedAppItem
  localeCode: 'zh_CN' | 'en_US'
  onOpenAuthor: (userId: string) => void
}) {
  const safeUrl = resolveSafeHostedAppUrl(item.url)
  return (
    <article className="soha-portal-work-card">
      <div className="soha-portal-work-card-top">
        <div className="soha-portal-work-card-title-row">
          <Text className="soha-portal-work-card-title" strong ellipsis={{ tooltip: item.name }}>
            {item.name}
          </Text>
          <MetadataTag
            tone={item.kind === 'dynamic' ? 'purple' : 'blue'}
            label={hostedAppKindLabel(item.kind, true)}
          />
        </div>
        <Paragraph
          className="soha-portal-work-card-desc"
          ellipsis={{ rows: 2, tooltip: item.description || undefined }}
          type="secondary"
        >
          {item.description || '作者暂未填写简介'}
        </Paragraph>
      </div>

      <div className="soha-portal-work-card-footer">
        <div className="soha-portal-work-card-author">
          <Avatar size={24} src={item.author.avatarUrl || undefined} icon={<UserOutlined />} />
          <div className="soha-portal-work-card-author-meta">
            <button
              type="button"
              className="soha-portal-work-author-trigger"
              onClick={() => onOpenAuthor(item.author.userId)}
            >
              {item.author.displayName}
            </button>
            {item.author.teamNames.length > 0 ? (
              <Space size={[4, 4]} wrap>
                {item.author.teamNames.slice(0, 2).map((team) => (
                  <MetadataTag key={team} label={team} />
                ))}
              </Space>
            ) : null}
          </div>
        </div>

        <div className="soha-portal-work-card-actions">
          <Text type="secondary" style={{ fontSize: 12 }}>
            更新于 {formatPortalDateTime(item.updatedAt, localeCode)}
          </Text>
          {safeUrl ? (
            <Button
              type="primary"
              size="small"
              icon={<ExportOutlined />}
              href={safeUrl}
              target="_blank"
              rel="noopener noreferrer"
              aria-label={`打开应用 ${item.name}`}
            >
              打开应用
            </Button>
          ) : (
            <Button size="small" disabled icon={<ExportOutlined />}>
              链接不可用
            </Button>
          )}
        </div>
      </div>
    </article>
  )
}

export function ColleagueWorksView({ authorId = '' }: { authorId?: string }) {
  const navigate = useNavigate()
  const { localeCode } = useI18n()
  const permissionSnapshot = usePermissionSnapshot().data?.data
  const canOpenHeya = hasPermission(permissionSnapshot, 'observe.ai.chat')
  const canManageMyApps =
    hasPermission(permissionSnapshot, 'apphosting.apps.view') ||
    hasPermission(permissionSnapshot, 'apphosting.apps.publish')

  const [keyword, setKeyword] = useState('')
  const [kindFilter, setKindFilter] = useState<ColleagueKindFilter>('all')

  const normalizedAuthorId = authorId.trim()
  const catalogQuery = useQuery(
    appHostingQueries.portalApps({
      query: keyword.trim() || undefined,
      kind: kindFilter === 'all' ? undefined : kindFilter,
      authorUserId: normalizedAuthorId || undefined,
      limit: 60,
    }),
  )

  const catalog = readableQueryData(catalogQuery)
  const denied = queryAccessDenied(catalogQuery)
  const items = catalog?.items ?? []
  const authors = catalog?.authors ?? []
  const selectedAuthor = catalog?.selectedAuthor
  const hasActiveFilter = Boolean(keyword.trim() || kindFilter !== 'all')

  const openAuthorPage = (targetAuthorId: string) => {
    const trimmed = targetAuthorId.trim()
    if (!trimmed) return
    navigate(`/portal/colleague-works/authors/${encodeURIComponent(trimmed)}`)
  }

  return (
    <div className="soha-provider-portal soha-provider-portal-home soha-portal-colleague-works">
      <main className="soha-portal-main">
        <PortalViewNav
          active="colleague-works"
          myAppsCount={catalog?.myAppsCount}
          extra={<PortalAccountMenu />}
        />
        <ReadFeedback query={catalogQuery} label="同事作品" />

        {!denied ? (
          <section className="soha-portal-hosted-surface">
            <header className="soha-portal-hosted-header">
              <div>
                <Title level={4} style={{ margin: 0 }}>
                  {normalizedAuthorId
                    ? `${selectedAuthor?.displayName || items[0]?.author.displayName || '同事'}公开的作品`
                    : '同事作品'}
                </Title>
                <Text type="secondary">
                  {normalizedAuthorId
                    ? '仅展示该同事对您当前身份或所属部门开放访问的在线作品'
                    : '发现并一键打开同事分享给您、您所在部门或全公司的内部看板与工具'}
                </Text>
              </div>
              {normalizedAuthorId ? (
                <Button
                  icon={<ArrowLeftOutlined />}
                  onClick={() => navigate('/portal/colleague-works')}
                >
                  返回全部同事作品
                </Button>
              ) : null}
            </header>

            {normalizedAuthorId && selectedAuthor ? (
              <Card size="small" className="soha-portal-selected-author-banner" variant="outlined">
                <div className="soha-portal-selected-author-row">
                  <Space size={12}>
                    <Avatar
                      size={44}
                      src={selectedAuthor.avatarUrl || undefined}
                      icon={<UserOutlined />}
                    />
                    <div>
                      <Space size={8} wrap>
                        <Text strong>{selectedAuthor.displayName}</Text>
                        {selectedAuthor.teamNames.map((team) => (
                          <MetadataTag key={team} label={team} />
                        ))}
                      </Space>
                      <div>
                        <Text type="secondary">
                          当前对您开放 {selectedAuthor.visibleAppCount} 个在线作品
                        </Text>
                      </div>
                    </div>
                  </Space>
                </div>
              </Card>
            ) : null}

            <div className="soha-portal-colleague-toolbar" role="search">
              <Input
                allowClear
                aria-label="搜索同事作品"
                className="soha-portal-colleague-search"
                placeholder="搜索作品名称或发布者姓名"
                prefix={<SearchOutlined />}
                value={keyword}
                variant="filled"
                onChange={(event) => setKeyword(event.target.value)}
              />
              <div className="soha-portal-colleague-filters">
                <Segmented
                  aria-label="作品类型筛选"
                  options={[
                    { label: '全部', value: 'all' },
                    { label: '页面看板', value: 'static' },
                    { label: '交互工具', value: 'dynamic' },
                  ]}
                  value={kindFilter}
                  onChange={(next) => setKindFilter(next as ColleagueKindFilter)}
                />
                <Button
                  aria-label="刷新同事作品"
                  icon={<ReloadOutlined />}
                  loading={catalogQuery.isFetching}
                  onClick={() => void catalogQuery.refetch()}
                />
              </div>
            </div>

            {catalogQuery.isLoading && !catalog ? (
              <div
                className="soha-portal-work-grid"
                role="status"
                aria-busy="true"
                aria-label="正在加载同事作品"
              >
                {Array.from({ length: 6 }, (_, index) => (
                  <Card key={index} size="small" variant="outlined">
                    <Skeleton active avatar paragraph={{ rows: 2 }} />
                  </Card>
                ))}
              </div>
            ) : catalogQuery.isError && !catalog ? (
              <ManagementState
                kind="error"
                title="同事作品加载失败"
                description="暂时无法读取同事作品目录，请检查网络或稍后重试。"
                actions={
                  <Button type="primary" onClick={() => void catalogQuery.refetch()}>
                    立即重试
                  </Button>
                }
              />
            ) : (
              <>
                {!normalizedAuthorId && authors.length > 0 ? (
                  <section aria-label="匹配的发布者" className="soha-portal-authors-section">
                    <Text strong className="soha-portal-subsection-title">
                      {keyword.trim() ? '匹配的同事主页' : '按发布者浏览'}
                    </Text>
                    <div className="soha-portal-author-grid">
                      {authors.map((author) => (
                        <AuthorSummaryCard
                          key={author.userId}
                          author={author}
                          onSelect={openAuthorPage}
                        />
                      ))}
                    </div>
                  </section>
                ) : null}

                {items.length > 0 ? (
                  <div className="soha-portal-work-grid">
                    {items.map((item) => (
                      <ColleagueWorkCard
                        key={item.id}
                        item={item}
                        localeCode={localeCode}
                        onOpenAuthor={openAuthorPage}
                      />
                    ))}
                  </div>
                ) : hasActiveFilter ? (
                  <div className="soha-portal-empty">
                    <Empty
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                      description="未找到匹配的同事作品，请尝试更换作品名或同事姓名关键词"
                    >
                      <Button
                        onClick={() => {
                          setKeyword('')
                          setKindFilter('all')
                        }}
                      >
                        清空筛选条件
                      </Button>
                    </Empty>
                  </div>
                ) : (
                  <div className="soha-portal-empty soha-portal-empty-guide">
                    <Empty
                      image={Empty.PRESENTED_IMAGE_SIMPLE}
                      description={
                        normalizedAuthorId
                          ? '该同事当前没有对您开放的在线作品'
                          : '当前暂无对您开放的同事作品'
                      }
                    >
                      {!normalizedAuthorId ? (
                        <Space orientation="vertical" size={8} align="center">
                          <Text type="secondary">
                            您可以使用本地 CLI（<code>soha host deploy ./dist --name &quot;季度看板&quot;</code>）或通过个人 AI 助手发布第一个内部作品。
                          </Text>
                          <Space wrap>
                            {canOpenHeya ? (
                              <Link to="/heya">
                                <Button type="primary">前往 Heya 个人助手发布</Button>
                              </Link>
                            ) : null}
                            {canManageMyApps ? (
                              <Link to="/portal/my-apps">
                                <Button>前往我的作品</Button>
                              </Link>
                            ) : null}
                          </Space>
                        </Space>
                      ) : (
                        <Button onClick={() => navigate('/portal/colleague-works')}>
                          返回全部同事作品
                        </Button>
                      )}
                    </Empty>
                  </div>
                )}
              </>
            )}
          </section>
        ) : null}
      </main>
    </div>
  )
}
