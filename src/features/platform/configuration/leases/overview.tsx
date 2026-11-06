import { StatusTag } from '@/components/status-tag'
import { useI18n } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { Button, Card, Tooltip, Typography } from 'antd'
import { useEffect, useState } from 'react'
import {
  WorkloadOverview,
  WorkloadOverviewConfiguration,
  WorkloadOverviewMetadata,
} from '../../workloads/shared/workload-overview'
import { leaseTiming, type LeaseDetail } from './lease-model'

function HelpTitle({ title, help }: { title: string; help: string }) {
  return (
    <span className="soha-lease-card-title">
      {title}
      <Tooltip title={help} trigger={['hover', 'focus', 'click']}>
        <Button
          type="text"
          size="small"
          icon={<QuestionCircleOutlined />}
          aria-label={title + ' info'}
        />
      </Tooltip>
    </span>
  )
}
function Timestamp({ value, zh }: { value?: string; zh: boolean }) {
  if (!value) return <span>—</span>
  return (
    <Tooltip title={value} trigger={['hover', 'focus', 'click']}>
      <span tabIndex={0} className="soha-lease-time">
        {Number.isFinite(Date.parse(value))
          ? formatDateTime(value)
          : zh
            ? '时间无法解析'
            : 'Invalid timestamp'}
      </span>
    </Tooltip>
  )
}
export function LeaseOverview({ detail }: { detail: LeaseDetail }) {
  const { localeCode } = useI18n(),
    zh = localeCode === 'zh_CN'
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const tick = () => {
      if (document.visibilityState === 'visible') setNow(Date.now())
    }
    const timer = window.setInterval(tick, 15000)
    document.addEventListener('visibilitychange', tick)
    return () => {
      window.clearInterval(timer)
      document.removeEventListener('visibilitychange', tick)
    }
  }, [])
  const timing = leaseTiming(detail, now)
  const labels = {
    unheld: zh ? '未声明持有者' : 'No holder declared',
    unknown: zh ? '无法估算租期' : 'Cannot estimate lease window',
    clock: zh ? '续租时间晚于本机' : 'Renewal is ahead of local clock',
    elapsed: zh ? '记录已超租期' : 'Recorded lease window elapsed',
    within: zh ? '记录未超租期' : 'Within recorded lease window',
  }
  return (
    <WorkloadOverview
      aside={
        <Card title={zh ? '租约配置' : 'Lease configuration'}>
          <WorkloadOverviewConfiguration
            items={[
              { label: zh ? '命名空间' : 'Namespace', children: detail.namespace },
              { label: 'API', children: 'coordination.k8s.io/v1' },
              {
                label: zh ? '创建时间' : 'Created',
                children: <Timestamp value={detail.createdAt} zh={zh} />,
              },
              ...(detail.uid
                ? [
                    {
                      label: 'UID',
                      children: <Typography.Text copyable>{detail.uid}</Typography.Text>,
                    },
                  ]
                : []),
            ]}
          />
        </Card>
      }
    >
      <Card
        title={zh ? '持有者' : 'Holder'}
        extra={
          <StatusTag
            value={
              timing.state === 'elapsed' || timing.state === 'clock'
                ? 'warning'
                : timing.state === 'within'
                  ? 'info'
                  : 'unknown'
            }
            label={labels[timing.state]}
          />
        }
      >
        {detail.holderIdentity ? (
          <Typography.Text className="soha-lease-holder" copyable>
            {detail.holderIdentity}
          </Typography.Text>
        ) : (
          <span className="soha-lease-muted">
            {zh ? '此记录未声明持有者' : 'This record declares no holder'}
          </span>
        )}
        <Typography.Paragraph type="secondary">
          {zh
            ? 'holderIdentity 是协调客户端声明的标识，不保证对应 Pod 或 Node；本页不据此推断对象关系。'
            : 'holderIdentity is an identifier declared by the coordinating client. It does not guarantee a Pod or Node identity, so no object relationship is inferred.'}
        </Typography.Paragraph>
        <div className="soha-lease-summary">
          <div>
            <span>{zh ? '租约时长' : 'Lease duration'}</span>
            <strong>
              {detail.leaseDurationSeconds == null ? '—' : `${detail.leaseDurationSeconds} s`}
            </strong>
          </div>
          <div>
            <span>{zh ? '持有者切换次数' : 'Holder transitions'}</span>
            <strong>{detail.leaseTransitions ?? '—'}</strong>
          </div>
        </div>
      </Card>
      <Card
        title={
          <HelpTitle
            title={zh ? '续租记录' : 'Renewal record'}
            help={
              zh
                ? '估算时间 = renewTime + leaseDurationSeconds，基于浏览器本机时钟。客户端时钟偏差、读取延迟和不同协调实现都会影响判断，不能据此确认节点故障或实际锁所有权。刷新仅重读，不会续租或抢占。'
                : 'Estimated time = renewTime + leaseDurationSeconds, using the browser clock. Clock skew, read delay and coordination implementations affect interpretation. This does not verify node health or lock ownership. Refresh only reads; it does not renew or acquire the Lease.'
            }
          />
        }
      >
        <WorkloadOverviewConfiguration
          items={[
            {
              label: zh ? '获取时间' : 'Acquired',
              children: <Timestamp value={detail.acquireTime} zh={zh} />,
            },
            {
              label: zh ? '最近续租' : 'Last renewed',
              children: <Timestamp value={detail.renewTime} zh={zh} />,
            },
            {
              label: zh ? '租期截止（估算）' : 'Lease window end (estimated)',
              children: (
                <Timestamp
                  value={
                    timing.expiresAt === undefined
                      ? undefined
                      : new Date(timing.expiresAt).toISOString()
                  }
                  zh={zh}
                />
              ),
            },
            ...(detail.preferredHolder !== undefined
              ? [
                  {
                    label: zh ? '优选持有者' : 'Preferred holder',
                    children: detail.preferredHolder || '—',
                  },
                ]
              : []),
            ...(detail.strategy !== undefined
              ? [{ label: zh ? '协调策略' : 'Strategy', children: detail.strategy || '—' }]
              : []),
          ]}
        />
      </Card>
      <WorkloadOverviewMetadata labels={detail.labels} annotations={detail.annotations} />
    </WorkloadOverview>
  )
}
