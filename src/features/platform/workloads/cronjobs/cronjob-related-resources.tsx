import { useI18n } from '@/i18n'
import type { ScopeKey } from '@/types'
import { Select, Typography } from 'antd'
import { useState } from 'react'
import { ControllerRelatedResources } from '../shared/controller-related-resources'
import type { useWorkloadRefresh } from '../shared/workload-refresh'
import type { CronJobChildJob, CronJobDetail } from './types'

export function CronJobRelatedResources({
  detail,
  jobs,
  scope,
  onShowJobs,
}: {
  detail: CronJobDetail
  jobs?: CronJobChildJob[]
  scope: ScopeKey
  refreshToken: number
  onShowJobs: () => void
  interval?: ReturnType<typeof useWorkloadRefresh>['interval']
}) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'
  const [selected, setSelected] = useState<string>()
  const current = jobs?.find((job) => job.name === selected) || jobs?.[0]
  return (
    <ControllerRelatedResources
      detail={detail}
      kind="CronJob"
      scope={scope}
      onShowPods={onShowJobs}
      selectedChildName={current?.name}
      childControls={
        Boolean(jobs?.length) && (
          <>
            <Typography.Text>{zh ? '查看执行实例' : 'Inspect execution'}</Typography.Text>
            <Select
              aria-label={zh ? '关联 Job' : 'Related Job'}
              value={current?.name}
              onChange={setSelected}
              options={jobs?.map((job) => ({ label: job.name, value: job.name }))}
              popupMatchSelectWidth={false}
            />
          </>
        )
      }
    />
  )
}
