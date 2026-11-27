import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import type { IdentityApplication } from '@/features/identity'
import { useI18n } from '@/i18n'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  InfoCircleOutlined,
  LoadingOutlined,
  MoreOutlined,
  ReloadOutlined,
  SearchOutlined,
  StarFilled,
  StarOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { InputRef } from 'antd'
import { Button, Dropdown, Empty, Input, Spin, Tag, Tooltip, Typography } from 'antd'
import type { CSSProperties, PointerEvent as ReactPointerEvent } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { providerPortalMutations } from '../mutations'
import '../provider-portal-pages.css'
import { providerPortalQueries } from '../queries'
import { PortalApplicationAvatar, portalStatusLabels } from '../shared/application-ui'
import { useAnnouncementInbox } from '@/features/announcements'
import { portalApplicationSearchText } from '../shared/formatters'
import { PortalViewNav } from '../shared/portal-view-nav'

const { Text } = Typography

function ApplicationCard({
  application,
  canViewDetails,
  favoriteLoading,
  launchLoading,
  blocked,
  onFavoriteToggle,
  onLaunch,
  onViewDetails,
}: {
  application: IdentityApplication
  canViewDetails: boolean
  favoriteLoading: boolean
  launchLoading: boolean
  blocked: boolean
  onFavoriteToggle: (application: IdentityApplication) => void
  onLaunch: (application: IdentityApplication) => void
  onViewDetails: (application: IdentityApplication) => void
}) {
  const { t } = useI18n()
  const status = portalStatusLabels[application.status] ?? portalStatusLabels.draft
  const statusLabel = t(`providerPortal.application.status.${application.status}`, status.label)
  const favoriteLabel = application.favorite
    ? t('providerPortal.home.unfavorite', 'Unfavorite')
    : t('providerPortal.home.favorite', 'Favorite')
  const openLabel = t('providerPortal.home.open', 'Open')
  const isEnabled = application.status === 'enabled'
  const cardRef = useRef<HTMLDivElement>(null)

  const handlePointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    const node = cardRef.current
    if (!node) return
    const rect = node.getBoundingClientRect()
    if (!rect.width || !rect.height) return
    const px = (event.clientX - rect.left) / rect.width
    const py = (event.clientY - rect.top) / rect.height
    const rx = ((0.5 - py) * 9).toFixed(2)
    const ry = ((px - 0.5) * 9).toFixed(2)
    node.style.setProperty('--soha-card-rx', `${rx}deg`)
    node.style.setProperty('--soha-card-ry', `${ry}deg`)
    node.style.setProperty('--soha-card-gx', `${(px * 100).toFixed(1)}%`)
    node.style.setProperty('--soha-card-gy', `${(py * 100).toFixed(1)}%`)
  }

  const handlePointerLeave = () => {
    const node = cardRef.current
    if (!node) return
    node.style.setProperty('--soha-card-rx', '0deg')
    node.style.setProperty('--soha-card-ry', '0deg')
    node.style.setProperty('--soha-card-gx', '30%')
    node.style.setProperty('--soha-card-gy', '18%')
  }

  return (
    <div
      ref={cardRef}
      style={
        {
          '--soha-card-rx': '0deg',
          '--soha-card-ry': '0deg',
          '--soha-card-gx': '30%',
          '--soha-card-gy': '18%',
        } as CSSProperties
      }
      onPointerMove={handlePointerMove}
      onPointerLeave={handlePointerLeave}
      className={`soha-portal-app-card is-small${application.favorite ? ' is-favorite' : ''}${!isEnabled ? ' is-unavailable' : ''}`}
    >
      <button
        aria-label={`${openLabel} ${application.name}${isEnabled ? '' : `: ${statusLabel}`}`}
        aria-busy={launchLoading}
        className="soha-portal-app-launch"
        disabled={blocked || !isEnabled || launchLoading}
        type="button"
        onClick={() => onLaunch(application)}
      >
        <span className="soha-portal-app-icon">
          <PortalApplicationAvatar application={application} />
          {application.favorite ? (
            <span className="soha-portal-app-fav-badge" aria-hidden="true">
              <StarFilled />
            </span>
          ) : null}
          {launchLoading ? <LoadingOutlined className="soha-portal-app-loading" spin /> : null}
        </span>
        <span className="soha-portal-app-body">
          <Text className="soha-portal-app-title" ellipsis={{ tooltip: application.name }}>
            {application.name}
          </Text>
        </span>
      </button>
      {!isEnabled || application.featured ? (
        <div className="soha-portal-app-meta">
          {!isEnabled ? <StatusTag value={application.status} label={statusLabel} /> : null}
          {application.featured ? (
            <Tag color="blue">{t('providerPortal.home.featured', 'Featured')}</Tag>
          ) : null}
        </div>
      ) : null}
      <Dropdown
        menu={{
          items: [
            {
              key: 'favorite',
              icon: application.favorite ? <StarFilled /> : <StarOutlined />,
              label: favoriteLabel,
              disabled: blocked || favoriteLoading,
            },
            ...(canViewDetails
              ? [
                  {
                    key: 'details',
                    icon: <InfoCircleOutlined />,
                    label: t('providerPortal.home.details', 'Details'),
                  },
                ]
              : []),
          ],
          onClick: ({ key }) => {
            if (key === 'favorite') onFavoriteToggle(application)
            if (key === 'details') onViewDetails(application)
          },
        }}
        placement="bottomRight"
        trigger={['click']}
      >
        <Button
          aria-label={`${t('common.actions', 'Actions')} ${application.name}`}
          className="soha-portal-app-more"
          icon={<MoreOutlined />}
          loading={favoriteLoading}
          size="small"
          type="text"
        />
      </Dropdown>
    </div>
  )
}

