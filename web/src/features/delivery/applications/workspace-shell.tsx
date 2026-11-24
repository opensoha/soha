import { Suspense, useRef, useState, type ComponentType, type RefObject } from 'react'
import { Button, Input, Select, Typography } from 'antd'
import { DeploymentUnitOutlined, PlusOutlined, SettingOutlined } from '@ant-design/icons'
import { useParams, useSearchParams } from 'react-router-dom'
import { useQuery } from '@tanstack/react-query'
import type { EnvironmentServiceReferenceSummary } from '@opensoha/contracts/gen/ts/sohaapi'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { MetadataTag } from '@/components/status-tag'
import { hasPermission } from '@/features/auth/permission-snapshot'
import { localeText, useI18n } from '@/i18n'
import { isApiError } from '@/services/api-error'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { ApplicationEnvironmentNavigation } from '../navigation'
import type { DeliveryApplication } from '../types'
import { environmentReferenceQuery } from './service-reference-api'
import { computeServiceSlots, type ServiceSlot } from './service-slot-projection'
import { OBSERVATION_LABELS, useApplicationServiceWorkspace } from './service-workspace-model'
import './workspace.css'

const { Text } = Typography

const serviceSlotLabels = {
  local: '本地',
  reference: '跨环境引用',
  unconfigured: '未配置',
  unknown: '待核对',
}

export function ServiceSlotBadge({ slot }: { slot?: ServiceSlot }) {
  if (!slot) return null
  return (
    <MetadataTag
      label={serviceSlotLabels[slot.kind]}
      tone={slot.kind === 'local' ? 'blue' : slot.kind === 'reference' ? 'purple' : 'default'}
    />
  )
}

export type ApplicationWorkspaceActions = {
  createService: () => void
  createEnvironment: () => void
  refreshRuntime: () => void
}

export type ApplicationWorkspaceDetailProps = {
  applicationId: string
  application: DeliveryApplication
  workspace: ReturnType<typeof useApplicationServiceWorkspace>
  focusedSlot?: ServiceSlot
  environmentReferences?: EnvironmentServiceReferenceSummary[]
  refreshEnvironmentReferences?: () => void
  actions: RefObject<ApplicationWorkspaceActions | null>
  onActionsReady: (ready: boolean) => void
}

type WorkspaceMountProps = {
  applicationId: string
  Detail: ComponentType<ApplicationWorkspaceDetailProps>
}

