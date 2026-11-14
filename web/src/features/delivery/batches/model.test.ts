import { expect, it } from 'vitest'
import type { DeliveryBatch, DeliveryWorkflowDefinition } from '../types'
import { deliveryServiceCount, targetStageNode, validateDeliveryEditor } from './model'

const definition: DeliveryWorkflowDefinition = {
  name: '联合发布',
  mode: 'build_all_then_deploy',
  stopOnFailure: true,
  targets: Array.from({ length: 20 }, (_, i) => ({
    id: `target-${i}`,
    applicationId: `app-${Math.floor(i / 8)}`,
    serviceId: `service-${Math.floor(i / 2)}`,
    applicationEnvironmentId: i % 2 ? 'prod' : 'dev',
    action: 'build_deploy',
    group: i % 2,
    dependsOn: i % 2 ? [`target-${i - 1}`] : [],
  })),
}

it('counts 10 services and accepts 20 distinct environment targets with promotion dependencies', () => {
  expect(deliveryServiceCount(definition.targets)).toBe(10)
  expect(validateDeliveryEditor(definition)).toBeUndefined()
  expect(
    validateDeliveryEditor({
      ...definition,
      targets: [...definition.targets, { ...definition.targets[0], id: 'duplicate' }],
    }),
  ).toContain('重复')
})

it('rejects dependencies conflicting with serial order, groups, or forming a cycle before starting', () => {
  const targets = definition.targets.slice(0, 2).map((item) => ({ ...item }))
  targets[0].dependsOn = [targets[1].id]
  targets[1].dependsOn = []
  expect(validateDeliveryEditor({ ...definition, targets, mode: 'service_serial' })).toContain(
    '顺序',
  )
  expect(validateDeliveryEditor({ ...definition, targets })).toContain('分组')
  targets[1].group = 0
  targets[1].dependsOn = [targets[0].id]
  expect(validateDeliveryEditor({ ...definition, targets })).toContain('循环')
})

it('shows the shared build status on every consumer without inventing target-specific tasks', () => {
  const batch = {
    targets: [
      { target: { id: 'dev' }, buildNodeId: 'build-1' },
      { target: { id: 'prod' }, buildNodeId: 'build-1' },
    ],
    nodes: [
      { nodeId: 'build-1', targetId: 'dev', stage: 'build', status: 'failed' },
      { nodeId: 'prod:deploy', targetId: 'prod', stage: 'deploy', status: 'skipped' },
    ],
  } as DeliveryBatch
  expect(targetStageNode(batch, 'prod', 'build')?.status).toBe('failed')
  expect(targetStageNode(batch, 'prod', 'deploy')?.status).toBe('skipped')
  expect(targetStageNode(batch, 'dev', 'deploy')).toBeUndefined()
})