export function SohaProviderPortalPage() {
  const navigate = useNavigate()
  const [searchParams] = useSearchParams()
  const { t } = useI18n()
  const queryClient = useQueryClient()
  const [query, setQuery] = useState('')
  const [isSearchOpen, setIsSearchOpen] = useState(false)
  const searchInputRef = useRef<InputRef>(null)

  const bootstrapQuery = useQuery(providerPortalQueries.bootstrap())
  const permissionSnapshotQuery = usePermissionSnapshot()
  const launchMutation = useOperationMutation(providerPortalMutations.launch(queryClient), {
    label: ['打开应用', 'Open application'],
    target: (v) => v.name,
    resultError: (decision) =>
      decision.launchUrl
        ? undefined
        : new Error(
            t(
              'providerPortal.home.launchUrlNotConfigured',
              'Application launch URL is not configured',
            ),
          ),
    receipt: () => ({
      message: '已收到应用入口',
      evidence: '目标系统是否登录成功由目标系统确认。',
    }),
  })
  const favoriteMutation = useOperationMutation(
    providerPortalMutations.toggleFavorite(queryClient),
    {
      label: ['更新应用收藏', 'Update favorite'],
      target: (v) => v.name,
      receipt: (_, v) => ({ message: v.favorite ? '已取消收藏' : '已收藏' }),
    },
  )
  const announcementQuery = useAnnouncementInbox()

  const openSearch = () => {
    setIsSearchOpen(true)
    searchInputRef.current?.focus({ cursor: 'end' })
    window.requestAnimationFrame(() => {
      searchInputRef.current?.focus({ cursor: 'end' })
    })
  }

  useEffect(() => {
    const handleKeyDown = (event: KeyboardEvent) => {
      const target = event.target as HTMLElement | null
      const inEditable = Boolean(
        target &&
          (target.tagName === 'INPUT' ||
            target.tagName === 'TEXTAREA' ||
            target.isContentEditable),
      )

      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'f') {
        event.preventDefault()
        openSearch()
        return
      }

      if (!inEditable && !event.metaKey && !event.ctrlKey && !event.altKey) {
        if (event.key === '/') {
          event.preventDefault()
          openSearch()
          return
        }
        if (event.key.length === 1 && /\S/.test(event.key)) {
          setIsSearchOpen(true)
          searchInputRef.current?.focus({ cursor: 'end' })
        }
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [])

  const launchApplication = (application: IdentityApplication) => {
    launchMutation.mutate(application, {
      onSuccess: (decision) => {
        if (decision.launchUrl) window.location.assign(decision.launchUrl)
      },
    })
  }

  const bootstrap = readableQueryData(bootstrapQuery)
  const operationBlocked =
    launchMutation.blocked || favoriteMutation.blocked || queryAccessDenied(bootstrapQuery)
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canViewApplicationDetails = hasPermission(permissionSnapshot, 'identity.applications.view')
  const applications = bootstrap?.applications ?? []

  const favoritesOnly = searchParams.get('group') === 'favorites'
  const selectedTag = searchParams.get('tag')?.trim() || undefined
  const activeGroupTitle = favoritesOnly
    ? t('providerPortal.home.favorites', 'Favorites')
    : selectedTag || t('providerPortal.home.allTags', 'All')

  const filteredApplications = useMemo(() => {
    const keyword = query.trim().toLowerCase()
    return applications.filter((application) => {
      if (favoritesOnly && !application.favorite) return false
      if (selectedTag && !application.tags?.includes(selectedTag)) return false
      if (keyword && !portalApplicationSearchText(application).includes(keyword)) return false
      return true
    })
  }, [applications, favoritesOnly, query, selectedTag])

  if (bootstrapQuery.isLoading) {
    return (
      <div className="soha-provider-portal is-loading">
        <Spin size="large" />
      </div>
    )
  }

  const searchExpanded = isSearchOpen || Boolean(query)
  const searchLabel = t('providerPortal.home.searchApplications', 'Search applications')

  return (
    <div className="soha-page soha-provider-portal soha-provider-portal-home is-app-shell">
      <PortalViewNav active="enterprise" />
      <ReadFeedback query={bootstrapQuery} label="应用门户" />
      <ReadFeedback
        query={announcementQuery}
        label="门户公告"
        canRead={announcementQuery.isEnabled}
      />
      {!queryAccessDenied(bootstrapQuery) ? (
        <>
          {launchMutation.feedback}
          {favoriteMutation.feedback}
        </>
      ) : null}

      <section className="soha-portal-apps">
        <div
          className="soha-portal-stage-head"
          aria-label={t('providerPortal.home.applications', 'Applications')}
        >
          <div className="soha-portal-stage-head__title">
            <span>{activeGroupTitle}</span>
            <span className="soha-portal-count-pill" aria-hidden="true">
              {filteredApplications.length}
            </span>
          </div>

          <div className={`soha-portal-expandable-search${searchExpanded ? ' is-expanded' : ''}`}>
            {!searchExpanded ? (
              <Tooltip placement="bottomRight" title={`${searchLabel} ( / )`}>
                <button
                  type="button"
                  aria-label={searchLabel}
                  className="soha-portal-search-trigger"
                  onClick={openSearch}
                >
                  <SearchOutlined />
                </button>
              </Tooltip>
            ) : null}
            <div className="soha-portal-search-input-wrap" aria-hidden={!searchExpanded}>
              <Input
                ref={searchInputRef}
                allowClear
                tabIndex={searchExpanded ? 0 : -1}
                className="soha-portal-search soha-portal-app-search"
                placeholder={searchLabel}
                prefix={<SearchOutlined />}
                value={query}
                variant="borderless"
                onBlur={(event) => {
                  if (!event.currentTarget.value.trim()) setIsSearchOpen(false)
                }}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    setQuery('')
                    setIsSearchOpen(false)
                    ;(event.currentTarget as HTMLInputElement).blur()
                  }
                }}
                onChange={(event) => setQuery(event.target.value)}
              />
            </div>
          </div>
          <Tooltip title={t('providerPortal.home.refresh', '刷新应用门户')}>
            <Button
              aria-label={t('providerPortal.home.refresh', '刷新应用门户')}
              icon={<ReloadOutlined />}
              loading={bootstrapQuery.isFetching}
              size="small"
              type="text"
              onClick={() => void bootstrapQuery.refetch()}
            />
          </Tooltip>
        </div>

        <div className="soha-portal-catalog-results">
          {bootstrapQuery.isError && !bootstrap ? null : filteredApplications.length ? (
            <div className="soha-portal-app-grid is-small">
              {filteredApplications.map((application) => (
                <ApplicationCard
                  application={application}
                  blocked={operationBlocked}
                  canViewDetails={canViewApplicationDetails}
                  favoriteLoading={
                    favoriteMutation.isPending &&
                    favoriteMutation.variables?.id === application.id
                  }
                  key={application.id}
                  launchLoading={
                    launchMutation.isPending && launchMutation.variables?.id === application.id
                  }
                  onFavoriteToggle={(item) => favoriteMutation.mutate(item)}
                  onLaunch={launchApplication}
                  onViewDetails={(item) =>
                    navigate(`/portal/applications/${encodeURIComponent(item.id)}`)
                  }
                />
              ))}
            </div>
          ) : (
            <div className="soha-portal-empty">
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={
                  favoritesOnly && !query.trim()
                    ? t('providerPortal.home.noFavorites', 'No favorite applications')
                    : t('providerPortal.home.noMatchingApplications', 'No matching applications')
                }
              />
            </div>
          )}
        </div>
      </section>
    </div>
  )
}
