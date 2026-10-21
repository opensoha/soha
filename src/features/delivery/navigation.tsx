import { useEffect, useRef, useState } from 'react'
import { Breadcrumb, Button, Input, Popover, Select } from 'antd'
import { ArrowLeftOutlined, CheckOutlined, PlusOutlined, SwapOutlined } from '@ant-design/icons'
import { ManagementState } from '@/components/management-list'
import { useQuery } from '@tanstack/react-query'
import { Link, useLocation, useMatch, useNavigate } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { usePreferencesStore } from '@/stores/preferences-store'
import { useI18n } from '@/i18n'
import { deliveryQueries } from './queries'
import {
  APPLICATION_SETTINGS_KEYS,
  APPLICATION_WORKSPACE_LABELS,
  applicationWorkspacePath,
  applicationEnvironmentSearch,
} from './applications/workspace-navigation'
import type { ApplicationRuntimeEnvironment } from './types'
import './applications/workspace.css'

function useApplicationNavigation() {
  const match = useMatch('/applications/:applicationId/*')
  const location = useLocation()
  const applicationId = match?.params.applicationId ?? ''
  const search = new URLSearchParams(location.search)
  const workloadMatch = useMatch(
    '/applications/:applicationId/application-environments/:applicationEnvironmentId/workloads/:workloadName',
  )
  const design = location.pathname.endsWith('/workflows/design')
  const environmentId =
    workloadMatch?.params.applicationEnvironmentId ||
    search.get('applicationEnvironmentId') ||
    (design ? search.get('bindingId') : '') ||
    ''
  const workload = workloadMatch?.params.workloadName
  const tab = workload ? 'services' : design ? 'delivery' : search.get('tab') || 'services'
  const detailQuery = useQuery(
    deliveryQueries.applications.detail(applicationId, Boolean(applicationId)),
  )
  return {
    applicationId,
    environmentId,
    workload,
    tab,
    design,
    application: detailQuery.data?.application,
    search,
  }
}

export function ApplicationIdentity() {
  const navigate = useNavigate()
  const { applicationId, application, environmentId, search, design, workload } =
    useApplicationNavigation()
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const [switching, setSwitching] = useState(false)
  const [filter, setFilter] = useState('')
  const trigger = useRef<HTMLButtonElement>(null)
  const visits = useRef(new Map<string, string>())
  const permissions = usePermissionSnapshot()
  const canBrowse = hasPermission(permissions.data?.data, 'delivery.applications.view')
  const applications = useQuery(deliveryQueries.applications.list(switching && canBrowse))
  const runtime = useQuery(deliveryQueries.applications.runtime(applicationId, canBrowse))
  const catalog = useQuery(deliveryQueries.environmentCatalog.list(canBrowse))
  const environments = runtime.data?.environments ?? []
  const selectedEnvironmentId = environmentId || environments[0]?.applicationEnvironmentId
  const userId = useAuthStore((state) => state.user?.userId)
  const visitApplication = usePreferencesStore((state) => state.visitApplication)
  useEffect(() => {
    if (userId && application?.id) visitApplication(userId, application.id)
  }, [userId, application?.id, visitApplication])
  const context = search.toString()
  useEffect(() => {
    visits.current.clear()
  }, [userId])
  useEffect(() => {
    if (application?.id !== applicationId || design) return
    const saved = new URLSearchParams()
    for (const key of ['tab', 'applicationEnvironmentId', 'serviceId', 'serviceTab']) {
      const value = new URLSearchParams(context).get(key)
      if (value) saved.set(key, value)
    }
    visits.current.set(applicationId, saved.toString())
  }, [application?.id, applicationId, context, design])
  const candidates = (applications.data ?? []).filter((app) =>
    [app.name, app.key, app.group, app.ownerTeam].some((value) =>
      value?.toLowerCase().includes(filter.trim().toLowerCase()),
    ),
  )
  return (
    <div className="soha-application-identity">
      <div className="soha-application-identity__switcher">
        <strong>{application?.name || applicationId}</strong>
        {canBrowse ? (
          <Popover
            trigger="click"
            placement="bottomLeft"
            open={switching}
            onOpenChange={(open) => {
              setSwitching(open)
              if (!open) setFilter('')
            }}
            content={
              <section
                className="soha-application-switch-panel"
                aria-label={english ? 'Applications' : '应用切换面板'}
                onKeyDown={(event) => {
                  if (event.key === 'Escape') {
                    setSwitching(false)
                    setFilter('')
                    trigger.current?.focus()
                  }
                }}
              >
                <Input
                  autoFocus
                  aria-label={english ? 'Search applications' : '搜索应用'}
                  placeholder={english ? 'Application or team' : '搜索应用或团队'}
                  value={filter}
                  onChange={(event) => setFilter(event.target.value)}
                  allowClear
                />
                {applications.isError ? (
                  <ManagementState
                    compact
                    kind="error"
                    title={english ? 'Applications unavailable' : '应用列表加载失败'}
                    actions={
                      <Button onClick={() => void applications.refetch()}>
                        {english ? 'Retry' : '重试'}
                      </Button>
                    }
                  />
                ) : applications.isPending ? (
                  <ManagementState compact kind="loading" />
                ) : candidates.length ? (
                  <div className="soha-application-switch-list">
                    {candidates.map((app) => (
                      <button
                        key={app.id}
                        type="button"
                        aria-current={app.id === applicationId ? 'page' : undefined}
                        onClick={() => {
                          setSwitching(false)
                          setFilter('')
                          navigate(
                            `/applications/${encodeURIComponent(app.id)}?${visits.current.get(app.id) || 'tab=services'}`,
                          )
                        }}
                      >
                        <span>
                          <strong>{app.name}</strong>
                          <small>{app.ownerTeam || app.group || app.key}</small>
                        </span>
                        {app.id === applicationId ? (
                          <CheckOutlined
                            aria-label={english ? 'Current application' : '当前应用'}
                          />
                        ) : null}
                      </button>
                    ))}
                  </div>
                ) : (
                  <ManagementState
                    compact
                    kind="empty"
                    title={english ? 'No matching applications' : '没有匹配的应用'}
                  />
                )}
              </section>
            }
          >
            <Button
              ref={trigger}
              aria-label={english ? 'Switch application' : '切换应用'}
              type="text"
              icon={<SwapOutlined />}
              aria-expanded={switching}
            >
              {english ? 'Switch' : '切换应用'}
            </Button>
          </Popover>
        ) : null}
      </div>
      {design || workload ? (
        <>
          <span className="soha-application-context-separator" aria-hidden="true">
            /
          </span>
          <Select
            className="soha-application-identity__environment"
            aria-label={english ? 'Application environment' : '应用环境'}
            variant="borderless"
            value={selectedEnvironmentId}
            placeholder={english ? 'Select environment' : '选择环境'}
            showSearch={{ optionFilterProp: 'label' }}
            loading={runtime.isFetching}
            options={environments.map((environment) => ({
              value: environment.applicationEnvironmentId,
              label: `${environment.environmentName || environment.environmentKey || environment.environmentId}${catalog.data?.find((item) => item.id === environment.environmentId)?.isProduction ? ' PROD' : ''}`,
            }))}
            onChange={(id) => {
              const next = applicationEnvironmentSearch(search, id)
              // A designer is bound to one workflow; changing environment returns to its services.
              if (design) {
                next.set('tab', 'services')
                next.delete('bindingId')
                next.delete('templateId')
                next.delete('source')
                next.delete('name')
              }
              navigate(`/applications/${encodeURIComponent(applicationId)}?${next}`)
            }}
            notFoundContent={
              runtime.isError ? (english ? 'Environments unavailable' : '环境加载失败') : undefined
            }
          />
        </>
      ) : null}
    </div>
  )
}

