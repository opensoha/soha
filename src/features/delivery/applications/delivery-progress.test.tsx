import { renderToStaticMarkup } from 'react-dom/server'
import { expect, it } from 'vitest'
import { WorkflowProgress, WorkloadProgress } from './delivery-progress'
import type { ApplicationRuntimeWorkload, DeliveryApplicationBindingSummary } from '../types'

const workload: ApplicationRuntimeWorkload = {
  applicationEnvironmentId: 'test',
  clusterId: 'c',
  namespace: 'ns',
  workloadName: 'web',
  workloadKind: 'Deployment',
  desiredReplicas: 3,
  updatedReplicas: 1,
  readyReplicas: 3,
  availableReplicas: 3,
}

it('keeps old ready replicas separate from the new rollout and handles zero and failure explicitly', () => {
  const rolling = renderToStaticMarkup(<WorkloadProgress workload={workload} />)
  expect(rolling).toContain('1 / 3 已更新')
  expect(rolling).toContain('3 / 3 已就绪')
  expect(rolling).not.toContain('当前版本已就绪')
  expect(rolling).toContain('data-status="process"')
  expect(rolling).not.toContain('data-status="finish"')
  expect(
    renderToStaticMarkup(
      <WorkloadProgress
        workload={{
          ...workload,
          desiredReplicas: 0,
          readyReplicas: 0,
          updatedReplicas: 0,
          availableReplicas: 0,
        }}
      />,
    ),
  ).toContain('data-status="wait"')
  expect(
    renderToStaticMarkup(<WorkloadProgress workload={{ ...workload, healthStatus: 'failed' }} />),
  ).toContain('data-status="error"')
  expect(
    renderToStaticMarkup(<WorkloadProgress workload={{ ...workload, workloadKind: 'Job' }} />),
  ).toBe('')
  for (const sourceStatus of ['unavailable', 'forbidden', 'replaced', 'missing'] as const) {
    const output = renderToStaticMarkup(
      <WorkloadProgress workload={{ ...workload, sourceStatus }} />,
    )
    expect(output).toContain('更新状态未知')
    expect(output).not.toContain('data-status="finish"')
  }
  expect(
    renderToStaticMarkup(<WorkloadProgress workload={{ ...workload, healthStatus: 'unknown' }} />),
  ).toContain('更新状态未知')
})

it('renders recorded node results without marking missing or skipped steps successful', () => {
  const binding: DeliveryApplicationBindingSummary = {
    applicationEnvironmentId: 'test',
    environmentId: 'test',
    targetCount: 1,
    requiresApproval: false,
    latestWorkflow: {
      id: 'run-1',
      applicationId: 'app',
      workflowName: 'release',
      createdAt: '',
      updatedAt: '',
      status: 'failed',
      steps: [],
      nodeRuns: [
        { nodeId: 'build', name: '构建镜像', type: 'build', status: 'succeeded' },
        {
          nodeId: 'deploy',
          name: '部署服务',
          type: 'deploy',
          status: 'failed',
          summary: '镜像无法拉取',
        },
      ],
    },
  }
  const output = renderToStaticMarkup(<WorkflowProgress binding={binding} />)
  expect(output).toContain('构建镜像')
  expect(output).toContain('镜像无法拉取')
  expect(output).toMatch(/class="[^"]*soha-execution-node is-success"/)
  expect(output).toMatch(/class="[^"]*soha-execution-node is-error"/)
  for (const [status, label] of [
    ['running', '执行中'],
    ['waiting_approval', '等待审批'],
    ['skipped', '已跳过'],
    ['cancelled', '已取消'],
    ['new-provider-status', '状态未知 · new-provider-status'],
  ]) {
    const result = renderToStaticMarkup(
      <WorkflowProgress
        binding={{
          ...binding,
          latestWorkflow: {
            ...binding.latestWorkflow!,
            nodeRuns: [{ nodeId: 'one', name: '真实节点', status, type: 'build' }],
          },
        }}
      />,
    )
    expect(result).toContain(label)
    expect(result).not.toContain('is-success')
    expect(result).not.toContain('progressbar')
  }
  expect(
    renderToStaticMarkup(<WorkflowProgress binding={{ ...binding, latestWorkflow: undefined }} />),
  ).toContain('暂无执行步骤')
  expect(output).toContain('disabled=""')
  expect(
    renderToStaticMarkup(<WorkflowProgress binding={binding} onSelectStage={() => {}} />),
  ).not.toContain('disabled=""')
  const withTemplate: DeliveryApplicationBindingSummary = {
    ...binding,
    workflowTemplate: {
      id: 'template',
      key: 'template',
      name: '模板',
      enabled: true,
      createdAt: '',
      updatedAt: '',
      definition: {
        nodes: [{ id: 'build', name: '模板构建', type: 'build' }],
        edges: [],
      },
    },
  }
  const notRun = renderToStaticMarkup(
    <WorkflowProgress binding={{ ...withTemplate, latestWorkflow: undefined }} />,
  )
  expect(notRun).toContain('模板构建，未运行')
  expect(notRun).toContain('disabled=""')
  expect(notRun).not.toContain('is-success')
  const missing = renderToStaticMarkup(
    <WorkflowProgress
      binding={{
        ...withTemplate,
        latestWorkflow: { ...binding.latestWorkflow!, nodeRuns: [], steps: [] },
      }}
      onSelectStage={() => {}}
    />,
  )
  expect(missing).toContain('模板构建，无步骤记录')
  expect(missing).toContain('disabled=""')
  expect(missing).not.toContain('is-success')
})
