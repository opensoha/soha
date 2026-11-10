import type { ReactNode } from 'react'
import { Alert, Button, Card, Space, Tag, Tooltip, Typography } from 'antd'
import {
  ArrowRightOutlined,
  QuestionCircleOutlined,
  RobotOutlined,
  SafetyCertificateOutlined,
} from '@ant-design/icons'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { ManagementState } from '@/components/management-list'
import { isApiError } from '@/services/api-error'
import type { GatewayManifest, GatewayTool } from '@/features/copilot'
import { deliveryQueries } from './queries'

const { Text } = Typography

type GatewayReadinessState = 'available' | 'approval' | 'restricted' | 'unavailable'

export interface DeliveryGatewayReadinessProps {
  capabilities: string[]
  description: string
  manualPath?: string
  manualTitle: string
  skillId: string
  title: string
}

interface CapabilityStatus {
  availableTools: GatewayTool[]
  missingCapabilities: string[]
  requiredScopes: string[]
  state: GatewayReadinessState
}

function uniqueSorted(values: Array<string | undefined>) {
  return Array.from(new Set(values.map((item) => item?.trim()).filter(Boolean) as string[])).sort()
}

function summarizeCapabilityStatus(
  manifest: GatewayManifest | undefined,
  capabilities: string[],
): CapabilityStatus {
  const requested = uniqueSorted(capabilities)
  const tools = manifest?.tools ?? []
  const byName = new Map(tools.map((tool) => [tool.name, tool]))
  const availableTools = requested.map((name) => byName.get(name)).filter(Boolean) as GatewayTool[]
  const missingCapabilities = requested.filter((name) => !byName.has(name))
  const requiredScopes = uniqueSorted(availableTools.flatMap((tool) => tool.requiredScopes ?? []))

  if (!manifest || availableTools.length === 0) {
    return {
      availableTools,
      missingCapabilities,
      requiredScopes,
      state: 'restricted',
    }
  }
  if (missingCapabilities.length > 0 || (manifest.summary?.deniedCount ?? 0) > 0) {
    return {
      availableTools,
      missingCapabilities,
      requiredScopes,
      state: 'restricted',
    }
  }
  if (availableTools.some((tool) => tool.requiresApproval)) {
    return {
      availableTools,
      missingCapabilities,
      requiredScopes,
      state: 'approval',
    }
  }
  return {
    availableTools,
    missingCapabilities,
    requiredScopes,
    state: 'available',
  }
}

function gatewayErrorReason(error: unknown) {
  if (!error) return ''
  if (isApiError(error)) {
    if (error.status === 403) return '当前账号缺少 AI Gateway 查看或调用权限。'
    if (error.status === 0) return 'AI Gateway 或后端 API 当前不可达。'
    return error.message
  }
  if (error instanceof Error && error.message) return error.message
  return '无法读取 AI Gateway 运行态能力。'
}

function readinessTone(state: GatewayReadinessState): {
  alertType: 'success' | 'info' | 'warning' | 'error'
  label: string
  title: string
} {
  if (state === 'available') {
    return { alertType: 'success', label: '可用', title: 'AI Gateway 可直接辅助' }
  }
  if (state === 'approval') {
    return { alertType: 'info', label: '需要审批', title: 'AI Gateway 可用，调用需要审批' }
  }
  if (state === 'restricted') {
    return { alertType: 'warning', label: '受限', title: 'AI Gateway 能力受限' }
  }
  return { alertType: 'error', label: '不可用', title: 'AI Gateway 当前不可用' }
}

