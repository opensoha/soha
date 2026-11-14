import type { IdentityOutpost } from './types'

export const outpostRuntimeLabels = { available: '可用', degraded: '降级', unavailable: '不可用' }

export function outpostRuntimeReason(reason?: string) {
  const labels: Record<string, string> = {
    embedded_runtime: '由 Soha 本机提供鉴权',
    signing_key_unconfigured: '控制面尚未配置签名密钥',
    awaiting_registration: '等待节点注册',
    awaiting_heartbeat: '等待首次心跳',
    protocol_incompatible: '节点协议版本不兼容',
    legacy_runtime: '旧协议节点，运行观测不完整',
    heartbeat_timeout: '节点心跳已超时',
    heartbeat_stale: '节点心跳延迟',
    configuration_not_applied: '等待节点应用配置',
    configuration_expired: '配置租约已过期',
    configuration_pending: '等待应用最新配置',
    configuration_expiry_unknown: '节点未报告配置有效期',
    configuration_unavailable: '节点没有可用配置',
    signature_invalid: '配置签名校验失败',
    configuration_rejected: '节点拒绝配置，请检查签名、协议与配置有效期',
  }
  return reason ? (labels[reason] ?? reason) : ''
}

export function outpostConfigurationSummary(outpost: IdentityOutpost) {
  if (outpost.mode === 'embedded') return '本机生效'
  if (outpost.configurationVersion === 0) return '尚未下发'
  return `${outpost.appliedConfigurationVersion ?? 0} / ${outpost.configurationVersion}`
}