export function ApplicationEnvironmentNavigation({
  environments,
  activeId,
  productionIds,
  clusterName,
  onCreate,
}: {
  environments: ApplicationRuntimeEnvironment[]
  activeId: string
  productionIds: string[]
  clusterName?: string
  onCreate?: () => void
}) {
  const { applicationId, search } = useApplicationNavigation()
  const navigate = useNavigate()
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  return (
    <section
      className="soha-application-environments"
      aria-label={english ? 'Application environments' : '应用环境'}
    >
      <strong>{english ? 'Environment' : '应用环境'}</strong>
      <nav aria-label={english ? 'Choose environment' : '选择环境'}>
        {environments.map((environment) => {
          const id = environment.applicationEnvironmentId
          const production = productionIds.includes(environment.environmentId)
          return (
            <button
              className="soha-application-environment"
              key={id}
              type="button"
              aria-current={id === activeId ? 'page' : undefined}
              data-production={production || undefined}
              onClick={() =>
                navigate(
                  `/applications/${encodeURIComponent(applicationId)}?${applicationEnvironmentSearch(search, id)}`,
                )
              }
            >
              <span>
                {environment.environmentName ||
                  environment.environmentKey ||
                  environment.environmentId}
              </span>
              {production ? (
                <small>
                  {environment.requiresApproval
                    ? english
                      ? 'Production · approval required'
                      : '生产 · 发布需审批'
                    : english
                      ? 'Production'
                      : '生产环境'}
                </small>
              ) : null}
              {id === activeId ? <small>{english ? 'Current' : '当前环境'}</small> : null}
            </button>
          )
        })}
        {onCreate ? (
          <Button type="text" icon={<PlusOutlined />} onClick={onCreate}>
            {english ? 'Add environment' : '添加环境'}
          </Button>
        ) : null}
      </nav>
      {clusterName ? (
        <span className="soha-application-environments__cluster">{clusterName}</span>
      ) : null}
    </section>
  )
}

export function ApplicationNavigation() {
  const { localeCode } = useI18n()
  return (
    <nav aria-label={localeCode === 'en_US' ? 'Application navigation' : '应用导航'}>
      <Link className="soha-application-back" to="/applications">
        <ArrowLeftOutlined />
        {localeCode === 'en_US' ? 'Applications' : '应用中心'}
      </Link>
    </nav>
  )
}

export function ApplicationBreadcrumb() {
  const { applicationId, environmentId, tab, design, application } = useApplicationNavigation()
  const { localeCode } = useI18n()
  const english = localeCode === 'en_US'
  const appPath = applicationWorkspacePath(applicationId, 'services', environmentId)
  const items = [
    { title: <Link to="/applications">{english ? 'Applications' : '应用中心'}</Link> },
    { title: <Link to={appPath}>{application?.name || applicationId}</Link> },
    ...[
      {
        title: design
          ? english
            ? 'Workflow designer'
            : '工作流设计'
          : APPLICATION_SETTINGS_KEYS.includes(tab)
            ? english
              ? 'Settings'
              : '设置'
            : APPLICATION_WORKSPACE_LABELS[tab as keyof typeof APPLICATION_WORKSPACE_LABELS] ||
              (english ? 'Overview' : '概览'),
      },
    ],
  ]
  return <Breadcrumb items={items} />
}
