import {
  AppstoreOutlined,
  BranchesOutlined,
  ClusterOutlined,
  RadarChartOutlined,
  RightOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
  StarOutlined,
  TagsOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { Tooltip } from 'antd'
import type { ReactNode } from 'react'
import { Fragment, useMemo, useState } from 'react'
import { AnimatedIcon } from '@/components/animated-icon'
import {
  isComputeWorkbenchMenuGroup,
  normalizeComputeWorkbenchNav,
} from '@/features/compute/navigation'
import { providerPortalQueries } from '@/features/provider-portal'
import { resolveMenuIcon } from '@/features/system/menu-icons'
import { resolveMenuSectionLabel } from '@/features/system/menu-schema'
import {
  canAccessRoute,
  filterSidebarNavByWorkbench,
  filterSidebarNavByWorkspace,
  findFirstAccessiblePathForWorkbench,
  getRouteMeta,
  type WorkbenchId,
} from '@/routes/meta'
import type { PermissionSnapshot, RuntimeMenuNode } from '@/types'

export type DualRailCollapseStage = 'expanded' | 'rail-only'

export interface DualRailWorkbenchItem {
  key: WorkbenchId | 'account'
  label: string
  description?: string
  icon: ReactNode
}

export interface UnifiedNavLeaf {
  id: string
  label: string
  icon: ReactNode
  path: string
  count?: number
}

export interface UnifiedNavGroup {
  id: string
  title: string
  items: UnifiedNavLeaf[]
}

export interface UnifiedWorkbenchMenuModel {
  directItems: UnifiedNavLeaf[]
  groups: UnifiedNavGroup[]
}

function resolvePortalTagIcon(tag: string) {
  const normalized = tag.toLowerCase()
  if (normalized.includes('ai') || normalized.includes('模型')) return <RobotOutlined />
  if (normalized.includes('云原生') || normalized.includes('运维') || normalized.includes('prod')) {
    return <ClusterOutlined />
  }
  if (normalized.includes('研发') || normalized.includes('协同') || normalized.includes('build')) {
    return <BranchesOutlined />
  }
  if (normalized.includes('监控') || normalized.includes('数据')) return <RadarChartOutlined />
  if (normalized.includes('安全') || normalized.includes('治理')) {
    return <SafetyCertificateOutlined />
  }
  return <TagsOutlined />
}

function collectNodeLeaves(
  nodes: RuntimeMenuNode[] | undefined,
  resolveNodeLabel: (node: RuntimeMenuNode) => string,
  pathSuffix = '',
): UnifiedNavLeaf[] {
  if (!nodes?.length) return []
  const leaves: UnifiedNavLeaf[] = []
  for (const node of nodes) {
    if (node.children?.length) {
      leaves.push(...collectNodeLeaves(node.children, resolveNodeLabel, pathSuffix))
    } else if (node.route) {
      const basePath = node.route.redirectTo ?? node.route.path
      leaves.push({
        id: node.id,
        label: resolveNodeLabel(node),
        icon: resolveMenuIcon(node.iconKey),
        path: `${basePath}${pathSuffix}`,
      })
    }
  }
  return leaves
}

export function buildUnifiedWorkbenchMenu({
  workbenchKey,
  fullSidebarNav,
  localeCode,
  resolveNodeLabel,
  aiSearchSuffix = '',
  accountLeaves = [],
}: {
  workbenchKey: WorkbenchId | 'account'
  fullSidebarNav: RuntimeMenuNode[]
  localeCode: 'zh_CN' | 'en_US'
  resolveNodeLabel: (node: RuntimeMenuNode) => string
  aiSearchSuffix?: string
  accountLeaves?: UnifiedNavLeaf[]
}): UnifiedWorkbenchMenuModel {
  if (workbenchKey === 'account') {
    return { directItems: accountLeaves, groups: [] }
  }

  const wsNav =
    workbenchKey === 'delivery'
      ? filterSidebarNavByWorkspace(fullSidebarNav, 'application')
      : workbenchKey === 'settings' || workbenchKey === 'home'
        ? filterSidebarNavByWorkspace(fullSidebarNav, 'system')
        : workbenchKey === 'security'
          ? [
              ...filterSidebarNavByWorkspace(fullSidebarNav, 'resource'),
              ...filterSidebarNavByWorkspace(fullSidebarNav, 'system'),
            ]
          : filterSidebarNavByWorkspace(fullSidebarNav, 'resource')
  const rawWbNav = filterSidebarNavByWorkbench(wsNav, workbenchKey)
  const wbNav = workbenchKey === 'compute' ? normalizeComputeWorkbenchNav(rawWbNav) : rawWbNav
  const suffix = workbenchKey === 'ai' ? aiSearchSuffix : ''

  const directItems: UnifiedNavLeaf[] = []
  const groups: UnifiedNavGroup[] = []
  const sectionMap = new Map<string, UnifiedNavGroup>()

  for (const node of wbNav) {
    if (node.children?.length || isComputeWorkbenchMenuGroup(node.id)) {
      const items = collectNodeLeaves(node.children, resolveNodeLabel, suffix)
      if (items.length > 0) {
        groups.push({
          id: `tree-${node.id}`,
          title: resolveNodeLabel(node),
          items,
        })
      }
      continue
    }

    if (!node.route) continue
    const basePath = node.route.redirectTo ?? node.route.path
    const leaf: UnifiedNavLeaf = {
      id: node.id,
      label: resolveNodeLabel(node),
      icon: resolveMenuIcon(node.iconKey),
      path: `${basePath}${suffix}`,
    }

    const sectionKey = String(node.section || '').trim()
    if (!sectionKey) {
      directItems.push(leaf)
      continue
    }

    let group = sectionMap.get(sectionKey)
    if (!group) {
      group = {
        id: `sec-${sectionKey}`,
        title: resolveMenuSectionLabel(sectionKey, localeCode),
        items: [],
      }
      sectionMap.set(sectionKey, group)
      groups.push(group)
    }
    group.items.push(leaf)
  }

  return { directItems, groups }
}

function isLeafSelected(
  leaf: UnifiedNavLeaf,
  selectedIds: Set<string>,
  currentPathname: string,
  currentSearch = '',
) {
  if (leaf.id.startsWith('portal-')) {
    if (currentPathname === '/portal/security') {
      return leaf.id === 'portal-security'
    }
    if (currentPathname !== '/portal') {
      return leaf.id === 'portal-all' && currentPathname.startsWith('/portal/')
    }
    const params = new URLSearchParams(currentSearch)
    const activeGroup = params.get('group')
    const activeTag = params.get('tag')?.trim()
    if (leaf.id === 'portal-all') return !activeGroup && !activeTag
    if (leaf.id === 'portal-favorites') return activeGroup === 'favorites'
    if (leaf.id === 'portal-security') return false
    if (leaf.id.startsWith('portal-tag:')) {
      return activeTag === leaf.id.slice('portal-tag:'.length)
    }
    return false
  }

  if (selectedIds.has(leaf.id)) return true
  const cleanLeafPath = leaf.path.split('?')[0]
  if (cleanLeafPath === '/') return currentPathname === '/'
  return currentPathname === cleanLeafPath || currentPathname.startsWith(`${cleanLeafPath}/`)
}

export function DualRailGroupedMenu({
  model,
  selectedIds,
  currentPathname,
  currentSearch = '',
  onNavigate,
  compact = false,
}: {
  model: UnifiedWorkbenchMenuModel
  selectedIds: Set<string>
  currentPathname: string
  currentSearch?: string
  onNavigate: (path: string) => void
  compact?: boolean
}) {
  const totalGroupedItems = useMemo(
    () => model.groups.reduce((sum, g) => sum + g.items.length, 0),
    [model.groups],
  )
  const [manualToggles, setManualToggles] = useState<Record<string, boolean>>({})

  const isGroupExpanded = (group: UnifiedNavGroup, index: number) => {
    if (group.id in manualToggles) {
      return manualToggles[group.id]
    }
    const containsActive = group.items.some((item) =>
      isLeafSelected(item, selectedIds, currentPathname, currentSearch),
    )
    if (containsActive) return true
    if (totalGroupedItems > 14 && index >= 2) return false
    return true
  }

  return (
    <nav className={`soha-rail-grouped-nav${compact ? ' is-compact' : ''}`}>
      {model.directItems.length > 0 ? (
        <div className="soha-rail-grouped-nav__direct">
          {model.directItems.map((item) => {
            const active = isLeafSelected(item, selectedIds, currentPathname, currentSearch)
            return (
              <button
                key={item.id}
                type="button"
                className={`soha-rail-nav-item soha-icon-trigger${active ? ' is-active' : ''}`}
                onClick={() => onNavigate(item.path)}
              >
                <span className="soha-rail-nav-item__icon">{item.icon}</span>
                <span className="soha-rail-nav-item__label">{item.label}</span>
                {item.count !== undefined ? (
                  <span className="soha-rail-nav-item__badge">{item.count}</span>
                ) : null}
              </button>
            )
          })}
        </div>
      ) : null}

      {model.groups.map((group, idx) => {
        const expanded = isGroupExpanded(group, idx)
        const hasActiveChild = group.items.some((item) =>
          isLeafSelected(item, selectedIds, currentPathname, currentSearch),
        )
        return (
          <div
            key={group.id}
            className={`soha-rail-group${expanded ? ' is-open' : ' is-folded'}${
              hasActiveChild ? ' has-active' : ''
            }`}
          >
            <button
              type="button"
              aria-expanded={expanded}
              className="soha-rail-group__header"
              onClick={() =>
                setManualToggles((prev) => ({
                  ...prev,
                  [group.id]: !expanded,
                }))
              }
            >
              <RightOutlined className="soha-rail-group__chevron" />
              <span className="soha-rail-group__title">{group.title}</span>
              <span className="soha-rail-group__count">{group.items.length}</span>
            </button>

            {expanded ? (
              <div className="soha-rail-group__list">
                {group.items.map((item) => {
                  const active = isLeafSelected(item, selectedIds, currentPathname, currentSearch)
                  return (
                    <button
                      key={item.id}
                      type="button"
                      className={`soha-rail-nav-item soha-icon-trigger${active ? ' is-active' : ''}`}
                      onClick={() => onNavigate(item.path)}
                    >
                      <span className="soha-rail-nav-item__icon">{item.icon}</span>
                      <span className="soha-rail-nav-item__label">{item.label}</span>
                      {item.count !== undefined ? (
                        <span className="soha-rail-nav-item__badge">{item.count}</span>
                      ) : null}
                    </button>
                  )
                })}
              </div>
            ) : null}
          </div>
        )
      })}
    </nav>
  )
}

function DualRailNarrowIconMenu({
  model,
  selectedIds,
  currentPathname,
  currentSearch = '',
  onNavigate,
}: {
  model: UnifiedWorkbenchMenuModel
  selectedIds: Set<string>
  currentPathname: string
  currentSearch?: string
  onNavigate: (path: string) => void
}) {
  return (
    <nav className="soha-rail-narrow-nav">
      {model.directItems.map((item) => {
        const active = isLeafSelected(item, selectedIds, currentPathname, currentSearch)
        return (
          <Tooltip key={item.id} placement="right" title={item.label}>
            <button
              type="button"
              aria-label={item.label}
              className={`soha-rail-narrow-btn soha-icon-trigger${active ? ' is-active' : ''}`}
              onClick={() => onNavigate(item.path)}
            >
              {item.icon}
            </button>
          </Tooltip>
        )
      })}

      {model.groups.map((group, groupIdx) => {
        const showDivider = model.directItems.length > 0 || groupIdx > 0
        return (
          <div key={group.id} className="soha-rail-narrow-group">
            {showDivider ? <div className="soha-rail-narrow-divider" title={group.title} /> : null}
            {group.items.map((item) => {
              const active = isLeafSelected(item, selectedIds, currentPathname, currentSearch)
              const tip = `${group.title} · ${item.label}`
              return (
                <Tooltip key={item.id} placement="right" title={tip}>
                  <button
                    type="button"
                    aria-label={tip}
                    className={`soha-rail-narrow-btn soha-icon-trigger${active ? ' is-active' : ''}`}
                    onClick={() => onNavigate(item.path)}
                  >
                    {item.icon}
                  </button>
                </Tooltip>
              )
            })}
          </div>
        )
      })}
    </nav>
  )
}

export function DualRailSidebar({
  stage,
  workbenches,
  activeWorkbenchKey,
  activeWorkbenchLabel,
  fullSidebarNav,
  accountLeaves,
  selectedKeys,
  currentPathname,
  currentSearch = '',
  aiSearchSuffix,
  localeCode,
  snapshot,
  logoUrl,
  brandTitle,
  homePath,
  quickJumpTrigger,
  userPod,
  onOpenCommand,
  onNavigate,
  resolveNodeLabel,
}: {
  stage: DualRailCollapseStage
  workbenches: DualRailWorkbenchItem[]
  activeWorkbenchKey: WorkbenchId | 'account' | null
  activeWorkbenchLabel: string
  fullSidebarNav: RuntimeMenuNode[]
  accountLeaves: UnifiedNavLeaf[]
  selectedKeys: string[]
  currentPathname: string
  currentSearch?: string
  aiSearchSuffix: string
  localeCode: 'zh_CN' | 'en_US'
  snapshot: PermissionSnapshot | undefined
  logoUrl?: string
  brandTitle: string
  homePath: string
  quickJumpTrigger: ReactNode
  userPod?: ReactNode
  onOpenCommand: () => void
  onNavigate: (path: string) => void
  resolveNodeLabel: (node: RuntimeMenuNode) => string
}) {
  const selectedIds = useMemo(() => new Set(selectedKeys), [selectedKeys])
  const portalBootstrapQuery = useQuery({
    ...providerPortalQueries.bootstrap(),
    enabled: activeWorkbenchKey === 'home',
  })
  const portalApps = portalBootstrapQuery.data?.applications

  const activeModel = useMemo<UnifiedWorkbenchMenuModel>(() => {
    if (activeWorkbenchKey === 'home') {
      const apps = portalApps ?? []
      const favCount = apps.filter((app) => app.favorite).length
      const tags = Array.from(new Set(apps.flatMap((app) => app.tags?.filter(Boolean) ?? [])))
      return {
        directItems: [
          {
            id: 'portal-all',
            label: localeCode === 'en_US' ? 'All' : '全部',
            icon: <AppstoreOutlined />,
            path: '/portal',
            count: apps.length,
          },
          {
            id: 'portal-favorites',
            label: localeCode === 'en_US' ? 'Favorites' : '收藏',
            icon: <StarOutlined />,
            path: '/portal?group=favorites',
            count: favCount,
          },
          {
            id: 'portal-security',
            label: localeCode === 'en_US' ? 'Account Security' : '账户安全',
            icon: <SafetyCertificateOutlined />,
            path: '/portal/security',
          },
        ],
        groups:
          tags.length > 0
            ? [
                {
                  id: 'portal-app-groups',
                  title: localeCode === 'en_US' ? 'Application groups' : '应用分组',
                  items: tags.map((tag) => ({
                    id: `portal-tag:${tag}`,
                    label: tag,
                    icon: resolvePortalTagIcon(tag),
                    path: `/portal?tag=${encodeURIComponent(tag)}`,
                    count: apps.filter((app) => app.tags?.includes(tag)).length,
                  })),
                },
              ]
            : [],
      }
    }

    return activeWorkbenchKey
      ? buildUnifiedWorkbenchMenu({
          workbenchKey: activeWorkbenchKey,
          fullSidebarNav,
          localeCode,
          resolveNodeLabel,
          aiSearchSuffix,
          accountLeaves,
        })
      : { directItems: [], groups: [] }
  }, [
    accountLeaves,
    activeWorkbenchKey,
    aiSearchSuffix,
    fullSidebarNav,
    localeCode,
    portalApps,
    resolveNodeLabel,
  ])

  const canOpenHeya = Boolean(
    snapshot &&
    Array.isArray(snapshot.permissionKeys) &&
    canAccessRoute(getRouteMeta('/heya'), snapshot),
  )
  const hasHomeWorkbench = workbenches.some((wb) => wb.key === 'home')
  const heyaButton = canOpenHeya ? (
    <Tooltip key="heya-assistant" placement="right" title="Heya">
      <button
        type="button"
        aria-label={localeCode === 'en_US' ? 'Open Heya' : '打开 Heya'}
        className="soha-dual-rail__wb-btn soha-icon-trigger"
        onClick={() => onNavigate('/heya')}
      >
        <img src="/heya-avatar.svg" width={20} height={20} alt="" />
      </button>
    </Tooltip>
  ) : null

  return (
    <div className={`soha-dual-rail-shell is-${stage}`}>
      <div className="soha-dual-rail__left-col">
        <div className="soha-dual-rail__dock">
          <div className="soha-dual-rail__top">
            <Tooltip
              placement="right"
              title={localeCode === 'zh_CN' ? `${brandTitle} · 返回首页` : `${brandTitle} · Home`}
            >
              <button
                type="button"
                className="soha-dual-rail__brand"
                aria-label={localeCode === 'zh_CN' ? '返回首页' : 'Go to overview'}
                onClick={() => onNavigate(homePath)}
              >
                {logoUrl ? (
                  <img className="soha-dual-rail__logo" src={logoUrl} alt={brandTitle} />
                ) : (
                  <div className="soha-brand-mark">S</div>
                )}
              </button>
            </Tooltip>
          </div>

          <div className="soha-dual-rail__center">
            <div className="soha-dual-rail__wb-list">
              {!hasHomeWorkbench ? heyaButton : null}
              {workbenches.map((wb) => {
                const isActive = activeWorkbenchKey === wb.key
                return (
                  <Fragment key={wb.key}>
                    <Tooltip placement="right" title={wb.label}>
                      <button
                        type="button"
                        aria-label={wb.label}
                        className={`soha-dual-rail__wb-btn soha-icon-trigger${
                          isActive ? ' is-active' : ''
                        }`}
                        onClick={() => {
                          if (wb.key === 'account') {
                            onNavigate('/account/profile')
                            return
                          }
                          const targetPath = findFirstAccessiblePathForWorkbench(wb.key, snapshot)
                          if (targetPath) onNavigate(targetPath)
                        }}
                      >
                        {wb.icon}
                      </button>
                    </Tooltip>
                    {wb.key === 'home' ? heyaButton : null}
                  </Fragment>
                )
              })}
            </div>
          </div>

          <div className="soha-dual-rail__bottom-spacer" aria-hidden="true" />
        </div>

        {userPod ? <div className="soha-dual-rail__user-pod">{userPod}</div> : null}
      </div>

      {stage === 'expanded' ? (
        <div className="soha-dual-rail__subnav">
          <div className="soha-dual-rail__subhead">
            <div className="soha-dual-rail__subhead-row">
              <strong>{activeWorkbenchLabel}</strong>
            </div>
            {quickJumpTrigger}
          </div>
          <div className="soha-dual-rail__subbody">
            <DualRailGroupedMenu
              model={activeModel}
              selectedIds={selectedIds}
              currentPathname={currentPathname}
              currentSearch={currentSearch}
              onNavigate={onNavigate}
            />
          </div>
        </div>
      ) : (
        <div
          className="soha-dual-rail__subnav is-narrow"
          aria-label={
            localeCode === 'zh_CN'
              ? `${activeWorkbenchLabel}收窄子菜单`
              : `${activeWorkbenchLabel} narrowed menu`
          }
        >
          <div className="soha-dual-rail__narrow-head">
            <Tooltip
              placement="right"
              title={
                localeCode === 'zh_CN'
                  ? `${activeWorkbenchLabel} · 搜索与跳转 (⌘K)`
                  : `${activeWorkbenchLabel} · Quick jump (⌘K)`
              }
            >
              <button
                type="button"
                aria-label={localeCode === 'zh_CN' ? '子菜单快速跳转' : 'Sub-menu quick jump'}
                className="soha-rail-narrow-btn is-search soha-icon-trigger"
                onClick={onOpenCommand}
              >
                <AnimatedIcon name="search" />
              </button>
            </Tooltip>
          </div>
          <div className="soha-dual-rail__narrow-body">
            <DualRailNarrowIconMenu
              model={activeModel}
              selectedIds={selectedIds}
              currentPathname={currentPathname}
              currentSearch={currentSearch}
              onNavigate={onNavigate}
            />
          </div>
        </div>
      )}
    </div>
  )
}
