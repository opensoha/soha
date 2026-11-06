import { describe, expect, it } from 'vitest'
import type { AlertEvent } from '@/features/observability'
import type { AuditLog, OperationLog } from '@/features/system'
import type { Cluster, ClusterCapabilityMatrixEntry } from '@/types'
import type { KubernetesClusterEvent } from '@opensoha/contracts/gen/ts/sohaapi'
import type { ClusterNode } from '../cluster-resources/types'
import {
  buildActivityTimeline,
  buildOperationsAIEvidence,
  filterAlertsForCluster,
  formatPlatformOverviewAlertScope,
  formatPlatformOverviewCapability,
  formatPlatformOverviewText,
  selectActiveAlerts,
  summarizeCapabilityGovernance,
  summarizeFleetReadiness,
  summarizeNodeCapacity,
} from './model'

describe('platform overview operational model', () => {
  it('localizes canonical capability, governance, and operation labels', () => {
    expect(formatPlatformOverviewCapability('custom.resources', 'Custom resources', 'zh_CN')).toBe(
      '自定义资源',
    )
    expect(
      formatPlatformOverviewText('Governance audit success: identity.provider.delete', 'zh_CN'),
    ).toBe('治理审计成功：删除身份提供商')
    expect(formatPlatformOverviewText('identity.provider.delete', 'zh_CN')).toBe('删除身份提供商')
    expect(formatPlatformOverviewText('resource creation batch succeeded', 'zh_CN')).toBe(
      '资源创建批次成功',
    )
    expect(formatPlatformOverviewText('deleted docker host', 'zh_CN')).toBe('已删除 Docker 主机')
    expect(formatPlatformOverviewText('logs.query', 'zh_CN')).toBe('日志查询')
    expect(formatPlatformOverviewText('OpenSoha · platform.resource_creation.batch', 'zh_CN')).toBe(
      'OpenSoha · 资源创建批次',
    )
    expect(formatPlatformOverviewText('custom user message', 'zh_CN')).toBe('custom user message')
    expect(formatPlatformOverviewText('resource creation batch succeeded', 'en_US')).toBe(
      'resource creation batch succeeded',
    )
    expect(
      formatPlatformOverviewAlertScope(
        { clusterId: 'cluster-a', namespace: 'prod', summary: 'logs' } as AlertEvent,
        'e2e-log-k3s-01',
        'zh_CN',
      ),
    ).toBe('集群：e2e-log-k3s-01 · 命名空间：prod')
  })

  it('bounds and structures evidence passed to AI operations', () => {
    const evidence = buildOperationsAIEvidence({
      capacity: { readyNodes: 2, totalNodes: 3, unschedulableNodes: 1, signalNodes: 3 },
      connectionMode: 'agent',
      governance: { approvalRequired: 2, highRisk: 1, partial: 1, supported: 4, items: [] },
      coverage: [
        { key: 'network-policy', count: 2 },
        { key: 'hpa', count: undefined },
      ],
      timeline: Array.from({ length: 8 }, (_, index) => ({
        id: String(index),
        source: 'kubernetes' as const,
        title: `event-${index}`,
        description: 'warning',
        status: 'Warning',
        timestamp: `2026-08-22T10:0${index}:00Z`,
        path: '/',
      })),
    })

    expect(evidence.recentEvidence).toHaveLength(5)
    expect(evidence.coverage).toEqual({ 'network-policy': 2 })
    expect(evidence.governance.approvalRequired).toBe(2)
  })

  it('separates peak node usage from Requests allocation without averaging away saturation', () => {
    const nodes: ClusterNode[] = [
      {
        name: 'node-a',
        status: 'Ready',
        unschedulable: false,
        roles: [],
        podCount: 5,
        ageSeconds: 1,
        resources: { usagePercentages: { cpu: 42, memory: 61, pods: 30 } },
      },
      {
        name: 'node-b',
        status: 'NotReady',
        unschedulable: true,
        roles: [],
        podCount: 8,
        ageSeconds: 1,
        resources: { requestPercentages: { cpu: 91, memory: 77, pods: 63 } },
      },
    ]

    expect(summarizeNodeCapacity(nodes)).toEqual({
      cpuPercent: 42,
      memoryPercent: 61,
      cpuRequestPercent: 91,
      memoryRequestPercent: 77,
      podPercent: 63,
      readyNodes: 1,
      totalNodes: 2,
      unschedulableNodes: 1,
      signalNodes: 2,
    })
    expect(summarizeNodeCapacity([nodes[1]])).toMatchObject({
      cpuPercent: undefined,
      memoryPercent: undefined,
      cpuRequestPercent: 91,
      memoryRequestPercent: 77,
    })
    expect(
      summarizeNodeCapacity([
        { ...nodes[0], resources: { usagePercentages: { cpu: 0, memory: 0 } } },
      ]),
    ).toMatchObject({ cpuPercent: 0, memoryPercent: 0 })
  })

  it('merges Kubernetes, audit, and operation evidence in descending time order', () => {
    const events: KubernetesClusterEvent[] = [
      {
        name: 'event-a',
        namespace: 'prod',
        type: 'Warning',
        reason: 'BackOff',
        involvedKind: 'Pod',
        involvedName: 'api-0',
        message: 'container restarted',
        count: 1,
        lastTimestamp: '2026-08-22T10:00:00Z',
        ageSeconds: 1,
      },
    ]
    const audits = [
      {
        id: 'audit-a',
        createdAt: '2026-08-22T10:02:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        action: 'update',
        resourceKind: 'Deployment',
        resourceName: 'api',
        result: 'success',
        summary: 'updated deployment',
      },
      {
        id: 'audit-read',
        createdAt: '2026-08-22T10:03:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        action: 'list',
        resourceKind: 'Pod',
        resourceName: '',
        result: 'success',
        summary: 'listed pods via cache',
        requestMethod: 'GET',
      },
    ] satisfies AuditLog[]
    const operations = [
      {
        id: 'operation-a',
        createdAt: '2026-08-22T10:01:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        operationType: 'platform.resource.apply',
        targetScope: {},
        result: 'success',
        summary: 'applied manifest',
      },
    ] satisfies OperationLog[]

    const timeline = buildActivityTimeline(events, audits, operations)
    expect(timeline.map((item) => item.source)).toEqual(['audit', 'operation', 'kubernetes'])
    expect(timeline.some((item) => item.id === 'audit:audit-read')).toBe(false)
    expect(timeline[2].path).toBe('/workloads/pods/api-0?namespace=prod')
  })

  it('collapses operation lifecycle entries and exposes their Kubernetes targets', () => {
    const operations = [
      {
        id: 'batch-running',
        createdAt: '2026-08-22T10:00:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        operationType: 'platform.resource_creation.batch',
        targetScope: { targetId: 'batch-a' },
        result: 'running',
        summary: 'resource creation batch running',
        metadata: {
          documents: [
            { kind: 'Deployment', name: 'api', namespace: 'prod' },
            { kind: 'Service', name: 'api', namespace: 'prod' },
          ],
        },
      },
      {
        id: 'batch-success',
        createdAt: '2026-08-22T10:00:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        operationType: 'platform.resource_creation.batch',
        targetScope: { targetId: 'batch-a' },
        result: 'succeeded',
        summary: 'resource creation batch succeeded',
        metadata: {
          documents: [
            { kind: 'Deployment', name: 'api', namespace: 'prod' },
            { kind: 'Service', name: 'api', namespace: 'prod' },
          ],
        },
      },
      {
        id: 'resource-success',
        createdAt: '2026-08-22T10:01:00Z',
        actorId: 'user-a',
        actorName: 'Alice',
        operationType: 'platform.resource_creation.resource',
        targetScope: {
          namespace: 'prod',
          resourceKind: 'Deployment',
          resourceName: 'api',
        },
        result: 'success',
        summary: 'created resource from manifest',
        metadata: { operationId: 'batch-a' },
      },
    ] satisfies OperationLog[]

    const timeline = buildActivityTimeline([], [], operations)

    expect(timeline.map((item) => item.id)).toEqual(['operation:batch-success'])
    expect(timeline[0].description).toBe('Deployment prod/api · Service prod/api')
  })

  it('keeps only alerts assigned to the active Kubernetes cluster', () => {
    const alerts = [
      { id: 'global', clusterId: undefined },
      { id: 'other', clusterId: 'cluster-b' },
      { id: 'active', clusterId: 'cluster-a' },
    ] as AlertEvent[]

    expect(filterAlertsForCluster(alerts, 'cluster-a').map((item) => item.id)).toEqual(['active'])
    expect(filterAlertsForCluster(alerts, null)).toEqual([])
  })

  it('projects approval and risk metadata for the active cluster mode', () => {
    const entries: ClusterCapabilityMatrixEntry[] = [
      {
        key: 'workload.mutations',
        label: 'Workload mutations',
        category: 'workloads',
        riskLevel: 'mutate',
        requiresApproval: true,
        direct: { status: 'available' },
        agent: { status: 'partial' },
      },
      {
        key: 'pod.exec',
        label: 'Pod exec',
        category: 'workloads',
        riskLevel: 'execute',
        requiresApproval: true,
        direct: { status: 'available' },
        agent: { status: 'available' },
      },
    ]

    expect(summarizeCapabilityGovernance(entries, 'agent')).toMatchObject({
      approvalRequired: 2,
      highRisk: 1,
      partial: 1,
      supported: 2,
    })
  })

  it('detects version drift, stale health checks, and prioritizes active alerts', () => {
    const clusters = [
      {
        id: 'a',
        name: 'A',
        region: 'local',
        environment: 'prod',
        labels: {},
        connectionMode: 'agent',
        version: 'v1.35.1',
        health: { status: 'healthy', lastChecked: '2026-08-22T09:30:00Z' },
      },
      {
        id: 'b',
        name: 'B',
        region: 'local',
        environment: 'prod',
        labels: {},
        connectionMode: 'direct_kubeconfig',
        version: 'v1.34.3',
        health: { status: 'unhealthy', lastChecked: '2026-08-22T09:59:00Z' },
      },
    ] satisfies Cluster[]
    const alerts = [
      {
        id: 'warning',
        sourceType: 'prometheus',
        fingerprint: 'warning',
        title: 'Warning',
        summary: '',
        severity: 'warning',
        status: 'firing',
        createdAt: '2026-08-22T09:00:00Z',
        updatedAt: '2026-08-22T09:59:00Z',
      },
      {
        id: 'critical',
        sourceType: 'prometheus',
        fingerprint: 'critical',
        title: 'Critical',
        summary: '',
        severity: 'critical',
        status: 'firing',
        createdAt: '2026-08-22T09:00:00Z',
        updatedAt: '2026-08-22T09:58:00Z',
      },
      {
        id: 'resolved',
        sourceType: 'prometheus',
        fingerprint: 'resolved',
        title: 'Resolved',
        summary: '',
        severity: 'critical',
        status: 'resolved',
        createdAt: '2026-08-22T09:00:00Z',
        updatedAt: '2026-08-22T09:59:00Z',
      },
    ] satisfies AlertEvent[]

    expect(summarizeFleetReadiness(clusters, Date.parse('2026-08-22T10:00:00Z'))).toEqual({
      healthy: 1,
      unhealthy: 1,
      stale: 1,
      versions: 2,
    })
    expect(selectActiveAlerts(alerts).map((alert) => alert.id)).toEqual(['critical', 'warning'])
  })
})
