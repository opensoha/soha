import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQueries, useQueryClient } from '@tanstack/react-query'
import {
  acknowledgeReadOnlyRunRequest,
  pendingRunRequests,
  forgetRunRequest,
  rememberRunRequest,
  runRecoveryBatch,
  pendingRunCancellations,
  requestRunCancellation,
  runCancellationRequested,
} from '../run-request-journal'
import { workbenchQueries } from '../queries'
import { workbenchKeys } from '../keys'
import type { ApiResponse } from '@/types'
import type { WorkbenchAgentRun } from '../types'
import { workbenchApi } from '../api'
import { isRunningAgentRun, isReadOnlyAgentRun } from '../agent-run-replay'
import { isTerminalWorkbenchAgentStatus } from '../stream'

function cancellationConfirmed(run: WorkbenchAgentRun) {
  return isTerminalWorkbenchAgentStatus(run.status) && !isRunningAgentRun(run)
}

export function useRunRequestRecovery(
  owner: string | undefined,
  sessionId: string | undefined,
  enabled: boolean,
) {
  const scope = owner ? JSON.stringify([workbenchApi.serviceScope(), owner]) : undefined
  const applied = useRef(new Map<string, string>())
  const inFlight = useRef(new Set<string>())
  const activeScope = useRef(scope)
  const [revision, setRevision] = useState(0)
  const [page, setPage] = useState(0)
  const [storageError, setStorageError] = useState<{ scope: string; sessionId: string }>()
  const client = useQueryClient()
  const refresh = useCallback(() => setRevision((value) => value + 1), [])
  useEffect(() => {
    activeScope.current = enabled ? scope : undefined
    return () => {
      activeScope.current = undefined
    }
  }, [scope, enabled])
  const applyRun = useCallback(
    (run: WorkbenchAgentRun) => {
      client.setQueryData<ApiResponse<WorkbenchAgentRun[]>>(
        workbenchKeys.agentRuns.session(run.sessionId),
        (before) => ({
          ...before,
          data: [...(before?.data ?? []).filter((item) => item.id !== run.id), run],
        }),
      )
      void client.invalidateQueries({
        queryKey: workbenchKeys.sessions.messages(run.sessionId, owner),
      })
    },
    [client, owner],
  )
  const cancellation = useMutation({
    mutationFn: async (input: {
      scope: string
      sessionId: string
      requestId: string
      runId: string
    }) => {
      const response = await workbenchApi.agentRuns.cancel(input.runId)
      if (response.data?.id !== input.runId || response.data.sessionId !== input.sessionId)
        throw new Error('取消回执与原任务不匹配')
      return response.data
    },
    onSuccess: (run, input) => {
      if (input.scope !== activeScope.current) return
      applyRun(run)
      if (cancellationConfirmed(run)) {
        forgetRunRequest(input.scope, input.sessionId, input.requestId)
        refresh()
      }
    },
    onSettled: (_run, _error, input) => {
      inFlight.current.delete(JSON.stringify([input.scope, input.sessionId, input.requestId]))
    },
  })
  useEffect(() => {
    window.addEventListener('storage', refresh)
    return () => window.removeEventListener('storage', refresh)
  }, [refresh])
  const journal = useMemo(() => {
    void revision
    try {
      const ids = pendingRunRequests(scope, sessionId)
      const cancellations = pendingRunCancellations(scope)
      const entries = [
        ...ids.map((requestId) => ({ sessionId: sessionId!, requestId })),
        ...cancellations.filter((entry) => entry.sessionId !== sessionId),
      ]
      return {
        ids,
        entries,
        cancelling: cancellations.some((entry) => entry.sessionId === sessionId),
        error: undefined,
      }
    } catch {
      return {
        ids: [],
        entries: [],
        cancelling: false,
        error: '任务恢复标识读取失败，请核对原会话',
      }
    }
  }, [scope, sessionId, revision])
  useEffect(() => {
    if (!enabled || journal.entries.length <= 64) return
    const timer = window.setInterval(() => setPage((value) => value + 1), 5000)
    return () => window.clearInterval(timer)
  }, [enabled, journal.entries.length])
  const batch = runRecoveryBatch(journal.entries, page)
  const recoveries = useQueries({
    queries: batch.map((entry) => ({
      ...workbenchQueries.agentRuns.request(scope, entry.sessionId, entry.requestId),
      enabled: enabled && Boolean(scope),
    })),
  })
  useEffect(() => {
    if (!enabled || !scope) return
    let changed = false
    recoveries.forEach((query, index) => {
      const run = query.data?.data
      const entry = batch[index]
      if (!run || !entry) return
      let stopping: boolean
      try {
        stopping = runCancellationRequested(scope, entry.sessionId, entry.requestId)
      } catch {
        changed = true
        return
      }
      const receipt = JSON.stringify([
        scope,
        entry.sessionId,
        entry.requestId,
        run.id,
        stopping,
        query.dataUpdatedAt,
      ])
      const key = JSON.stringify([scope, entry.sessionId, entry.requestId])
      if (isReadOnlyAgentRun(run)) {
        if (applied.current.get(key) === receipt) return
        try {
          acknowledgeReadOnlyRunRequest(scope, entry.sessionId, entry.requestId)
          // Cache updates may render again before the journal refresh commits.
          applied.current.set(key, receipt)
          applyRun(run)
          changed = true
        } catch {
          // Storage failure retains the unresolved journal and prevents dispatch.
        }
        return
      }
      if (cancellationConfirmed(run) && forgetRunRequest(scope, entry.sessionId, entry.requestId))
        changed = true
      if (applied.current.get(key) === receipt || inFlight.current.has(key)) return
      applied.current.set(key, receipt)
      applyRun(run)
      changed = true
      if (stopping && !cancellationConfirmed(run)) {
        inFlight.current.add(key)
        cancellation.mutate({ scope, ...entry, runId: run.id })
      }
    })
    if (changed) refresh()
  }, [recoveries, batch, scope, enabled, refresh, applyRun, cancellation])
  return {
    pending: journal.ids.length > 0,
    canRetry: (_id: string | undefined) =>
      !journal.cancelling && !journal.error && journal.ids.length === 0,
    cancelling: journal.cancelling,
    error:
      journal.error ||
      (storageError && storageError.scope === scope && storageError.sessionId === sessionId
        ? '停止意图保存失败，请保持页面打开并重试停止；离开后无法保证自动恢复取消。'
        : undefined) ||
      (journal.cancelling &&
      cancellation.isError &&
      cancellation.variables?.scope === scope &&
      cancellation.variables.sessionId === sessionId
        ? `停止任务未确认：${cancellation.error.message}`
        : undefined),
    recheck: () => {
      cancellation.reset()
      recoveries.forEach((query) => void query.refetch())
    },
    cancel: (target: string) => {
      if (!scope) throw new Error('请先登录后停止任务')
      try {
        requestRunCancellation(scope, target)
        setStorageError(undefined)
      } catch (error) {
        setStorageError({ scope, sessionId: target })
        throw error
      } finally {
        refresh()
      }
    },
    remember: (target: string, id: string) => {
      if (!scope) throw new Error('请先登录后分配任务')
      rememberRunRequest(scope, target, id)
      refresh()
    },
    acknowledge: (target: string, id: string) => {
      try {
        if (scope && !runCancellationRequested(scope, target, id))
          forgetRunRequest(scope, target, id)
      } finally {
        refresh()
      }
    },
  }
}
