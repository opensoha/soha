import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useSearchParams } from 'react-router-dom'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import type { ApplicationEnvironmentRuntimeSummary } from '@opensoha/contracts/gen/ts/sohaapi'
import { deliveryQueries } from '../queries'
import type { ApplicationRuntimeWorkload } from '../types'
import { visibleServiceInventory } from './helm-service-inventory'
import { serviceDeliveryKind } from './service-workflow'
import { SERVICE_KIND_OPTIONS } from './service-presets'
import { environmentRuntimeStatus } from './runtime-status'

export function serviceKindLabel(value?: string) {
  return SERVICE_KIND_OPTIONS.find((item) => item.value === value)?.label ?? value ?? '-'
}

export const OBSERVATION_LABELS: Record<
  ApplicationEnvironmentRuntimeSummary['observationStatus'],
  string
> = {
  available: '运行观测正常',
  partial: '运行状态部分可用',
  timeout: '当前环境运行观测超时',
  cluster_unready: '当前环境集群不可用',
  forbidden: '无运行态查看权限',
  identity_mismatch: '资源身份已变化',
  unconfigured: '当前环境未配置',
  unavailable: '运行状态读取失败',
}

export function useApplicationEntryData(applicationId: string, searchParams: URLSearchParams) {
  const requestedEnvironmentId = searchParams.get('applicationEnvironmentId')?.trim() ?? ''
  const focusedServiceId = searchParams.get('serviceId')?.trim() ?? ''
  const defaultServiceTab = 'pods'
  const requestedServiceTab = searchParams.get('serviceTab') || defaultServiceTab
  const serviceTab = [
    'runtime',
    'pods',
    'metrics',
    'related-resources',
    'build',
    'resources',
    'configuration',
    'events',
  ].includes(requestedServiceTab)
    ? requestedServiceTab
    : defaultServiceTab
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canReadApplication = hasPermission(permissionSnapshot, 'delivery.applications.view')
  const canReadServices =
    canReadApplication && hasPermission(permissionSnapshot, 'delivery.application-services.view')
  const recordOptions = deliveryQueries.applications.record(applicationId)
  const applicationQuery = useQuery({
    ...recordOptions,
    enabled: recordOptions.enabled && canReadApplication,
  })
  const environmentQuery = useQuery(
    deliveryQueries.applications.environmentNavigation(applicationId, canReadApplication),
  )
  const application = canReadApplication ? readableQueryData(applicationQuery) : undefined
  const environmentData = canReadApplication ? readableQueryData(environmentQuery) : undefined
  const environments = environmentData ?? []
  const activeNavigation = requestedEnvironmentId
    ? environments.find((item) => item.applicationEnvironmentId === requestedEnvironmentId)
    : environments[0]
  const activeEnvironmentId = activeNavigation?.applicationEnvironmentId ?? ''
  const runtimeQuery = useQuery(
    deliveryQueries.applications.environmentRuntimeSummary(
      applicationId ?? '',
      activeEnvironmentId,
      Boolean(application),
    ),
  )
  const runtimeData = readableQueryData(runtimeQuery)
  const runtimeSummary =
    runtimeData?.environment.applicationEnvironmentId === activeEnvironmentId
      ? runtimeData
      : undefined
  const observationStatus = queryAccessDenied(runtimeQuery)
    ? 'forbidden'
    : (runtimeSummary?.observationStatus ??
      (runtimeQuery.isError || runtimeData ? 'unavailable' : undefined))
  const activeEnvironment =
    observationStatus === 'available' || observationStatus === 'partial'
      ? runtimeSummary?.environment
      : undefined
  const runtimePending = Boolean(activeEnvironmentId && runtimeQuery.isPending)
  const invalidEnvironment = Boolean(requestedEnvironmentId && environmentData && !activeNavigation)
  const servicesQuery = useQuery(
    deliveryQueries.applications.serviceDirectory(applicationId, canReadServices),
  )
  const serviceData = canReadServices ? readableQueryData(servicesQuery) : undefined
  const services = serviceData ?? []
  const servicesPending = canReadServices && !serviceData && servicesQuery.isPending
  const servicesDenied = !canReadServices || queryAccessDenied(servicesQuery)
  const servicesUnavailable = servicesDenied || (!serviceData && servicesQuery.isError)
  const requestedWorkload = searchParams.get('workload') || ''
  const focusedService = focusedServiceId
    ? services.find((service) => service.id === focusedServiceId)
    : undefined
  const canReadPodSummary =
    !permissionSnapshotQuery.isError && hasPermission(permissionSnapshot, 'platform.pods.view')
  const metricRoot = searchParams.get('metricRoot')
  const verifiedService = Boolean(focusedService && !requestedWorkload)
  const projectPods =
    verifiedService && (serviceTab === 'pods' || (serviceTab === 'metrics' && !metricRoot))
  const servicePodsQuery = useQuery(
    deliveryQueries.workloads.servicePods(
      applicationId ?? '',
      activeEnvironmentId,
      focusedService?.id ?? '',
      Boolean(application) && projectPods && canReadPodSummary,
    ),
  )
  return {
    permissionSnapshotQuery,
    canReadApplication,
    applicationQuery,
    environmentQuery,
    servicesQuery,
    runtimeQuery,
    application,
    environments,
    activeNavigation,
    activeEnvironmentId,
    activeEnvironment,
    environmentData,
    runtimeSummary,
    observationStatus,
    invalidEnvironment,
    runtimePending,
    services,
    serviceData,
    servicesPending,
    servicesUnavailable,
    servicesDenied,
    requestedEnvironmentId,
    focusedServiceId,
    serviceTab,
    requestedWorkload,
    focusedService,
    canReadPodSummary,
    metricRoot,
    verifiedService,
    projectPods,
    servicePodsQuery,
  }
}

