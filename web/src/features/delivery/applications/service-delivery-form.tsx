import { useRef, useState } from 'react'
import { Alert, App, Button, Card, Descriptions, Space } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { formatDateTime } from '@/utils/time'
import { DeliveryTargetEditor } from '../batches/target-editor'
import { deliveryActionLabels, deliveryStatusLabels } from '../batches/model'
import { deliveryMutations } from '../mutations'
import { deliveryQueries } from '../queries'
import './service-delivery-form.css'
import type { ApplicationServiceComponent, DeliveryBatchInput, DeliveryTargetInput } from '../types'

export function ServiceDeliveryForm({
  service,
  applicationEnvironmentId,
  onClose,
}: {
  service: ApplicationServiceComponent
  applicationEnvironmentId?: string
  onClose: () => void
}) {
  const { message } = App.useApp()
  const permissions = usePermissionSnapshot().data?.data
  const canView = hasPermission(permissions, 'delivery.workflows.view')
  const canTrigger = hasPermission(permissions, 'delivery.workflows.trigger')
  const queryClient = useQueryClient()
  const create = useMutation(deliveryMutations.batches.create(queryClient))
  const [preview, setPreview] = useState<DeliveryTargetInput>()
  const request = useRef<{ signature: string; key: string }>()
  const [currentId, setCurrentId] = useState('')
  const current = useQuery(deliveryQueries.batches.detail(currentId, canView))
  const history = useQuery(
    deliveryQueries.batches.list(
      { applicationId: service.applicationId, serviceId: service.id, limit: 10 },
      { enabled: canView, refetchInterval: 5000 },
    ),
  )
  const detail = useQuery(deliveryQueries.applications.detail(service.applicationId))
  const previewBinding = detail.data?.bindings?.find(
    (binding) => binding.applicationEnvironmentId === preview?.applicationEnvironmentId,
  )
  const start = async () => {
    if (!preview || !canTrigger) return
    const input: Omit<DeliveryBatchInput, 'idempotencyKey'> = {
      definition: {
        name: `${service.name} · ${deliveryActionLabels[preview.action]}`,
        mode: 'service_serial',
        stopOnFailure: true,
        maxConcurrency: 1,
        targets: [preview],
      },
    }
    const signature = JSON.stringify(input)
    if (request.current?.signature !== signature)
      request.current = { signature, key: crypto.randomUUID() }
    try {
      const batch = await create.mutateAsync({ ...input, idempotencyKey: request.current.key })
      setCurrentId(batch.id)
      setPreview(undefined)
      request.current = undefined
      message.success('交付已创建')
    } catch (error) {
      message.error((error as Error).message)
    }
  }
  return (
    <Space
      className="service-delivery-form"
      orientation="vertical"
      size={16}
      style={{ width: '100%' }}
    >
      <Card title="交付配置" size="small">
        {canTrigger ? (
          <DeliveryTargetEditor
            key={`${service.id}/${applicationEnvironmentId ?? ''}`}
            embedded
            fixed
            pending={create.isPending}
            target={{
              id: 'service',
              applicationId: service.applicationId,
              serviceId: service.id,
              applicationEnvironmentId: applicationEnvironmentId || undefined,
              action: service.buildSourceId ? 'build_deploy' : 'config_update',
            }}
            onClose={onClose}
            onChange={() => setPreview(undefined)}
            onSave={(target) => {
              const building = target.action === 'build' || target.action === 'build_deploy'
              if (building && !hasPermission(permissions, 'delivery.builds.trigger')) {
                message.error('缺少构建权限')
                return
              }
              if (
                target.action !== 'build' &&
                !hasPermission(permissions, 'delivery.releases.trigger')
              ) {
                message.error('缺少发布权限')
                return
              }
              setPreview(target)
            }}
          />
        ) : (
          <ManagementState compact kind="no-permission" title="无权发起交付" />
        )}
      </Card>
      {preview ? (
        <Card title="确认交付参数" size="small">
          <Descriptions
            column={1}
            size="small"
            items={[
              { key: 'action', label: '操作', children: deliveryActionLabels[preview.action] },
              { key: 'service', label: '服务', children: service.name },
              {
                key: 'environment',
                label: '环境',
                children:
                  previewBinding?.environmentName ||
                  previewBinding?.environmentKey ||
                  preview.applicationEnvironmentId ||
                  '不指定环境',
              },
              ...(preview.releaseBundleId
                ? [{ key: 'bundle', label: '已有产物', children: preview.releaseBundleId }]
                : []),
              {
                key: 'approval',
                label: '部署审批',
                children:
                  preview.action === 'build'
                    ? '无部署步骤'
                    : previewBinding?.requiresApproval
                      ? '部署前需要审批'
                      : '按环境策略执行预检',
              },
            ]}
          />
          <Space>
            <Button disabled={create.isPending} onClick={() => setPreview(undefined)}>
              返回修改
            </Button>
            <Button type="primary" loading={create.isPending} onClick={() => void start()}>
              确认并开始
            </Button>
          </Space>
        </Card>
      ) : null}
      {current.data ? (
        <Alert
          type={current.data.status === 'failed' ? 'error' : 'info'}
          showIcon
          title={
            <Space>
              本次执行
              <StatusTag
                value={current.data.status}
                label={deliveryStatusLabels[current.data.status]}
              />
              <Link to={`/delivery/batches/${encodeURIComponent(current.data.id)}`}>
                查看阶段、预检与审批
              </Link>
            </Space>
          }
        />
      ) : null}
      {current.isError ? (
        <ManagementState
          compact
          kind="error"
          title="本次执行状态读取失败"
          actions={<Button onClick={() => void current.refetch()}>重试</Button>}
        />
      ) : null}
      <Card
        title="最近交付记录"
        size="small"
        extra={
          <Link
            to={`/execution-history?${new URLSearchParams({ applicationId: service.applicationId, serviceId: service.id, tab: 'workflows' })}`}
          >
            全部记录
          </Link>
        }
      >
        {!canView ? (
          <ManagementState compact kind="no-permission" title="无权查看交付记录" />
        ) : history.isLoading ? (
          <ManagementState compact kind="loading" />
        ) : history.isError ? (
          <ManagementState
            compact
            kind="error"
            title="交付记录读取失败"
            actions={<Button onClick={() => void history.refetch()}>重试</Button>}
          />
        ) : history.data?.length ? (
          <ul className="service-delivery-history" aria-label="最近交付记录">
            {history.data.map((batch) => {
              const targets = batch.targets.filter((item) => item.target.serviceId === service.id)
              const target = targets.length === 1 ? targets[0] : undefined
              const environments = [
                ...new Set(targets.map((item) => item.environmentName).filter(Boolean)),
              ].join('、')
              const action = target && deliveryActionLabels[target.target.action]
              const name =
                batch.definition.name === 'Application delivery'
                  ? [target?.serviceName || service.name, action].filter(Boolean).join(' · ')
                  : batch.definition.name
              return (
                <li className="service-delivery-history__card" key={batch.id}>
                  <div className="service-delivery-history__heading">
                    <Link to={`/delivery/batches/${encodeURIComponent(batch.id)}`}>{name}</Link>
                    <StatusTag
                      value={batch.status === 'partially_completed' ? 'warning' : batch.status}
                      label={deliveryStatusLabels[batch.status]}
                    />
                  </div>
                  <div className="service-delivery-history__meta">
                    {action ? <span>{action}</span> : null}
                    {targets.length > 1 ? <span>{targets.length} 个交付目标</span> : null}
                    {environments ? <span>{environments}</span> : null}
                    {target?.manifestRevision != null ? (
                      <span>配置版本 {target.manifestRevision}</span>
                    ) : null}
                    {target?.target.releaseBundleId ? (
                      <span>产物 {target.target.releaseBundleId}</span>
                    ) : null}
                    <time dateTime={batch.createdAt}>{formatDateTime(batch.createdAt)}</time>
                  </div>
                </li>
              )
            })}
          </ul>
        ) : (
          <ManagementState compact title="暂无交付记录" />
        )}
      </Card>
    </Space>
  )
}