function readinessReason({
  error,
  manifest,
  status,
}: {
  error: unknown
  manifest?: GatewayManifest
  status: CapabilityStatus
}) {
  const errorReason = gatewayErrorReason(error)
  if (errorReason) return errorReason
  if (status.state === 'approval') {
    return '当前身份可以看到所需交付能力，但至少一个 Gateway tool 需要审批策略放行后才能执行。'
  }
  if (status.state === 'restricted') {
    if ((manifest?.summary?.toolCount ?? 0) === 0) {
      return '当前身份没有可见的交付 AI tool，通常是缺少 scope、tool grant、access policy 或 skill binding。'
    }
    if (status.missingCapabilities.length > 0) {
      return '部分交付 AI tool 不在当前 manifest 中，通常是 scope、tool grant、access policy 或 skill binding 未覆盖。'
    }
    return '当前 manifest 存在被拒绝的能力，请检查 Gateway 授权、scope 和 skill 绑定。'
  }
  return '当前身份已具备本页所需的交付 AI 能力；所有 AI 产物仍需进入草稿、预览和确认流程。'
}

function CapabilityTags({ children, label }: { children: ReactNode; label: string }) {
  return (
    <Space size={6} wrap>
      <Text type="secondary">{label}</Text>
      {children}
    </Space>
  )
}

export function DeliveryGatewayReadinessPanel({
  capabilities,
  description,
  manualPath,
  manualTitle,
  skillId,
  title,
}: DeliveryGatewayReadinessProps) {
  const navigate = useNavigate()
  const manifestQuery = useQuery(
    deliveryQueries.gateway.readiness({ skillId, source: 'delivery-workbench' }),
  )
  if (manifestQuery.isPending) {
    return (
      <ManagementState bordered={false} compact kind="loading" title="正在读取 AI Gateway 能力" />
    )
  }

  const manifest = manifestQuery.data
  const status = summarizeCapabilityStatus(manifest, capabilities)
  const state: GatewayReadinessState = manifestQuery.isError ? 'unavailable' : status.state
  const tone = readinessTone(state)
  const reason = readinessReason({ error: manifestQuery.error, manifest, status })

  return (
    <Card className="soha-delivery-workbench-ai-card" size="small">
      <Space align="start" size={12}>
        <RobotOutlined className="soha-delivery-workbench-ai-card__icon" />
        <Space className="soha-delivery-gateway-readiness" orientation="vertical" size={10}>
          <Space size={8} wrap>
            <Text strong>{title}</Text>
            <Tooltip title={description} trigger={['hover', 'focus']}>
              <QuestionCircleOutlined aria-label={`${title}说明`} tabIndex={0} />
            </Tooltip>
            <Tag
              color={
                state === 'available'
                  ? 'success'
                  : state === 'approval'
                    ? 'processing'
                    : state === 'restricted'
                      ? 'warning'
                      : 'error'
              }
            >
              {tone.label}
            </Tag>
            {manifestQuery.isFetching ? <Tag>刷新中</Tag> : null}
          </Space>
          <Alert
            showIcon
            type={tone.alertType}
            title={tone.title}
            description={reason}
            action={
              <Space size={8} wrap>
                <Button
                  size="small"
                  icon={<SafetyCertificateOutlined />}
                  onClick={() => navigate('/ai-gateway/manifest')}
                >
                  能力清单
                </Button>
                {manualPath ? (
                  <Button
                    size="small"
                    icon={<ArrowRightOutlined />}
                    onClick={() => navigate(manualPath)}
                  >
                    {manualTitle}
                  </Button>
                ) : null}
              </Space>
            }
          />
          {status.requiredScopes.length > 0 ? (
            <CapabilityTags label="所需 scope">
              {status.requiredScopes.map((scope) => (
                <Tag key={scope}>{scope}</Tag>
              ))}
            </CapabilityTags>
          ) : null}
          {status.availableTools.length > 0 ? (
            <CapabilityTags label="可见能力">
              {status.availableTools.map((tool) => (
                <Tag key={tool.name} color={tool.requiresApproval ? 'processing' : undefined}>
                  {tool.name}
                  {tool.requiresApproval ? ' / 审批' : ''}
                </Tag>
              ))}
            </CapabilityTags>
          ) : null}
          {status.missingCapabilities.length > 0 ? (
            <CapabilityTags label="缺失能力">
              {status.missingCapabilities.map((name) => (
                <Tag key={name} color="warning">
                  {name}
                </Tag>
              ))}
            </CapabilityTags>
          ) : null}
        </Space>
      </Space>
    </Card>
  )
}