export function useApplicationServiceWorkspace(applicationId: string) {
  const [searchParams, setSearchParams] = useSearchParams()
  const [serviceSearch, setServiceSearch] = useState('')
  const [serviceKindFilter, setServiceKindFilter] = useState('all')
  const reads = useApplicationEntryData(applicationId, searchParams)
  const {
    requestedEnvironmentId,
    focusedServiceId,
    serviceTab,
    activeEnvironmentId,
    activeNavigation,
    activeEnvironment,
    services,
    servicesDenied,
    requestedWorkload,
    focusedService,
    projectPods,
    runtimePending,
    environmentQuery,
    observationStatus,
  } = reads
  const setFocusedService = (
    serviceId?: string,
    nextServiceTab = 'pods',
    nextSettings?: string,
  ) => {
    const next = new URLSearchParams(searchParams)
    next.delete('pod')
    next.delete('podUid')
    next.delete('podCluster')
    next.delete('podNamespace')
    next.delete('tool')
    next.delete('container')
    next.delete('workload')
    next.delete('serviceEditor')
    next.delete('metricPod')
    next.delete('metricCluster')
    next.delete('metricNamespace')
    next.delete('metricRange')
    next.delete('metricUid')
    next.delete('metricRoot')
    if (serviceId) {
      next.delete('settings')
      next.set('tab', 'services')
      next.set('serviceId', serviceId)
      next.set('serviceTab', nextServiceTab)
      if (nextSettings) next.set('settings', nextSettings)
      if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
    } else {
      next.set('tab', 'services')
      next.delete('settings')
      next.delete('serviceId')
      next.delete('serviceTab')
    }
    setSearchParams(next)
  }
  useEffect(() => {
    if (requestedEnvironmentId || !activeEnvironmentId) return
    const next = new URLSearchParams(searchParams)
    next.set('applicationEnvironmentId', activeEnvironmentId)
    setSearchParams(next, { replace: true })
  }, [requestedEnvironmentId, activeEnvironmentId, searchParams, setSearchParams])
  const runtimeWorkloads = activeEnvironment?.workloads ?? []
  const importedSourceVisible =
    (['runtime', 'related-resources', 'resources', 'configuration'].includes(serviceTab) ||
      (serviceTab === 'metrics' && !projectPods)) &&
    runtimeWorkloads.some(
      (item) =>
        item.source &&
        (item.workloadName === requestedWorkload ||
          (focusedService &&
            (item.serviceId === focusedService.id || item.serviceKey === focusedService.key))),
    )
  const activeBindingQuery = useQuery(
    deliveryQueries.environments.detail(activeEnvironmentId, importedSourceVisible),
  )
  const activeRuntimeBinding = importedSourceVisible
    ? readableQueryData(activeBindingQuery)?.binding
    : undefined
  const activeRuntimeClusterId =
    activeNavigation?.clusterId ||
    runtimeWorkloads[0]?.clusterId ||
    activeEnvironment?.targets?.[0]?.clusterId ||
    ''
  const serviceOverview = !focusedServiceId && !requestedWorkload
  const orphanWorkloads = !servicesDenied
    ? runtimeWorkloads.filter(
        (workload) =>
          !services.some(
            (service) => service.id === workload.serviceId || service.key === workload.serviceKey,
          ),
      )
    : []
  const selectedWorkloads = requestedWorkload
    ? runtimeWorkloads.filter((workload) => workload.workloadName === requestedWorkload)
    : focusedService
      ? runtimeWorkloads.filter(
          (workload) =>
            workload.serviceId === focusedService.id || workload.serviceKey === focusedService.key,
        )
      : focusedServiceId
        ? []
        : orphanWorkloads.slice(0, 1)
  const serviceManifestDeployments = (serviceId?: string) =>
    (activeEnvironment?.manifestDeployments ?? []).filter(
      (deployment) =>
        serviceId &&
        (activeEnvironment?.targets ?? []).some(
          (target) =>
            target.enabled &&
            target.executorKind === 'manifest_ssa' &&
            target.configRef === deployment.bindingId &&
            target.metadata?.serviceId === serviceId,
        ),
    )
  const serviceStatus = (
    items: ApplicationRuntimeWorkload[],
    serviceId?: string,
  ): { tone: string; label: string; description?: string } => {
    if (runtimePending || environmentQuery.isPending) return { tone: 'default', label: '待观测' }
    if (
      observationStatus &&
      (!activeEnvironment || (observationStatus === 'partial' && !items.length))
    )
      return { tone: 'default', label: OBSERVATION_LABELS[observationStatus] }
    if (!activeEnvironment) return { tone: 'default', label: '未关联环境' }
    for (const [state, label] of [
      ['forbidden', '无资源查看权限'],
      ['replaced', '资源身份已变化'],
      ['missing', '未发现关联资源'],
      ['unavailable', '运行状态读取失败'],
    ]) {
      if (items.some((item) => item.sourceStatus === state))
        return {
          tone: 'default',
          label,
          description:
            state === 'unavailable'
              ? '暂时无法从集群读取运行状态，当前无法判断服务是否正常。请检查集群连接后刷新重试。'
              : undefined,
        }
    }
    return environmentRuntimeStatus({
      ...activeEnvironment,
      workloads: items,
      manifestDeployments: serviceManifestDeployments(serviceId),
    })
  }
  const selectionValue =
    (serviceOverview ? 'all' : focusedService?.id) ||
    (selectedWorkloads[0] ? `workload:${selectedWorkloads[0].workloadName}` : undefined)
  const selectService = (value: string) => {
    if (value === 'all') {
      setFocusedService()
      return
    }
    if (!value.startsWith('workload:')) {
      setFocusedService(value, serviceTab)
      return
    }
    const next = new URLSearchParams(searchParams)
    next.set('tab', 'services')
    next.set('workload', value.slice(9))
    next.set('serviceTab', serviceTab)
    next.delete('serviceId')
    next.delete('pod')
    next.delete('podUid')
    next.delete('podCluster')
    next.delete('podNamespace')
    next.delete('tool')
    next.delete('container')
    for (const key of [
      'metricPod',
      'metricCluster',
      'metricNamespace',
      'metricRange',
      'metricUid',
      'metricRoot',
    ])
      next.delete(key)
    if (activeEnvironmentId) next.set('applicationEnvironmentId', activeEnvironmentId)
    setSearchParams(next)
  }
  const serviceCards = [
    ...services.map((service) => ({
      id: service.id,
      name: service.name || service.key,
      kind:
        serviceDeliveryKind(
          service,
          activeRuntimeBinding?.targets ?? activeEnvironment?.targets ?? [],
          runtimeWorkloads.filter(
            (workload) => workload.serviceId === service.id || workload.serviceKey === service.key,
          ),
        ) || serviceKindLabel(service.serviceKind),
      service,
      workloads: runtimeWorkloads.filter(
        (workload) => workload.serviceId === service.id || workload.serviceKey === service.key,
      ),
    })),
    ...orphanWorkloads.map((workload) => ({
      id: `workload:${workload.workloadName}`,
      name: workload.workloadName,
      kind: workload.workloadKind,
      workloads: [workload],
    })),
  ]
  const currentServiceCard = serviceCards.find((service) => service.id === selectionValue)
  const { visibleCards: visibleServiceCards, nestedServices } = visibleServiceInventory(
    serviceCards,
    activeRuntimeBinding?.targets ?? activeEnvironment?.targets ?? [],
  )
  const filteredServiceCards = visibleServiceCards.filter(
    (service) =>
      (serviceKindFilter === 'all' || service.kind === serviceKindFilter) &&
      [service.name, service.id, service.service?.key ?? ''].some((value) =>
        value.toLowerCase().includes(serviceSearch.trim().toLowerCase()),
      ),
  )
  return {
    ...reads,
    activeBindingQuery,
    activeRuntimeClusterId,
    serviceCards,
    visibleServiceCards,
    filteredServiceCards,
    nestedServices,
    currentServiceCard,
    serviceSearch,
    setServiceSearch,
    serviceKindFilter,
    setServiceKindFilter,
    selectionValue,
    serviceOverview,
    serviceStatus,
    selectService,
    setFocusedService,
    serviceManifestDeployments,
    runtimeWorkloads,
    selectedWorkloads,
    activeRuntimeBinding,
  }
}
