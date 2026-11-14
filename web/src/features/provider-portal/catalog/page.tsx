import { ManagementDensityButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useAnnouncementInbox, type AnnouncementInboxItem } from '@/features/announcements'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import type { IdentityApplication } from '@/features/identity'
import { MENU_WORKBENCH_LABELS, MENU_WORKBENCH_ORDER, resolveMenuIcon } from '@/features/system'
import { useI18n } from '@/i18n'
import {
  canAccessRoute,
  findFirstAccessiblePathForWorkbench,
  getAccessibleWorkbenchIds,
  getMenuWorkbenchId,
  getRouteMeta,
} from '@/routes/meta'
import { useAuthStore } from '@/stores/auth-store'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import {
  AppstoreOutlined,
  BellOutlined,
  InfoCircleOutlined,
  LeftOutlined,
  LoadingOutlined,
  MenuFoldOutlined,
  MoreOutlined,
  ReloadOutlined,
  RightOutlined,
  SearchOutlined,
  StarFilled,
  StarOutlined,
  TagsOutlined,
  UserOutlined,
} from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Badge, Button, Dropdown, Empty, Input, Menu, Spin, Tag, Typography } from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { providerPortalMutations } from '../mutations'
import '../provider-portal-pages.css'
import { providerPortalQueries } from '../queries'
import { PortalUserAvatar } from '../shared/account-menu'
import { PortalApplicationAvatar, portalStatusLabels } from '../shared/application-ui'
import { formatPortalDateTime, portalApplicationSearchText } from '../shared/formatters'
import type { PortalSecuritySummary } from '../shared/types'

const { Paragraph, Text } = Typography

type PortalApplicationView = 'small' | 'medium'

interface PortalLayoutPreferences {
  applicationView: PortalApplicationView
  isGroupCollapsed: boolean
  isSideCollapsed: boolean
}

const APPLICATION_VIEW_ORDER: PortalApplicationView[] = ['small', 'medium']
const ALL_APPLICATIONS_TAB_KEY = '__all_applications__'
const FAVORITE_APPLICATIONS_TAB_KEY = '__favorite_applications__'
const APPLICATION_TAG_TAB_PREFIX = 'tag:'
const PORTAL_LAYOUT_STORAGE_KEY_PREFIX = 'soha-provider-portal-layout:v1'
const DEFAULT_PORTAL_LAYOUT_PREFERENCES: PortalLayoutPreferences = {
  applicationView: 'medium',
  isGroupCollapsed: false,
  isSideCollapsed: false,
}

function applicationTagTabKey(tag: string) {
  return `${APPLICATION_TAG_TAB_PREFIX}${tag}`
}

function cycleApplicationView(view: PortalApplicationView) {
  const currentIndex = APPLICATION_VIEW_ORDER.indexOf(view)
  return APPLICATION_VIEW_ORDER[(currentIndex + 1) % APPLICATION_VIEW_ORDER.length]
}

function portalLayoutStorageKey(userId: string) {
  return `${PORTAL_LAYOUT_STORAGE_KEY_PREFIX}:${encodeURIComponent(userId)}`
}

function readPortalLayoutPreferences(userId?: string): PortalLayoutPreferences {
  if (!userId || typeof window === 'undefined') return DEFAULT_PORTAL_LAYOUT_PREFERENCES

  try {
    const raw = window.localStorage.getItem(portalLayoutStorageKey(userId))
    if (!raw) return DEFAULT_PORTAL_LAYOUT_PREFERENCES
    const stored = JSON.parse(raw) as Partial<PortalLayoutPreferences> | null
    return {
      applicationView:
        stored?.applicationView === 'small' || stored?.applicationView === 'medium'
          ? stored.applicationView
          : DEFAULT_PORTAL_LAYOUT_PREFERENCES.applicationView,
      isGroupCollapsed: stored?.isGroupCollapsed === true,
      isSideCollapsed: stored?.isSideCollapsed === true,
    }
  } catch {
    return DEFAULT_PORTAL_LAYOUT_PREFERENCES
  }
}

