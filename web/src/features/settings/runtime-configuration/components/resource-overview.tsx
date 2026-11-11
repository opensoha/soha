import { ManagementIconButton } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { readableQueryData } from '@/utils/query-feedback'
import { formatAgeSeconds, formatDateTime } from '@/utils/time'
import {
  ApartmentOutlined,
  CloudServerOutlined,
  DatabaseOutlined,
  DownloadOutlined,
  HddOutlined,
  ReloadOutlined,
  UploadOutlined,
} from '@ant-design/icons'
import type { UseQueryResult } from '@tanstack/react-query'
import { Progress, Skeleton, Typography } from 'antd'
import type { ReactNode } from 'react'
import type { RuntimeResourceSnapshot } from '../types'

const { Text } = Typography

function formatBytes(value: number) {
  if (!Number.isFinite(value)) return '-'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let current = Math.max(0, value)
  let index = 0
  while (current >= 1024 && index < units.length - 1) {
    current /= 1024
    index += 1
  }
  return `${current >= 10 ? current.toFixed(0) : current.toFixed(1)} ${units[index]}`
}

function formatPercent(value: number) {
  return `${Math.max(0, value).toFixed(1)}%`
}

function UsageProgress({ percent }: { percent: number }) {
  return (
    <Progress
      aria-label={`使用率 ${formatPercent(percent)}`}
      percent={Math.min(100, Math.max(0, percent))}
      showInfo={false}
      size="small"
      strokeColor="var(--soha-primary)"
    />
  )
}

function Metric({
  children,
  detail,
  icon,
  label,
  progress,
  value,
}: {
  children?: ReactNode
  detail: ReactNode
  icon: ReactNode
  label: string
  progress?: number
  value: ReactNode
}) {
  return (
    <div className="soha-runtime-resource-metric">
      <div className="soha-runtime-resource-metric__label">
        {icon}
        <Text type="secondary">{label}</Text>
      </div>
      <div className="soha-runtime-resource-metric__value">{value}</div>
      <Text className="soha-runtime-resource-metric__detail" type="secondary">
        {detail}
      </Text>
      {progress === undefined ? children : <UsageProgress percent={progress} />}
    </div>
  )
}

export function RuntimeResourceOverview({
  query,
}: {
  query: UseQueryResult<RuntimeResourceSnapshot, Error>
}) {
  const snapshot = readableQueryData(query)

  return (
    <section className="soha-runtime-resource-overview" aria-label="Soha 服务资源">
      <div className="soha-runtime-resource-overview__header">
        <div>
          <Text strong>服务资源</Text>
          {snapshot ? (
            <Text className="soha-runtime-resource-overview__updated" type="secondary">
              更新于 {formatDateTime(snapshot.generatedAt)}
            </Text>
          ) : null}
        </div>
        <ManagementIconButton
          aria-label="刷新服务资源"
          icon={<ReloadOutlined />}
          loading={query.isFetching}
          tooltip="刷新服务资源"
          onClick={() => void query.refetch()}
        />
      </div>
      <ReadFeedback query={query} label="服务资源" canRead={query.isEnabled} />
      {query.isLoading && !snapshot ? (
        <div className="soha-runtime-resource-overview__loading">
          <Skeleton active paragraph={{ rows: 2 }} title={false} />
        </div>
      ) : snapshot ? (
        <div className="soha-runtime-resource-grid">
          <Metric
            detail={`${snapshot.cpu.logicalCores} 个逻辑核`}
            icon={<CloudServerOutlined />}
            label="CPU"
            progress={snapshot.cpu.usagePercent}
            value={formatPercent(snapshot.cpu.usagePercent)}
          />
          <Metric
            detail={`Go 保留 ${formatBytes(snapshot.memory.goReservedBytes)}`}
            icon={<DatabaseOutlined />}
            label="内存"
            progress={snapshot.memory.heapUsagePercent}
            value={`${formatBytes(snapshot.memory.heapAllocBytes)} / ${formatBytes(snapshot.memory.heapSysBytes)}`}
          />
          <Metric
            detail={snapshot.disk.available ? snapshot.disk.path : '文件系统指标不可用'}
            icon={<HddOutlined />}
            label="磁盘"
            progress={snapshot.disk.available ? snapshot.disk.usagePercent : undefined}
            value={
              snapshot.disk.available
                ? `${formatBytes(snapshot.disk.usedBytes)} / ${formatBytes(snapshot.disk.totalBytes)}`
                : '-'
            }
          />
          <Metric
            detail={
              snapshot.network.available
                ? `累计 ${formatBytes(snapshot.network.rxBytes)} / ${formatBytes(snapshot.network.txBytes)}`
                : '当前平台不提供可靠的流量归属'
            }
            icon={<ApartmentOutlined />}
            label="网络"
            value={
              snapshot.network.available ? (
                <span className="soha-runtime-resource-network">
                  <span>
                    <DownloadOutlined /> {formatBytes(snapshot.network.rxBytesPerSecond)}/s
                  </span>
                  <span>
                    <UploadOutlined /> {formatBytes(snapshot.network.txBytesPerSecond)}/s
                  </span>
                </span>
              ) : (
                '-'
              )
            }
          />
          <Metric
            detail={`GC ${snapshot.goRuntime.gcCycles} 次 · GOMAXPROCS ${snapshot.goRuntime.gomaxprocs}`}
            icon={<CloudServerOutlined />}
            label="Go 运行时"
            value={`${snapshot.goRuntime.goroutines} 个协程`}
          />
          <Metric
            detail={`失败 ${snapshot.services.failed} · 取消 ${snapshot.services.canceled} · 队列 ${snapshot.services.queueDepth}`}
            icon={<ApartmentOutlined />}
            label="后台服务"
            value={`${snapshot.services.succeeded} / ${snapshot.services.started} 成功`}
          >
            <Text className="soha-runtime-resource-metric__uptime" type="secondary">
              已运行 {formatAgeSeconds(snapshot.uptimeSeconds)}
            </Text>
          </Metric>
        </div>
      ) : null}
    </section>
  )
}
