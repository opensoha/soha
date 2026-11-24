import { useState, type ComponentProps } from 'react'
import { Button, Card, Drawer, Popconfirm, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ContextFeedback } from '@/components/context-feedback'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { StatusTag } from '@/components/status-tag'
import { isApiError } from '@/services/api-error'
import type {
  EnvironmentServiceReferenceCandidateDeleteInput,
  EnvironmentServiceReferenceCandidateInput,
  EnvironmentServiceReferenceDeleteInput,
  EnvironmentServiceReferenceInput,
  EnvironmentServiceReferencePreview,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { deliveryKeys } from '../keys'
import { deliveryQueries } from '../queries'
import type { ApplicationEnvironment, DeliveryEnvironment, ReleaseTarget } from '../types'
import { environmentReferenceKey, serviceReferenceApi } from './service-reference-api'

type Write =
  | { kind: 'save'; input: EnvironmentServiceReferenceCandidateInput }
  | { kind: 'activate'; input: EnvironmentServiceReferenceInput }
  | { kind: 'clear'; input: EnvironmentServiceReferenceCandidateDeleteInput }
  | { kind: 'detach'; input: EnvironmentServiceReferenceDeleteInput }

function referenceTargetServiceId(target: ReleaseTarget) {
  const legacyId = target.metadata?.serviceId
  return target.serviceId?.trim() || (typeof legacyId === 'string' ? legacyId.trim() : '')
}

function compatibleEnvironment(consumer: DeliveryEnvironment, source: DeliveryEnvironment) {
  const tier = consumer.tier
  return (
    consumer.enabled &&
    source.enabled &&
    Boolean(tier?.trim()) &&
    consumer.stageLevel > 0 &&
    tier === source.tier &&
    consumer.stageLevel === source.stageLevel &&
    consumer.isProduction === source.isProduction &&
    (!['dev', 'test', 'pre'].includes(tier ?? '') || !consumer.isProduction) &&
    (!['prod', 'local-prod'].includes(tier ?? '') || consumer.isProduction) &&
    (!consumer.isProduction || (consumer.requiresApproval && source.requiresApproval))
  )
}

function SourceTargets({ preview }: { preview: EnvironmentServiceReferencePreview }) {
  return (
    <ul style={{ marginBlock: 0, paddingInlineStart: 20 }}>
      {preview.targets.map((target) => (
        <li key={target.targetId} style={{ overflowWrap: 'anywhere' }}>
          <strong>
            {target.workloadKind} / {target.workloadName}
          </strong>
          <div>
            {target.clusterId} / {target.namespace}
          </div>
          <Typography.Text type="secondary">
            {target.ownershipMode === 'managed' ? 'Soha 管理' : '只读观察'}
            {target.helmRevision ? ` · Helm 版本 ${target.helmRevision}` : ''}
            {' · '}
            {target.resources.length} 项资源身份已核对
          </Typography.Text>
        </li>
      ))}
    </ul>
  )
}

export function ServiceReferenceEntry(
  props: Omit<ComponentProps<typeof ServiceReferenceDrawer>, 'onClose'>,
) {
  const [open, setOpen] = useState(false)
  return (
    <>
      <Button onClick={() => setOpen(true)}>配置环境目标</Button>
      {open ? (
        <ServiceReferenceDrawer
          {...props}
          onClose={() => setOpen(false)}
          onConfigureLocal={(binding) => {
            setOpen(false)
            props.onConfigureLocal(binding)
          }}
          onOpenSource={(bindingId) => {
            setOpen(false)
            props.onOpenSource(bindingId)
          }}
        />
      ) : null}
    </>
  )
}

function matchedDetail<T extends { id: string; revision: number }>(
  detail: T | undefined,
  marker: { id: string; revision: number } | null | undefined,
) {
  return detail && marker && detail.id === marker.id && detail.revision === marker.revision
    ? detail
    : undefined
}

function ServiceReferenceDrawer({
  applicationId,
  bindingId,
  serviceId,
  serviceName,
  canEditReference,
  onClose,
  onConfigureLocal,
  onOpenSource,
}: {
  applicationId: string
  bindingId: string
  serviceId: string
  serviceName: string
  canEditReference: boolean
  onClose: () => void
  onConfigureLocal: (binding: ApplicationEnvironment) => void
  onOpenSource: (bindingId: string) => void
}) {
  const queryClient = useQueryClient()
  const [sourceId, setSourceId] = useState<string>()
  const slotKey = [...deliveryKeys.environments.all, 'service-reference', bindingId, serviceId]
  const bindingsKey = [...deliveryKeys.environments.all, 'reference-bindings', applicationId]
  const bindingsQuery = useQuery({
    queryKey: bindingsKey,
    queryFn: () => serviceReferenceApi.bindings(applicationId),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  const catalogQuery = useQuery(deliveryQueries.environmentCatalog.list(canEditReference))
  const markersQuery = useQuery({
    queryKey: [...slotKey, 'identities'],
    queryFn: async () => {
      const [candidate, active] = await Promise.all([
        serviceReferenceApi.findCandidate(bindingId, serviceId),
        serviceReferenceApi.findActive(bindingId, serviceId),
      ])
      return { candidate, active }
    },
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  const bindings = bindingsQuery.isError ? undefined : bindingsQuery.data
  const markers = markersQuery.isError ? undefined : markersQuery.data
  const consumer = bindings?.find((binding) => binding.id === bindingId)
  const localTargets = (consumer?.targets ?? []).filter(
    (target) => referenceTargetServiceId(target) === serviceId,
  )
  const candidateQuery = useQuery({
    queryKey: [...slotKey, 'candidate', markers?.candidate?.id, markers?.candidate?.revision],
    queryFn: () => serviceReferenceApi.candidate(bindingId, serviceId),
    enabled: canEditReference && Boolean(markers?.candidate),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  const activeQuery = useQuery({
    queryKey: [...slotKey, 'active', markers?.active?.id, markers?.active?.revision],
    queryFn: () => serviceReferenceApi.active(bindingId, serviceId),
    enabled: Boolean(markers?.active),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })
  const catalog = catalogQuery.isError ? undefined : catalogQuery.data
  const currentEnvironment = catalog?.find((entry) => entry.id === consumer?.environmentId)
  const sources = (bindings ?? []).flatMap((binding) => {
    if (!currentEnvironment || binding.id === bindingId) return []
    const environment = catalog?.find((entry) => entry.id === binding.environmentId)
    if (!environment || !compatibleEnvironment(currentEnvironment, environment)) return []
    const targets = (binding.targets ?? []).filter(
      (target) => referenceTargetServiceId(target) === serviceId,
    )
    if (
      !targets.length ||
      targets.length > 100 ||
      targets.some(
        (target) =>
          target.applicationEnvironmentId !== binding.id ||
          (target.metadata?.ownershipMode !== 'observe_only' &&
            (target.metadata?.ownershipMode !== 'managed' || !target.enabled)),
      )
    )
      return []
    return [{ binding, label: binding.alias || environment.name || environment.key }]
  })
  const source = sources.find((entry) => entry.binding.id === sourceId)?.binding
  const request =
    consumer && source
      ? {
          serviceId,
          sourceApplicationEnvironmentId: source.id,
          expectedConsumerUpdatedAt: consumer.updatedAt,
          expectedSourceUpdatedAt: source.updatedAt,
        }
      : undefined
  const previewQuery = useQuery({
    queryKey: [...slotKey, 'preview', request],
    queryFn: ({ signal }) => serviceReferenceApi.preview(bindingId, request!, signal),
    enabled: Boolean(canEditReference && request),
    gcTime: 0,
    staleTime: 0,
    retry: false,
  })
  const preview = previewQuery.isError ? undefined : previewQuery.data
  const candidateMarker = markers?.candidate
  const activeMarker = markers?.active
  const candidate = matchedDetail(
    candidateQuery.isError ? undefined : candidateQuery.data,
    candidateMarker,
  )
  const active = matchedDetail(activeQuery.isError ? undefined : activeQuery.data, activeMarker)
  const refreshSlot = () =>
    queryClient.invalidateQueries({
      queryKey: slotKey,
      predicate: (query) => query.queryKey[slotKey.length] !== 'preview',
    })

  const write = useOperationMutation(
    {
      retry: false,
      mutationFn: async (operation: Write) => {
        switch (operation.kind) {
          case 'save':
            await serviceReferenceApi.saveCandidate(bindingId, serviceId, operation.input)
            break
          case 'activate':
            await serviceReferenceApi.activate(bindingId, serviceId, operation.input)
            break
          case 'clear':
            await serviceReferenceApi.clearCandidate(bindingId, serviceId, operation.input)
            break
          case 'detach':
            await serviceReferenceApi.detach(bindingId, serviceId, operation.input)
            break
        }
      },
      onSettled: async (_, _error, operation) => {
        setSourceId(undefined)
        await Promise.all([
          refreshSlot(),
          queryClient.invalidateQueries({ queryKey: bindingsKey }),
          ...(operation.kind === 'activate' || operation.kind === 'detach'
            ? [
                queryClient.invalidateQueries({
                  queryKey: deliveryKeys.applications.detail(applicationId),
                }),
                queryClient.invalidateQueries({ queryKey: deliveryKeys.plans.referenceImpacts }),
                queryClient.invalidateQueries({ queryKey: environmentReferenceKey(bindingId) }),
              ]
            : []),
        ])
      },
    },
    {
      label: ['配置环境目标', 'Configure environment target'],
      target: () => serviceName,
      receipt: (_, operation) => ({
        message: {
          save: '已保存引用候选',
          activate: '已激活跨环境引用',
          clear: '已清理引用候选',
          detach: '已解除跨环境引用',
        }[operation.kind],
      }),
    },
  )
  const writeReady =
    Boolean(consumer && markers) &&
    !bindingsQuery.isFetching &&
    !markersQuery.isFetching &&
    !write.blocked
  const canActivate = Boolean(
    canEditReference &&
    writeReady &&
    candidate &&
    candidate.status === 'current' &&
    candidate.preview.consumer.updatedAt === consumer?.updatedAt &&
    !candidateQuery.isFetching &&
    !localTargets.length &&
    !candidate.preview.currentLocalTargetIds.length,
  )
  const refresh = () => {
    setSourceId(undefined)
    void Promise.all([
      bindingsQuery.refetch(),
      refreshSlot(),
      ...(canEditReference ? [catalogQuery.refetch()] : []),
    ])
  }
  const save = () => {
    if (!canEditReference || !writeReady || !preview || !markers || previewQuery.isFetching) return
    const input: EnvironmentServiceReferenceCandidateInput = {
      sourceApplicationEnvironmentId: preview.source.applicationEnvironmentId,
      expectedConsumerUpdatedAt: preview.consumer.updatedAt,
      expectedSourceUpdatedAt: preview.source.updatedAt,
      sourceTargetIds: preview.targets.map((target) => target.targetId),
      expectedSourceRevision: preview.sourceRevision,
      ...(markers.candidate
        ? {
            expectedRevision: markers.candidate.revision,
            expectedCandidateId: markers.candidate.id,
          }
        : { expectedRevision: 0 }),
    }
    write.mutate({ kind: 'save', input })
  }
  const activate = () => {
    if (!canActivate || !candidate || !markers) return
    write.mutate({
      kind: 'activate',
      input: {
        expectedCandidateId: candidate.id,
        expectedCandidateRevision: candidate.revision,
        expectedConsumerUpdatedAt: candidate.preview.consumer.updatedAt,
        expectedSourceUpdatedAt: candidate.preview.source.updatedAt,
        expectedSourceRevision: candidate.currentSourceRevision,
        expectedRevision: markers.active?.revision ?? 0,
        ...(markers.active ? { expectedReferenceId: markers.active.id } : {}),
      },
    })
  }
  const detach = () => {
    if (!writeReady || !consumer || !activeMarker) return
    write.mutate({
      kind: 'detach',
      input: {
        expectedReferenceId: activeMarker.id,
        expectedRevision: activeMarker.revision,
        expectedConsumerUpdatedAt: consumer.updatedAt,
      },
    })
  }
  const clear = () => {
    if (!writeReady || !consumer || !candidateMarker) return
    write.mutate({
      kind: 'clear',
      input: {
        expectedCandidateId: candidateMarker.id,
        expectedRevision: candidateMarker.revision,
        expectedConsumerUpdatedAt: consumer.updatedAt,
      },
    })
  }

  return (
    <Drawer
      open
      size={720}
      title={`配置环境目标 · ${serviceName}`}
      onClose={onClose}
      closable={!write.isPending}
      mask={{ closable: !write.isPending }}
      keyboard={!write.isPending}
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        {write.feedback}
        <ReadFeedback query={bindingsQuery} label="当前环境绑定" />
        <ReadFeedback query={markersQuery} label="当前服务引用状态" />
        {canEditReference ? <ReadFeedback query={catalogQuery} label="环境目录" /> : null}
        {write.outcomeUnknown ? (
          <ContextFeedback
            type="warning"
            showIcon
            title="写入结果待核对"
            description="请关闭后重新打开配置，从最新状态核对操作结果。"
          />
        ) : null}
        {isApiError(write.error) && write.error.status === 409 ? (
          <ContextFeedback
            type="warning"
            showIcon
            title="环境或引用版本已变化"
            description="请刷新当前配置，再重新选择或预检来源。"
          />
        ) : null}
        <Card
          size="small"
          title={consumer?.alias || consumer?.environmentKey || '当前环境'}
          extra={
            <Button
              type="text"
              loading={bindingsQuery.isFetching || markersQuery.isFetching}
              disabled={write.isPending}
              onClick={refresh}
            >
              刷新配置
            </Button>
          }
        >
          <Space orientation="vertical" size={8} style={{ width: '100%' }}>
            {localTargets.length ? (
              <>
                <StatusTag value="enabled" label="本地" />
                <ul style={{ marginBlock: 0, paddingInlineStart: 20 }}>
                  {localTargets.map((target) => (
                    <li key={target.id} style={{ overflowWrap: 'anywhere' }}>
                      {target.workloadKind} / {target.workloadName} · {target.clusterId} /{' '}
                      {target.namespace}
                    </li>
                  ))}
                </ul>
              </>
            ) : markers?.active ? (
              <>
                <StatusTag
                  value={activeQuery.isError ? 'warning' : 'info'}
                  label={active ? `引用自 ${active.preview.source.environmentKey}` : '跨环境引用'}
                />
                {isApiError(activeQuery.error) && [404, 409].includes(activeQuery.error.status) ? (
                  <Typography.Text type="warning">
                    引用来源已失效，请核对来源或解除引用。
                  </Typography.Text>
                ) : null}
                <ReadFeedback query={activeQuery} label="引用来源" />
                {activeQuery.isLoading ? (
                  <Typography.Text>正在核对引用来源…</Typography.Text>
                ) : null}
                {active ? (
                  <>
                    <SourceTargets preview={active.preview} />
                    <Button
                      onClick={() => onOpenSource(active.preview.source.applicationEnvironmentId)}
                    >
                      查看来源环境
                    </Button>
                  </>
                ) : null}
              </>
            ) : (
              <StatusTag
                value="default"
                label={consumer && markers ? '未配置' : '目标状态待核对'}
              />
            )}
            <Space wrap>
              {consumer && markers && !markers.active ? (
                <Button disabled={write.blocked} onClick={() => onConfigureLocal(consumer)}>
                  配置本地目标
                </Button>
              ) : null}
              {consumer && markers?.active ? (
                <Popconfirm
                  key={JSON.stringify([
                    markers.active.id,
                    markers.active.revision,
                    consumer.updatedAt,
                  ])}
                  title="解除当前服务的跨环境引用？"
                  description="解除后，此环境不再使用该引用。"
                  okText="解除引用"
                  onConfirm={detach}
                >
                  <Button danger disabled={!writeReady}>
                    解除引用
                  </Button>
                </Popconfirm>
              ) : null}
            </Space>
          </Space>
        </Card>
        {markers?.candidate ? (
          <Card size="small" title="已保存的引用候选">
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              {canEditReference ? (
                <ReadFeedback query={candidateQuery} label="引用候选来源" />
              ) : null}
              {candidate ? (
                <>
                  <StatusTag
                    value={candidate.status === 'current' ? 'enabled' : 'warning'}
                    label={
                      candidate.status === 'current'
                        ? `来源 ${candidate.preview.source.environmentKey}`
                        : '候选需要重新预检'
                    }
                  />
                  <SourceTargets preview={candidate.preview} />
                </>
              ) : null}
              {localTargets.length ? (
                <Typography.Text type="secondary">
                  请先处理当前服务的本地目标，再激活引用。
                </Typography.Text>
              ) : null}
              <Space wrap>
                {canEditReference ? (
                  <Popconfirm
                    key={JSON.stringify([
                      markers.active,
                      candidate?.id,
                      candidate?.revision,
                      candidate?.currentSourceRevision,
                      candidate?.preview.consumer.updatedAt,
                      candidate?.preview.source.updatedAt,
                    ])}
                    title={markers.active ? '切换该服务的引用来源？' : '激活该服务的跨环境引用？'}
                    okText={markers.active ? '切换引用' : '激活引用'}
                    onConfirm={activate}
                  >
                    <Button type="primary" disabled={!canActivate}>
                      {markers.active ? '切换引用' : '激活引用'}
                    </Button>
                  </Popconfirm>
                ) : null}
                {consumer ? (
                  <Popconfirm
                    key={JSON.stringify([
                      markers.candidate.id,
                      markers.candidate.revision,
                      consumer.updatedAt,
                    ])}
                    title="清理保存的引用候选？"
                    okText="清理候选"
                    onConfirm={clear}
                  >
                    <Button disabled={!writeReady}>清理候选</Button>
                  </Popconfirm>
                ) : null}
              </Space>
            </Space>
          </Card>
        ) : null}
        {canEditReference ? (
          <Card size="small" title="选择引用来源">
            <Space orientation="vertical" size={8} style={{ width: '100%' }}>
              <Select
                aria-label="引用源环境"
                placeholder="选择同等级的来源环境"
                allowClear
                style={{ width: '100%' }}
                loading={catalogQuery.isFetching || bindingsQuery.isFetching}
                disabled={!consumer || write.blocked}
                value={source?.id}
                options={sources.map((entry) => ({ label: entry.label, value: entry.binding.id }))}
                onChange={setSourceId}
                notFoundContent="暂无符合条件的来源环境"
              />
              {request ? <ReadFeedback query={previewQuery} label="来源预检" /> : null}
              {previewQuery.isLoading ? <Typography.Text>正在预检来源…</Typography.Text> : null}
              {preview ? (
                <>
                  <SourceTargets preview={preview} />
                  <Button
                    type="primary"
                    disabled={!writeReady || previewQuery.isFetching}
                    onClick={save}
                  >
                    保存引用候选
                  </Button>
                </>
              ) : null}
            </Space>
          </Card>
        ) : null}
      </Space>
    </Drawer>
  )
}