function writePortalLayoutPreferences(userId: string | undefined, value: PortalLayoutPreferences) {
  if (!userId || typeof window === 'undefined') return

  try {
    window.localStorage.setItem(portalLayoutStorageKey(userId), JSON.stringify(value))
  } catch {
    return
  }
}

function ApplicationCard({
  application,
  canViewDetails,
  favoriteLoading,
  launchLoading,
  viewMode,
  blocked,
  onFavoriteToggle,
  onLaunch,
  onViewDetails,
}: {
  application: IdentityApplication
  canViewDetails: boolean
  favoriteLoading: boolean
  launchLoading: boolean
  viewMode: PortalApplicationView
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
  return (
    <div className={`soha-portal-app-card is-${viewMode}`}>
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
          {launchLoading ? <LoadingOutlined className="soha-portal-app-loading" spin /> : null}
        </span>
        <Text className="soha-portal-app-title" ellipsis={{ tooltip: application.name }}>
          {application.name}
        </Text>
        {viewMode === 'small' ? null : (
          <Text className="soha-portal-app-description" ellipsis={{ tooltip: true }}>
            {application.description || t('providerPortal.home.noDescription', 'No description')}
          </Text>
        )}
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

function PortalUserPanel({ security }: { security?: PortalSecuritySummary }) {
  const { t, localeCode } = useI18n()
  const principal = security?.principal
  return (
    <div className="soha-portal-side-body">
      <div className="soha-portal-principal">
        <PortalUserAvatar size={40} />
        <div className="soha-portal-principal-copy">
          <Text strong>{principal?.userName || t('layout.user', 'User')}</Text>
          <Text type="secondary" ellipsis title={principal?.email}>
            {principal?.email || principal?.userId || '-'}
          </Text>
        </div>
      </div>
      <div className="soha-portal-security-grid">
        <div>
          <Text type="secondary">{t('providerPortal.home.mfa', 'MFA')}</Text>
          <div>
            <Badge
              status={security?.mfaEnabled ? 'success' : 'default'}
              text={
                security === undefined
                  ? '-'
                  : security.mfaEnabled
                    ? t('providerPortal.home.enabled', 'Enabled')
                    : t('providerPortal.home.notEnabled', 'Not enabled')
              }
            />
          </div>
        </div>
        <div>
          <Text type="secondary">{t('providerPortal.home.sessions', 'Sessions')}</Text>
          <div className="soha-portal-side-value">{security?.activeSession ?? '-'}</div>
        </div>
        <div>
          <Text type="secondary">{t('providerPortal.home.sources', 'Sources')}</Text>
          <div className="soha-portal-side-value">{security?.linkedSources.length ?? '-'}</div>
        </div>
        <div>
          <Text type="secondary">{t('providerPortal.home.recentLogin', 'Recent login')}</Text>
          <div className="soha-portal-side-value">
            {formatPortalDateTime(security?.recentLoginAt, localeCode)}
          </div>
        </div>
      </div>
    </div>
  )
}

function announcementLevel(
  item: AnnouncementInboxItem,
  t: (key: string, fallback?: string) => string,
) {
  const normalized = String(item.level || '').toLowerCase()
  if (normalized === 'critical') {
    return {
      status: 'error' as const,
      label: t('providerPortal.home.announcementLevel.critical', 'Critical'),
    }
  }
  if (normalized === 'warning') {
    return {
      status: 'warning' as const,
      label: t('providerPortal.home.announcementLevel.warning', 'Warning'),
    }
  }
  return {
    status: 'processing' as const,
    label: t('providerPortal.home.announcementLevel.info', 'Info'),
  }
}

function PortalAnnouncementPanel({
  items,
  isLoading,
  unreadCount,
}: {
  items: AnnouncementInboxItem[]
  isLoading: boolean
  unreadCount: number
}) {
  const { t, localeCode } = useI18n()
  const trackRef = useRef<HTMLDivElement>(null)
  const [activeIndex, setActiveIndex] = useState(0)

  useEffect(() => {
    setActiveIndex((current) => Math.min(current, Math.max(items.length - 1, 0)))
  }, [items.length])

  useEffect(() => {
    const track = trackRef.current
    const item = track?.children.item(activeIndex)
    if (!track || !(item instanceof HTMLElement)) return
    track.scrollTo?.({ behavior: 'smooth', left: item.offsetLeft })
  }, [activeIndex])

  useEffect(() => {
    if (items.length < 2) return
    const intervalID = window.setInterval(() => {
      setActiveIndex((current) => (current + 1) % items.length)
    }, 6000)
    return () => window.clearInterval(intervalID)
  }, [items.length])

  if (!isLoading && items.length === 0) return null

  return (
    <section
      aria-label={t('providerPortal.home.announcements', 'Announcements')}
      className="soha-portal-announcements"
    >
      <div className="soha-portal-announcement-heading">
        <div className="soha-portal-side-title">
          <BellOutlined />
          <span>{t('providerPortal.home.announcements', 'Announcements')}</span>
          {unreadCount > 0 ? (
            <Badge
              className="soha-portal-announcement-unread"
              count={unreadCount}
              size="small"
              overflowCount={99}
            />
          ) : null}
        </div>
      </div>
      {isLoading ? (
        <div className="soha-portal-announcement-empty">
          <Spin size="small" />
        </div>
      ) : items.length ? (
        <div
          ref={trackRef}
          className="soha-portal-announcement-track"
          data-active-index={activeIndex}
          aria-live="polite"
        >
          {items.map((item) => {
            const level = announcementLevel(item, t)
            return (
              <article className="soha-portal-announcement-item" key={item.id}>
                <div className="soha-portal-announcement-item-meta">
                  <Badge status={level.status} text={level.label} />
                  <Text type="secondary">
                    {formatPortalDateTime(
                      item.publishedAt || item.updatedAt || item.createdAt,
                      localeCode,
                    )}
                  </Text>
                </div>
                <Text strong>{item.title}</Text>
                {item.content ? (
                  <Paragraph className="soha-portal-announcement-content">{item.content}</Paragraph>
                ) : null}
              </article>
            )
          })}
        </div>
      ) : (
        <div className="soha-portal-announcement-empty">
          <Text type="secondary">
            {t('providerPortal.home.noAnnouncements', 'No announcements')}
          </Text>
        </div>
      )}
    </section>
  )
}

export function SohaProviderPortalPage() {
  const navigate = useNavigate()
  const { t, localeCode } = useI18n()
  const queryClient = useQueryClient()
  const currentUserId = useAuthStore((state) => state.user?.userId)
  const [query, setQuery] = useState('')
  const [selectedGroup, setSelectedGroup] = useState(ALL_APPLICATIONS_TAB_KEY)
  const [portalLayout, setPortalLayout] = useState(() => readPortalLayoutPreferences(currentUserId))
  const { applicationView, isGroupCollapsed, isSideCollapsed } = portalLayout

  const updatePortalLayout = (updates: Partial<PortalLayoutPreferences>) => {
    setPortalLayout((current) => {
      const next = { ...current, ...updates }
      writePortalLayoutPreferences(currentUserId, next)
      return next
    })
  }

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
  const announcementQuery = useAnnouncementInbox(10, true)

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
  const workbenches = useMemo(() => {
    if (!permissionSnapshot) return []
    return getAccessibleWorkbenchIds(permissionSnapshot)
      .filter((workbenchId) => workbenchId !== 'home')
      .flatMap((workbenchId) => {
        const path = findFirstAccessiblePathForWorkbench(workbenchId, permissionSnapshot)
        if (!path) return []
        const menu = permissionSnapshot.visibleMenus
          .filter((item) => getMenuWorkbenchId(item) === workbenchId)
          .sort(
            (left, right) =>
              Number(Boolean(left.parentId)) - Number(Boolean(right.parentId)) ||
              (left.sortOrder ?? Number.MAX_SAFE_INTEGER) -
                (right.sortOrder ?? Number.MAX_SAFE_INTEGER),
          )[0]
        return [
          {
            iconKey: menu?.iconKey,
            id: workbenchId,
            label:
              (localeCode === 'en_US'
                ? menu?.labelEn || menu?.labelZh
                : MENU_WORKBENCH_LABELS[workbenchId]) || workbenchId,
            path,
          },
        ]
      })
      .sort(
        (left, right) =>
          MENU_WORKBENCH_ORDER.indexOf(left.id) - MENU_WORKBENCH_ORDER.indexOf(right.id),
      )
  }, [localeCode, permissionSnapshot])
  const applications = bootstrap?.applications ?? []
  const announcementItems = readableQueryData(announcementQuery)?.data.items ?? []
  const announcementUnreadCount = readableQueryData(announcementQuery)?.data.unreadCount ?? 0
  const applicationTags = useMemo(
    () =>
      Array.from(
        new Set(applications.flatMap((application) => application.tags?.filter(Boolean) ?? [])),
      ),
    [applications],
  )
  const applicationGroupItems = useMemo(
    () => [
      {
        key: FAVORITE_APPLICATIONS_TAB_KEY,
        icon: <StarOutlined />,
        label: t('providerPortal.home.favorites', 'Favorites'),
      },
      {
        key: ALL_APPLICATIONS_TAB_KEY,
        label: t('providerPortal.home.allTags', 'All'),
      },
      ...applicationTags.map((tag) => ({
        key: applicationTagTabKey(tag),
        label: tag,
      })),
    ],
    [applicationTags, t],
  )
  const selectedTag = selectedGroup.startsWith(APPLICATION_TAG_TAB_PREFIX)
    ? selectedGroup.slice(APPLICATION_TAG_TAB_PREFIX.length)
    : undefined
  const favoritesOnly = selectedGroup === FAVORITE_APPLICATIONS_TAB_KEY

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

  return (
    <div className="soha-provider-portal soha-provider-portal-home">
      <main className="soha-portal-main">
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
        <div
          className={`soha-portal-workspace${isGroupCollapsed ? ' is-group-collapsed' : ''}${isSideCollapsed ? ' is-side-collapsed' : ''}`}
        >
          {isGroupCollapsed ? null : (
            <nav
              aria-label={t('providerPortal.home.applicationGroups', 'Application groups')}
              className="soha-portal-group-nav"
            >
              <div className="soha-portal-group-nav-heading">
                <span className="soha-portal-group-nav-heading-main">
                  <TagsOutlined />
                  <Text strong>
                    {t('providerPortal.home.applicationGroups', 'Application groups')}
                  </Text>
                </span>
                <span className="soha-portal-group-nav-heading-actions">
                  <Text type="secondary">{applicationTags.length}</Text>
                  <Button
                    aria-label={t(
                      'providerPortal.home.collapseApplicationGroups',
                      'Collapse application groups',
                    )}
                    className="soha-portal-group-nav-toggle"
                    icon={<MenuFoldOutlined />}
                    size="small"
                    type="text"
                    onClick={() => updatePortalLayout({ isGroupCollapsed: true })}
                  />
                </span>
              </div>
              <Menu
                className="soha-portal-group-menu"
                items={applicationGroupItems}
                mode="inline"
                selectedKeys={[selectedGroup]}
                onClick={({ key }) => setSelectedGroup(key)}
              />
            </nav>
          )}

          <section className="soha-portal-apps">
            <div
              className="soha-portal-app-toolbar"
              aria-label={t('providerPortal.home.applications', 'Applications')}
            >
              <Input
                allowClear
                className="soha-portal-search soha-portal-app-search"
                placeholder={t('providerPortal.home.searchApplications', 'Search applications')}
                prefix={<SearchOutlined />}
                value={query}
                variant="filled"
                onChange={(event) => setQuery(event.target.value)}
              />
              <div className="soha-portal-view-actions">
                <Button
                  aria-label="刷新门户"
                  icon={<ReloadOutlined />}
                  loading={bootstrapQuery.isFetching || announcementQuery.isFetching}
                  onClick={() =>
                    void Promise.all([
                      bootstrapQuery.refetch(),
                      announcementQuery.isEnabled === false
                        ? undefined
                        : announcementQuery.refetch(),
                    ])
                  }
                />
                {isGroupCollapsed ? (
                  <Button
                    aria-label={t(
                      'providerPortal.home.expandApplicationGroups',
                      'Expand application groups',
                    )}
                    icon={<RightOutlined />}
                    size="small"
                    type="text"
                    onClick={() => updatePortalLayout({ isGroupCollapsed: false })}
                  />
                ) : null}
                {isSideCollapsed ? (
                  <Button
                    aria-label={t('providerPortal.home.expandSidebar', 'Expand sidebar')}
                    icon={<LeftOutlined />}
                    size="small"
                    type="text"
                    onClick={() => updatePortalLayout({ isSideCollapsed: false })}
                  />
                ) : null}
                <ManagementDensityButton
                  aria-label={t(
                    'providerPortal.home.cycleApplicationView',
                    'Switch application card size',
                  )}
                  tooltip={t(
                    'providerPortal.home.cycleApplicationView',
                    'Switch application card size',
                  )}
                  onClick={() =>
                    updatePortalLayout({ applicationView: cycleApplicationView(applicationView) })
                  }
                />
              </div>
            </div>
            <PortalAnnouncementPanel
              isLoading={announcementQuery.isLoading}
              items={announcementItems}
              unreadCount={announcementUnreadCount}
            />
            <div className="soha-portal-catalog-results">
              {bootstrapQuery.isError && !bootstrap ? null : filteredApplications.length ? (
                <div className={`soha-portal-app-grid is-${applicationView}`}>
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
                      viewMode={applicationView}
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
                        : t(
                            'providerPortal.home.noMatchingApplications',
                            'No matching applications',
                          )
                    }
                  />
                </div>
              )}
            </div>
          </section>

          {isSideCollapsed ? null : (
            <aside className="soha-portal-side">
              <Button
                aria-label={t('providerPortal.home.collapseSidebar', 'Collapse sidebar')}
                className="soha-portal-side-toggle"
                icon={<RightOutlined />}
                size="small"
                type="text"
                onClick={() => updatePortalLayout({ isSideCollapsed: true })}
              />
              <section className="soha-portal-side-panel">
                <div className="soha-portal-side-title">
                  <UserOutlined />
                  <span>{t('providerPortal.home.user', 'User')}</span>
                </div>
                <PortalUserPanel security={bootstrap?.security} />
              </section>
              {workbenches.length ? (
                <section
                  aria-label={t('providerPortal.home.workbenches', 'Workbenches')}
                  className="soha-portal-side-panel soha-portal-workbenches"
                >
                  <div className="soha-portal-side-title">
                    <AppstoreOutlined />
                    <span>{t('providerPortal.home.workbenches', 'Workbenches')}</span>
                  </div>
                  <div className="soha-portal-workbench-list">
                    {canAccessRoute(getRouteMeta('/heya'), permissionSnapshot) ? (
                      <Link
                        aria-label={localeCode === 'en_US' ? 'Open Heya' : '打开 Heya'}
                        className="soha-portal-recent-item soha-portal-workbench-link"
                        to="/heya"
                      >
                        <span className="soha-portal-workbench-icon">
                          <img src="/heya-avatar.svg" width={24} height={24} alt="" />
                        </span>
                        <Text className="soha-portal-workbench-label" strong>
                          Heya
                        </Text>
                        <RightOutlined className="soha-portal-workbench-arrow" />
                      </Link>
                    ) : null}
                    {workbenches.map((workbench) => (
                      <Link
                        aria-label={`${t('providerPortal.home.open', 'Open')} ${workbench.label}`}
                        className="soha-portal-recent-item soha-portal-workbench-link"
                        key={workbench.id}
                        to={workbench.path}
                      >
                        <span className="soha-portal-workbench-icon">
                          {resolveMenuIcon(workbench.iconKey)}
                        </span>
                        <Text
                          className="soha-portal-workbench-label"
                          ellipsis
                          strong
                          title={workbench.label}
                        >
                          {workbench.label}
                        </Text>
                        <RightOutlined className="soha-portal-workbench-arrow" />
                      </Link>
                    ))}
                  </div>
                </section>
              ) : null}
            </aside>
          )}
        </div>
      </main>
    </div>
  )
}
