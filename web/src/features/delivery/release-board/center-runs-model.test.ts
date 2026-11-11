import { expect, it } from 'vitest'
import type { DeliveryExecutionHistoryEntry, DeliveryTargetSnapshot } from '../types'
import { centerRunPhase, currentCenterStage, mergeCenterRuns } from './center-runs-model'

const createdAt = '2026-11-06T12:00:00Z'
const target = (id: string, applicationId: string): DeliveryTargetSnapshot => ({
  target: { id, applicationId, serviceId: 'api', action: 'build_deploy' },
  applicationName: applicationId,
  environmentName: 'Staging',
  serviceName: 'API',
  serviceVersion: 2,
  configurationDigest: 'sha256:example',
  buildNodeId: 'shared-build',
})

it('keeps shared build evidence and separate per-target plan and deploy evidence', () => {
  const targets = [target('one', 'Orders'), target('two', 'Inventory')]
  const entry: DeliveryExecutionHistoryEntry = {
    kind: 'batch',
    id: 'batch',
    createdAt,
    batch: {
      id: 'batch',
      rootRunId: 'root',
      definition: { name: 'Release', targets: targets.map(({ target }) => target) },
      targets,
      partialView: true,
      status: 'waiting_approval',
      createdBy: 'tester',
      createdAt,
      updatedAt: createdAt,
      serviceCount: 2,
      targetCount: 2,
      buildCount: 1,
      nodes: [
        {
          nodeId: 'shared-build',
          name: 'Build',
          type: 'build',
          stage: 'build',
          status: 'completed',
          buildRecordId: 'build-1',
        },
        {
          nodeId: 'plan-one',
          name: 'Plan',
          type: 'plan',
          stage: 'plan',
          targetId: 'one',
          status: 'waiting_approval',
          deliveryPlanId: 'plan-1',
        },
        {
          nodeId: 'deploy-two',
          name: 'Deploy',
          type: 'deploy',
          stage: 'deploy',
          targetId: 'two',
          status: 'running',
          executionTaskId: 'deploy-2',
        },
      ],
    },
  }
  const [run] = mergeCenterRuns([entry, entry], true)
  expect(run.partialView).toBe(true)
  expect(run.targets.map(({ application, environment }) => [application, environment])).toEqual([
    ['Orders', 'Staging'],
    ['Inventory', 'Staging'],
  ])
  const [one, two] = run.targets
  expect(one.stages.map(({ key }) => key)).toEqual(['shared-build', 'plan-one'])
  expect(two.stages.map(({ key }) => key)).toEqual(['shared-build', 'deploy-two'])
  expect(currentCenterStage(one.stages)?.evidence).toEqual([
    { kind: 'plan', href: '/delivery/batches/batch?targetId=one&planId=plan-1' },
  ])
  expect(currentCenterStage(two.stages)?.evidence).toEqual([
    {
      kind: 'task',
      href: '/delivery/batches/batch?targetId=two&evidenceKind=execution_task&evidenceId=deploy-2',
    },
  ])
  expect(centerRunPhase(run.status)).toBe('approval')
  expect(centerRunPhase('queued')).toBe('running')
  expect(centerRunPhase('partially_completed')).toBe('recent')
})
