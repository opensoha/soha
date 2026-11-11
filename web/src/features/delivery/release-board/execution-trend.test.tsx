import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import { MemoryRouter } from 'react-router-dom'
import { deliveryKeys, normalizeBatchListParams, normalizeDeliveryListParams } from '../keys'
import type { BuildRecord, WorkflowRun, DeliveryBatch } from '../types'
import { executionDuration, executionPoints, executionTone } from './execution-trend-model'
import { ExecutionTrendPlot } from './execution-trend'

const start = '2026-09-14T01:00:00Z'
const finish = '2026-09-14T01:02:00Z'
const now = Date.parse('2026-09-14T01:03:00Z')
const build: BuildRecord = {
  id: 'build-1',
  applicationId: 'app',
  sourceSystem: 'manual',
  status: 'completed',
  createdAt: start,
  startedAt: start,
  finishedAt: finish,
}

describe('execution trends', () => {
  it('uses real execution boundaries and preserves missing or partial durations', () => {
    expect(executionDuration(build, now)).toBe(120)
    expect(executionDuration({ ...build, status: 'running', finishedAt: undefined }, now)).toBe(180)
    expect(executionDuration({ ...build, finishedAt: undefined }, now)).toBeNull()
    expect(executionDuration({ ...build, finishedAt: start, startedAt: finish }, now)).toBeNull()
    const workflow = {
      id: 'w',
      status: 'completed',
      createdAt: start,
      updatedAt: finish,
      nodeRuns: [{ startedAt: start, finishedAt: finish }],
    } as WorkflowRun
    expect(executionDuration(workflow, now)).toBe(120)
    expect(executionDuration({ ...workflow, nodeRuns: [] }, now)).toBeNull()
    expect(
      executionDuration(
        {
          ...workflow,
          rootRunId: 'w',
          partialView: true,
          nodes: workflow.nodeRuns,
        } as unknown as DeliveryBatch,
        now,
      ),
    ).toBeNull()
  })

  it('keeps definition filters in both requests and cache keys', () => {
    expect(
      normalizeDeliveryListParams({ applicationId: 'app', buildSourceId: ' source ', limit: 10 }),
    ).toEqual({ applicationId: 'app', buildSourceId: 'source', limit: 10 })
    expect(normalizeDeliveryListParams({ applicationEnvironmentId: ' env ', limit: 10 })).toEqual({
      applicationEnvironmentId: 'env',
      limit: 10,
    })
    expect(normalizeBatchListParams({ workflowId: ' workflow ', limit: 10 })).toEqual({
      workflowId: 'workflow',
      limit: 10,
    })
    expect(deliveryKeys.builds.list({ buildSourceId: 'a' })).not.toEqual(
      deliveryKeys.builds.list({ buildSourceId: 'b' }),
    )
  })

  it('renders ten compact status bars in chronological order with accessible detail links', () => {
    const records = Array.from({ length: 12 }, (_, i) => ({
      ...build,
      id: String(i),
      createdAt: new Date(Date.parse(start) + i * 1000).toISOString(),
      finishedAt: i === 9 ? undefined : finish,
      status: i === 10 ? 'failed' : i === 11 ? 'running' : 'completed',
    })).reverse()
    const points = executionPoints(records, now)
    expect(points.map((point) => point.id)).toEqual([
      '2',
      '3',
      '4',
      '5',
      '6',
      '7',
      '8',
      '9',
      '10',
      '11',
    ])
    const markup = renderToStaticMarkup(
      <MemoryRouter>
        <ExecutionTrendPlot points={points} />
      </MemoryRouter>,
    )
    expect(markup.match(/class="soha-execution-trend__fill"/g)).toHaveLength(10)
    expect(markup).not.toContain('<svg')
    expect(markup).not.toContain('soha-execution-trend__unknown')
    expect(markup).toContain('耗时未知')
    expect(markup).toContain('href="/builds/10"')
    expect(markup).toContain('soha-execution-trend--danger')
    expect(markup).toContain('soha-execution-trend--primary')
    expect(markup).toContain('aria-label="10 ·')
    expect(
      ['completed', 'failed', 'running', 'waiting_approval', 'canceled'].map(executionTone),
    ).toEqual(['success', 'danger', 'primary', 'warning', 'neutral'])
  })
})
