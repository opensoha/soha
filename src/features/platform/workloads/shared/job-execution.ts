import type { WorkloadCondition } from '@/types'

export interface JobExecution {
  conditions?: WorkloadCondition[]
  suspend?: boolean
  active?: number
  failed?: number
  succeeded?: number
  startTime?: string
  completionTime?: string
  completionTarget?: number
  completions?: number
}

export function jobState(job: JobExecution) {
  const has = (type: string) => job.conditions?.some((c) => c.type === type && c.status === 'True')
  if (has('Failed')) return 'failed'
  if (has('Complete')) return 'completed'
  if (has('FailureTarget')) return 'terminating'
  if (has('SuccessCriteriaMet')) return 'finishing'
  if (job.suspend || has('Suspended')) return 'suspended'
  if ((job.active ?? 0) > 0) return 'running'
  if ((job.failed ?? 0) > 0) return 'failures-recorded'
  if ((job.succeeded ?? 0) > 0) return 'awaiting-result'
  return job.active === 0 ? 'pending' : 'unknown'
}

export function jobStateLabel(state: ReturnType<typeof jobState>, zh: boolean) {
  const labels = {
    failed: ['失败', 'Failed'],
    completed: ['已完成', 'Completed'],
    terminating: ['失败收尾中', 'Terminating after failure'],
    finishing: ['完成收尾中', 'Finishing'],
    suspended: ['已暂停', 'Suspended'],
    running: ['执行中', 'Executing'],
    'failures-recorded': ['有失败记录', 'Failed Pods recorded'],
    'awaiting-result': ['等待结果确认', 'Awaiting result'],
    pending: ['待执行', 'Pending'],
    unknown: ['未知', 'Unknown'],
  }
  return labels[state][zh ? 0 : 1]
}

export function jobEndTime(job: JobExecution) {
  const state = jobState(job)
  if (state === 'completed')
    return (
      job.completionTime ||
      job.conditions?.find((c) => c.type === 'Complete' && c.status === 'True')?.lastTransitionTime
    )
  if (state === 'failed')
    return job.conditions?.find((c) => c.type === 'Failed' && c.status === 'True')
      ?.lastTransitionTime
  return undefined
}

export function jobDuration(job: JobExecution, now = Date.now()) {
  const start = Date.parse(job.startTime || '')
  const state = jobState(job),
    terminal = ['failed', 'completed'].includes(state)
  const end = terminal ? Date.parse(jobEndTime(job) || '') : now
  return state !== 'suspended' && Number.isFinite(start) && Number.isFinite(end) && end >= start
    ? Math.floor((end - start) / 1000)
    : undefined
}

export function jobCompletionTarget(job: JobExecution) {
  return (
    job.completionTarget ?? (job.completions && job.completions > 0 ? job.completions : undefined)
  )
}

export function formatJobDuration(seconds?: number) {
  if (seconds === undefined || !Number.isFinite(seconds)) return '—'
  const total = Math.max(0, Math.floor(seconds))
  if (total < 60) return `${total}s`
  if (total < 3600) return `${Math.floor(total / 60)}m ${total % 60}s`
  if (total < 86400) return `${Math.floor(total / 3600)}h ${Math.floor((total % 3600) / 60)}m`
  return `${Math.floor(total / 86400)}d ${Math.floor((total % 86400) / 3600)}h`
}
