import { lazy, Suspense, useId, useMemo } from 'react'
import './catalog.css'
import { App, Button, Card, Dropdown, Input, Modal, Segmented, Select, Typography } from 'antd'
import {
  AppstoreOutlined,
  ArrowRightOutlined,
  ClusterOutlined,
  DeleteOutlined,
  EditOutlined,
  ImportOutlined,
  MoreOutlined,
  PlusOutlined,
  SearchOutlined,
  StarFilled,
  StarOutlined,
  UnorderedListOutlined,
} from '@ant-design/icons'
import { useQueries } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementState,
} from '@/components/management-list'
import { scrollableModalBodyStyle, viewportModalStyle } from '@/components/modal-styles'
import { BooleanTag, MetadataTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import type { ApplicationListFilters } from '@/stores/preferences-store'
import { readableQueryData } from '@/utils/query-feedback'
import {
  ApplicationCenterModals,
  ApplicationForm,
  splitApplicationGroups,
  useApplicationCenterState,
} from '../application-center-model'
import { deliveryQueries } from '../queries'
import type { ApplicationServiceKind, DeliveryApplication } from '../types'

const { Text } = Typography
const DEFAULT_FILTERS: ApplicationListFilters = { group: 'all', search: '', scope: 'all' }
const SERVICE_KIND_LABELS: Record<ApplicationServiceKind, string> = {
  kubernetes_workload: 'Kubernetes 工作负载',
  helm_release: 'Helm Release',
  external_service: '外部服务',
  job: '批处理任务',
}
const ImportKubernetesServicesModal = lazy(async () => {
  const module = await import('../environments/import-kubernetes-services-modal')
  return { default: module.ImportKubernetesServicesModal }
})

export function ApplicationsPage() {
  const { modal } = App.useApp()
  const catalogId = useId()
  const managementState = useApplicationCenterState({
    loadWorkflowTemplates: false,
    loadClusters: false,
  })
  const userId = useAuthStore((state) => state.user?.userId ?? '')
  const shortcuts = usePreferencesStore((state) => state.applicationShortcuts[userId])
  const toggleFavorite = usePreferencesStore((state) => state.toggleFavoriteApplication)
  const favorites = shortcuts?.favorites ?? []
  const [searchParams, setSearchParams] = useSearchParams()
  const filters =
    usePreferencesStore((state) => state.applicationListFilters[userId]) ?? DEFAULT_FILTERS
  const setFilters = usePreferencesStore((state) => state.setApplicationListFilters)
  const entryOpen = searchParams.get('action') === 'create'
  const permissions = readableQueryData(usePermissionSnapshot())?.data
  const canViewServices = hasPermission(permissions, 'delivery.application-services.view')
  const canCreateService = hasPermission(permissions, 'delivery.application-services.create')
  const canMapExistingService =
    canViewServices && hasPermission(permissions, 'delivery.application-services.update')
  const canImport =
    (canCreateService || canMapExistingService) &&
    hasPermission(permissions, 'delivery.application-environments.view') &&
    (managementState.canCreateBinding || managementState.canUpdateBinding)
  const importOpen = searchParams.get('action') === 'import'

  const applicationsQuery = managementState.applicationsQuery
  const applications = readableQueryData(applicationsQuery)
  const keyword = filters.search?.trim().toLowerCase() ?? ''
  const hasFilters = Boolean(keyword || (filters.group && filters.group !== 'all'))

  const scopedRows = useMemo(() => {
    const rows = (applications ?? []).filter((app) => {
      const groups = splitApplicationGroups(app.group)
      const matchesGroup =
        !filters.group ||
        filters.group === 'all' ||
        (filters.group === 'unassigned' ? groups.length === 0 : groups.includes(filters.group))
      const matchesScope =
        filters.scope === 'favorites'
          ? shortcuts?.favorites.includes(app.id)
          : filters.scope === 'recent'
            ? shortcuts?.recent.includes(app.id)
            : true
      return matchesGroup && matchesScope
    })
    return filters.scope === 'recent'
      ? rows.sort(
          (a, b) => (shortcuts?.recent.indexOf(a.id) ?? 0) - (shortcuts?.recent.indexOf(b.id) ?? 0),
        )
      : rows
  }, [applications, filters.group, filters.scope, shortcuts])

  const servicesQueries = useQueries({
    queries: canViewServices
      ? scopedRows.map((app) => deliveryQueries.applications.services(app.id))
      : [],
  })
  const visibleRows = scopedRows.flatMap((app, index) => {
    const serviceQuery = servicesQueries[index]
    const services = serviceQuery ? readableQueryData(serviceQuery) : undefined
    const kinds = [...new Set(services?.map((service) => service.serviceKind))]
    const groups = splitApplicationGroups(app.group)
    const searchValues = [
      app.name,
      app.key,
      app.description ?? '',
      ...groups,
      ...kinds.flatMap((kind) => [kind, SERVICE_KIND_LABELS[kind] ?? kind]),
    ]
    return searchValues.some((value) => value.toLowerCase().includes(keyword))
      ? [{ app, groups, kinds, services, serviceQuery }]
      : []
  })
  const searchingServiceTypes = Boolean(keyword && servicesQueries.some((query) => query.isPending))
  const partialServiceSearch = Boolean(
    keyword && servicesQueries.some((query) => query.isError && !readableQueryData(query)),
  )

  const openCreateApplication = () => {
    const next = new URLSearchParams(searchParams)
    next.set('action', 'create')
    next.delete('mode')
    next.delete('templateId')
    setSearchParams(next, { replace: true })
  }

  const closeApplicationEntry = () => {
    const next = new URLSearchParams(searchParams)
    next.delete('action')
    next.delete('mode')
    next.delete('templateId')
    setSearchParams(next, { replace: true })
  }

  const openEditApplication = (app: DeliveryApplication) => {
    managementState.setEditingApp(app)
    managementState.setBuildSources(app.buildSources ?? [])
    managementState.setAppModalVisible(true)
  }

  return (
    <>
      <div className="soha-page">
        <section
          className="soha-application-center-results"
          aria-labelledby={`${catalogId}-heading`}
        >
          <h1 className="soha-application-center-heading" id={`${catalogId}-heading`}>
            应用中心
          </h1>
          <div className="soha-application-center-panel">
            <div className="soha-application-center-toolbar">
              <Segmented<NonNullable<ApplicationListFilters['scope']>>
                aria-label="应用列表范围"
                name={`${catalogId}-scope`}
                value={filters.scope ?? 'all'}
                options={[
                  { value: 'all', label: '全部应用' },
                  { value: 'favorites', label: '我的收藏' },
                  { value: 'recent', label: '最近访问' },
                ]}
                onChange={(scope) => setFilters(userId, { ...filters, scope })}
              />
              <div className="soha-application-center-controls">
                <div className="soha-application-center-filters">
                  <Input
                    aria-label="搜索应用或服务类型"
                    placeholder="搜索应用或服务类型"
                    prefix={<SearchOutlined />}
                    allowClear
                    value={filters.search ?? ''}
                    onChange={(event) =>
                      setFilters(userId, { ...filters, search: event.target.value })
                    }
                  />
                  <Select
                    aria-label="应用分组"
                    value={filters.group ?? 'all'}
                    showSearch={{ optionFilterProp: 'label' }}
                    options={[
                      { label: '全部分组', value: 'all' },
                      { label: '未分组', value: 'unassigned' },
                      ...managementState.applicationGroupOptions.map((group) => ({
                        label: group,
                        value: group,
                      })),
                    ]}
                    onChange={(group) => setFilters(userId, { ...filters, group })}
                  />
                  <Segmented<NonNullable<ApplicationListFilters['view']>>
                    aria-label="应用显示方式"
                    name={`${catalogId}-view`}
                    value={filters.view ?? 'cards'}
                    options={[
                      {
                        value: 'cards',
                        title: '卡片',
                        label: (
                          <>
                            <AppstoreOutlined aria-hidden="true" />
                            <span className="soha-application-center-sr-only">卡片</span>
                          </>
                        ),
                      },
                      {
                        value: 'list',
                        title: '列表',
                        label: (
                          <>
                            <UnorderedListOutlined aria-hidden="true" />
                            <span className="soha-application-center-sr-only">列表</span>
                          </>
                        ),
                      },
                    ]}
                    onChange={(view) => setFilters(userId, { ...filters, view })}
                  />
                </div>
                <div className="soha-application-center-toolbar__actions">
                  {canImport ? (
                    <Button
                      icon={<ImportOutlined />}
                      onClick={() => {
                        const next = new URLSearchParams(searchParams)
                        next.set('action', 'import')
                        setSearchParams(next, { replace: true })
                      }}
                    >
                      接入已有资源
                    </Button>
                  ) : null}
                  {managementState.canCreateApplication ? (
                    <Button type="primary" icon={<PlusOutlined />} onClick={openCreateApplication}>
                      创建应用
                    </Button>
                  ) : null}
                  <ManagementRefreshButton
                    tooltip="刷新"
                    aria-label="刷新应用"
                    loading={
                      applicationsQuery.isFetching ||
                      servicesQueries.some((query) => query.isRefetching)
                    }
                    onClick={() => {
                      void applicationsQuery.refetch()
                      for (const query of servicesQueries) void query.refetch()
                    }}
                  />
                </div>
              </div>
            </div>

            {searchingServiceTypes || partialServiceSearch ? (
              <Text type="secondary" role="status">
                {searchingServiceTypes
                  ? '正在匹配服务类型…'
                  : '部分服务类型暂不可搜索，可刷新后重试。'}
              </Text>
            ) : null}
            {applicationsQuery.isLoading ? (
              <ManagementState compact bordered={false} kind="loading" title="正在加载应用" />
            ) : applicationsQuery.isError ? (
              <ManagementState
                bordered={false}
                compact
                kind="error"
                title="应用加载失败"
                description="暂时无法读取应用，请重试。"
                actions={
                  <Button
                    aria-label="重试"
                    size="small"
                    onClick={() => void applicationsQuery.refetch()}
                  >
                    重试
                  </Button>
                }
              />
            ) : searchingServiceTypes && visibleRows.length === 0 ? (
              <ManagementState compact bordered={false} kind="loading" title="正在匹配应用" />
            ) : visibleRows.length === 0 ? (
              <ManagementState
                bordered={false}
                compact
                title={
                  hasFilters
                    ? '没有匹配的应用'
                    : filters.scope === 'favorites'
                      ? '还没有收藏的应用'
                      : filters.scope === 'recent'
                        ? '还没有最近访问的应用'
                        : '暂无应用'
                }
                description={
                  hasFilters
                    ? '请调整搜索关键词或分组。'
                    : filters.scope === 'favorites'
                      ? '点击应用卡片上的星标，方便下次快速进入。'
                      : filters.scope === 'recent'
                        ? '打开应用后，它会出现在这里。最近访问仅保存在当前浏览器。'
                        : managementState.canCreateApplication
                          ? '点击右上角“创建应用”，开始管理应用。'
                          : '当前还没有可查看的应用。'
                }
                actions={
                  hasFilters || (filters.scope && filters.scope !== 'all') ? (
                    <Button onClick={() => setFilters(userId, { ...filters, ...DEFAULT_FILTERS })}>
                      查看全部应用
                    </Button>
                  ) : undefined
                }
              />
            ) : (
              <div
                className={
                  filters.view === 'list'
                    ? 'soha-application-card-list'
                    : 'soha-application-card-grid'
                }
                role="list"
              >
                {visibleRows.map(({ app, groups, kinds, services, serviceQuery }) => {
                  const applicationPath = `/applications/${encodeURIComponent(app.id)}`
                  const isFavorite = favorites.includes(app.id)
                  return (
                    <Card
                      className="soha-application-card"
                      key={app.id}
                      role="listitem"
                      size="small"
                    >
                      <div className="soha-application-card__header">
                        <Link className="soha-application-card__link" to={applicationPath}>
                          <span className="soha-application-card__icon" aria-hidden="true">
                            <ClusterOutlined />
                          </span>
                          <span className="soha-application-card__identity">
                            <Text strong ellipsis title={app.name}>
                              {app.name}
                            </Text>
                            <Text type="secondary" ellipsis title={app.description}>
                              {app.description || '暂无备注'}
                            </Text>
                          </span>
                        </Link>
                        {userId ? (
                          <ManagementIconButton
                            className="soha-application-card__favorite"
                            aria-label={`${isFavorite ? '取消收藏' : '收藏'} ${app.name}`}
                            aria-pressed={isFavorite}
                            icon={isFavorite ? <StarFilled /> : <StarOutlined />}
                            tooltip={isFavorite ? '取消收藏' : '收藏到当前浏览器'}
                            onClick={() => toggleFavorite(userId, app.id)}
                          />
                        ) : null}
                        {managementState.canUpdateApplication ||
                        managementState.canDeleteApplication ? (
                          <Dropdown
                            trigger={['click']}
                            menu={{
                              items: [
                                ...(managementState.canUpdateApplication
                                  ? [{ key: 'edit', label: '编辑', icon: <EditOutlined /> }]
                                  : []),
                                ...(managementState.canDeleteApplication
                                  ? [
                                      {
                                        key: 'delete',
                                        label: '删除',
                                        danger: true,
                                        icon: <DeleteOutlined />,
                                      },
                                    ]
                                  : []),
                              ],
                              onClick: ({ key }) => {
                                if (key === 'edit') {
                                  openEditApplication(app)
                                  return
                                }
                                modal.confirm({
                                  title: '确认删除应用？',
                                  content: `将删除 ${app.name} 及其环境、部署配置和历史记录，且不可恢复。集群中的实际工作负载不会卸载。`,
                                  okText: '删除',
                                  cancelText: '取消',
                                  okButtonProps: { danger: true },
                                  onOk: () => managementState.deleteAppMutation.mutateAsync(app.id),
                                })
                              },
                            }}
                          >
                            <ManagementIconButton
                              className="soha-application-card__more"
                              aria-label={`管理 ${app.name}`}
                              icon={<MoreOutlined />}
                              tooltip="更多操作"
                            />
                          </Dropdown>
                        ) : null}
                      </div>
                      <div className="soha-application-card__status">
                        <BooleanTag value={app.enabled} trueLabel="已启用" falseLabel="已停用" />
                        {serviceQuery ? (
                          <Text type="secondary">
                            {services
                              ? `${services.length} 个服务`
                              : serviceQuery.isPending
                                ? '正在读取服务'
                                : '服务档案不可用'}
                          </Text>
                        ) : null}
                      </div>
                      <div className="soha-application-card__metadata">
                        {kinds.map((kind) => (
                          <MetadataTag key={kind} label={SERVICE_KIND_LABELS[kind] ?? kind} />
                        ))}
                        <Text type="secondary" ellipsis title={groups.join(' / ') || '未分组'}>
                          {groups.length > 1
                            ? `${groups[0]} +${groups.length - 1}`
                            : groups[0] || '未分组'}
                        </Text>
                      </div>
                      <div className="soha-application-card__footer">
                        <Link to={applicationPath} aria-label={`查看应用 ${app.name}`}>
                          查看应用 <ArrowRightOutlined />
                        </Link>
                      </div>
                    </Card>
                  )
                })}
              </div>
            )}
          </div>
        </section>
      </div>
      <ApplicationCenterModals state={managementState} />
      {importOpen ? (
        <Suspense fallback={<ManagementState compact kind="loading" />}>
          {canImport ? (
            <ImportKubernetesServicesModal
              open
              applications={applications ?? []}
              bindings={managementState.bindingsQuery.data ?? []}
              canCreateApplication={managementState.canCreateApplication}
              canCreateService={canCreateService}
              canMapExistingService={canMapExistingService}
              onClose={closeApplicationEntry}
            />
          ) : (
            <ManagementState
              compact
              kind="no-permission"
              title="无权接入资源"
              actions={<Button onClick={closeApplicationEntry}>返回应用中心</Button>}
            />
          )}
        </Suspense>
      ) : null}
      <Modal
        title="创建应用"
        open={entryOpen}
        onCancel={closeApplicationEntry}
        footer={null}
        destroyOnHidden
        width={560}
        style={viewportModalStyle}
        styles={{ body: scrollableModalBodyStyle }}
      >
        {managementState.canCreateApplication ? (
          <ApplicationForm
            application={null}
            state={managementState}
            onCancel={closeApplicationEntry}
            onCreated={(application) => {
              closeApplicationEntry()
              managementState.navigate(`/applications/${encodeURIComponent(application.id)}`)
            }}
          />
        ) : (
          <ManagementState compact kind="no-permission" title="无权创建应用" />
        )}
      </Modal>
    </>
  )
}