function ApplicationWorkspace({ applicationId, Detail }: WorkspaceMountProps) {
  const [searchParams, setSearchParams] = useSearchParams()
  const { localeCode } = useI18n()
  const settingsLabel = localeText(localeCode, '应用设置', 'Application settings')
  const actions = useRef<ApplicationWorkspaceActions>(null)
  const [actionsReady, setActionsReady] = useState(false)
  const workspace = useApplicationServiceWorkspace(applicationId)
  const {
    application,
    applicationQuery,
    environmentData,
    environmentQuery,
    runtimeSummary,
    runtimeQuery,
    observationStatus,
    environments,
    activeEnvironmentId,
    activeRuntimeClusterId,
    focusedServiceId,
    services,
    permissionSnapshotQuery,
    invalidEnvironment,
    visibleServiceCards,
    filteredServiceCards,
    serviceOverview,
    selectService,
    serviceStatus,
    serviceSearch,
    setServiceSearch,
    serviceKindFilter,
    setServiceKindFilter,
    selectionValue,
  } = workspace

  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canReadEnvironmentReferences = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.update',
  )
  const referenceSummaries = useQuery(
    environmentReferenceQuery(
      activeEnvironmentId,
      Boolean(application) && canReadEnvironmentReferences,
    ),
  )
  const environmentReferences = referenceSummaries.isEnabled
    ? readableQueryData(referenceSummaries)
    : undefined
  const { slots: serviceSlots, counts: slotCounts } = computeServiceSlots(
    activeEnvironmentId ? services.map(({ id }) => id) : [],
    runtimeSummary ? (runtimeSummary.environment.targets ?? []) : undefined,
    environmentReferences,
  )

  if (!application && applicationQuery.isPending && workspace.canReadApplication) {
    return (
      <div className="soha-page">
        <ManagementState kind="loading" title="正在加载应用" />
      </div>
    )
  }

  if (!application) {
    const notFound = isApiError(applicationQuery.error) && applicationQuery.error.status === 404
    const noPermission = !workspace.canReadApplication || queryAccessDenied(applicationQuery)
    return (
      <div className="soha-page">
        <ManagementState
          kind={notFound ? 'not-found' : noPermission ? 'no-permission' : 'error'}
          title={notFound ? '应用不存在' : noPermission ? '无应用查看权限' : '应用加载失败'}
          actions={
            notFound || noPermission ? undefined : (
              <Button aria-label="重试" onClick={() => void applicationQuery.refetch()}>
                重试
              </Button>
            )
          }
        />
      </div>
    )
  }

  const canCreateService = hasPermission(permissionSnapshot, 'delivery.application-services.create')
  const canCreateBinding = hasPermission(
    permissionSnapshot,
    'delivery.application-environments.create',
  )
  const productionIds = environments
    .filter((item) => item.isProduction)
    .map((item) => item.environmentId)
  return (
    <div className="soha-page soha-application-workbench">
      <ApplicationEnvironmentNavigation
        environments={environments}
        activeId={activeEnvironmentId}
        productionIds={productionIds}
        clusterName={activeRuntimeClusterId}
        serviceSummary={
          activeEnvironmentId && services.length ? (
            <span aria-label="当前环境服务目标统计">
              本地 {slotCounts.local} · 引用 {slotCounts.reference} · 未配置{' '}
              {slotCounts.unconfigured}
              {slotCounts.unknown ? ` · 待核对 ${slotCounts.unknown}` : ''}
            </span>
          ) : undefined
        }
        onCreate={
          canCreateBinding && actionsReady ? () => actions.current?.createEnvironment() : undefined
        }
      />
      {!environmentData ? (
        <ManagementState
          compact
          bordered={false}
          kind={
            environmentQuery.isPending
              ? 'loading'
              : queryAccessDenied(environmentQuery)
                ? 'no-permission'
                : 'error'
          }
          title={
            environmentQuery.isPending
              ? '正在读取应用环境'
              : queryAccessDenied(environmentQuery)
                ? '无应用环境查看权限'
                : '应用环境加载失败'
          }
          actions={
            environmentQuery.isError && !queryAccessDenied(environmentQuery) ? (
              <Button onClick={() => void environmentQuery.refetch()}>重试读取应用环境</Button>
            ) : undefined
          }
        />
      ) : null}
      {observationStatus && observationStatus !== 'available' ? (
        <ManagementState
          compact
          bordered={false}
          kind={
            observationStatus === 'forbidden'
              ? 'no-permission'
              : observationStatus === 'unconfigured'
                ? 'not-configured'
                : 'error'
          }
          title={OBSERVATION_LABELS[observationStatus]}
          description={
            observationStatus === 'partial' && runtimeSummary
              ? `最近观测 ${formatDateTime(runtimeSummary.observedAt)}`
              : undefined
          }
          actions={
            observationStatus !== 'forbidden' &&
            observationStatus !== 'unconfigured' &&
            runtimeQuery.isEnabled ? (
              <Button
                loading={runtimeQuery.isFetching}
                onClick={() => {
                  if (actions.current) actions.current.refreshRuntime()
                  else {
                    void runtimeQuery.refetch()
                    if (workspace.servicePodsQuery.isEnabled)
                      void workspace.servicePodsQuery.refetch()
                    if (workspace.activeBindingQuery.isEnabled)
                      void workspace.activeBindingQuery.refetch()
                  }
                }}
              >
                重试当前环境
              </Button>
            ) : undefined
          }
        />
      ) : observationStatus === 'available' && runtimeSummary ? (
        <Text type="secondary">最近观测 {formatDateTime(runtimeSummary.observedAt)}</Text>
      ) : null}
      {invalidEnvironment ? (
        <ManagementState
          kind="not-found"
          compact
          title="所选环境不存在或不可访问"
          description="请重新选择环境；不会自动切换到其他环境。"
          actions={
            <Button
              onClick={() => {
                const next = new URLSearchParams(searchParams)
                next.delete('applicationEnvironmentId')
                setSearchParams(next)
              }}
            >
              重新选择环境
            </Button>
          }
        />
      ) : null}
      <section
        aria-label="服务工作台"
        className="soha-service-console"
        data-scroll-view
        data-invalid-environment={invalidEnvironment || undefined}
      >
        {!invalidEnvironment ? (
          <aside className="soha-service-selector">
            <div className="soha-service-selector__heading">
              <strong>
                服务{' '}
                <Text type="secondary">
                  {filteredServiceCards.length} / {visibleServiceCards.length}
                </Text>
              </strong>
              <ManagementIconButton
                icon={<SettingOutlined />}
                aria-label={settingsLabel}
                tooltip={settingsLabel}
                onClick={() => {
                  const next = new URLSearchParams(searchParams)
                  next.set('tab', 'services')
                  next.set('settings', 'application')
                  if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
                  if (focusedServiceId) next.set('serviceId', focusedServiceId)
                  setSearchParams(next)
                }}
              />
            </div>
            <Button
              className="soha-service-selector__overview"
              aria-current={serviceOverview ? 'page' : undefined}
              onClick={() => selectService('all')}
            >
              服务总览
            </Button>
            <Input
              aria-label="搜索服务"
              placeholder="搜索名称或标识"
              value={serviceSearch}
              onChange={(event) => setServiceSearch(event.target.value)}
              allowClear
            />
            <Select
              aria-label="筛选服务类型"
              value={serviceKindFilter}
              options={[
                { value: 'all', label: '全部服务类型' },
                ...Array.from(new Set(visibleServiceCards.map((service) => service.kind))).map(
                  (kind) => ({ value: kind, label: kind }),
                ),
              ]}
              onChange={setServiceKindFilter}
            />
            <nav aria-label="服务列表" className="soha-service-selector__list">
              {filteredServiceCards.map((service) => {
                const status = serviceStatus(service.workloads, service.id)
                return (
                  <button
                    key={service.id}
                    type="button"
                    aria-current={selectionValue === service.id ? 'page' : undefined}
                    onClick={() => selectService(service.id)}
                    title={service.name}
                  >
                    <span className="soha-service-selector__icon" aria-hidden="true">
                      <DeploymentUnitOutlined />
                    </span>
                    <span className="soha-service-selector__identity">
                      <strong>{service.name}</strong>
                      <small>{service.kind}</small>
                      <ServiceSlotBadge slot={serviceSlots.get(service.id)} />
                    </span>
                    <span
                      className={`soha-service-status-dot is-${status.tone}`}
                      role="img"
                      aria-label={status.label}
                      title={status.label}
                    />
                  </button>
                )
              })}
            </nav>
            {!filteredServiceCards.length && visibleServiceCards.length ? (
              <ManagementState compact bordered={false} kind="empty" title="没有匹配的服务" />
            ) : null}
            <Select
              className="soha-service-selector__mobile"
              aria-label="选择服务"
              value={selectionValue}
              showSearch={{ optionFilterProp: 'label' }}
              options={filteredServiceCards
                .map((service) => {
                  const slot = serviceSlots.get(service.id)
                  return {
                    value: service.id,
                    label: slot
                      ? `${service.name} · ${serviceSlotLabels[slot.kind]}`
                      : service.name,
                  }
                })
                .concat([{ value: 'all', label: '服务总览' }])}
              onChange={selectService}
            />
            {canCreateService && actionsReady ? (
              <Button
                block
                icon={<PlusOutlined />}
                onClick={() => actions.current?.createService()}
              >
                添加服务
              </Button>
            ) : null}
          </aside>
        ) : null}
        <div className="soha-service-console__body">
          <Suspense fallback={<ManagementState compact kind="loading" title="正在加载服务详情" />}>
            <Detail
              applicationId={applicationId}
              application={application}
              workspace={workspace}
              focusedSlot={serviceSlots.get(focusedServiceId)}
              environmentReferences={environmentReferences}
              refreshEnvironmentReferences={
                referenceSummaries.isEnabled ? () => void referenceSummaries.refetch() : undefined
              }
              actions={actions}
              onActionsReady={setActionsReady}
            />
          </Suspense>
        </div>
      </section>
    </div>
  )
}

export function ApplicationWorkspaceRoute({
  Detail,
}: {
  Detail: ComponentType<ApplicationWorkspaceDetailProps>
}) {
  const { applicationId = '' } = useParams()
  return <ApplicationWorkspace key={applicationId} applicationId={applicationId} Detail={Detail} />
}
