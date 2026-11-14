import { afterEach, describe, expect, it, vi } from 'vitest'
import { systemApi } from './api'
import { systemKeys } from './keys'
import { systemQueries } from './queries'

async function executeQuery(options: { queryFn?: unknown }) {
  if (typeof options.queryFn !== 'function') throw new Error('Expected queryFn')
  return options.queryFn({} as never)
}

describe('systemQueries', () => {
  afterEach(() => vi.restoreAllMocks())

  it('binds the canonical online-user key to the sessions API', async () => {
    const sessions = vi.spyOn(systemApi.sessions, 'list').mockResolvedValue([])
    const options = systemQueries.sessions()

    expect(options.queryKey).toEqual(systemKeys.sessions.list())
    const interval = options.refetchInterval as (query: unknown) => number | false
    expect(interval({ state: { status: 'success' } })).toBe(10_000)
    expect(interval({ state: { status: 'error' } })).toBe(false)
    await executeQuery(options)
    expect(sessions).toHaveBeenCalledOnce()
  })

  it('passes audit and operation filters to keys and APIs unchanged', async () => {
    const audit = vi.spyOn(systemApi.audit, 'list').mockResolvedValue([])
    const operations = vi.spyOn(systemApi.operationLogs, 'list').mockResolvedValue([])
    const auditFilters = { action: 'login', result: 'failure', metadataValue: 'user-1' }
    const operationFilters = { operationType: 'apply', metadataKey: 'usageSnapshot.templateId' }
    const auditOptions = systemQueries.audit('system', auditFilters)
    const operationOptions = systemQueries.operationLogs(operationFilters)

    expect(auditOptions.queryKey).toEqual(systemKeys.audit.list('system', auditFilters))
    expect(operationOptions.queryKey).toEqual(systemKeys.operationLogs.list(operationFilters))
    await executeQuery(auditOptions)
    await executeQuery(operationOptions)
    expect(audit).toHaveBeenCalledWith('system', auditFilters)
    expect(operations).toHaveBeenCalledWith(operationFilters)
  })

  it('guards system activity summaries with view permissions', () => {
    expect(systemQueries.sessions(false).enabled).toBe(false)
    expect(systemQueries.auditSummary(false).enabled).toBe(false)
    expect(systemQueries.operationSummary(false).enabled).toBe(false)
    expect(systemQueries.auditSummary().queryKey).toEqual(systemKeys.audit.summary())
    expect(systemQueries.operationSummary().queryKey).toEqual(systemKeys.operationLogs.summary())
  })
})
