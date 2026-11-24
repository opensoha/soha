import { expect, test } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'

const consumer = 'retail-test'
const origin = 'retail-test-source'
const service = 'order-api'
const targetId = 'origin-target'
const referenceId = 'reference-1'
const namespace = 'retail-source'
const podName = 'origin-pod'
const podUid = 'pod-uid-original'
const root = {
  clusterId: 'cluster-a',
  namespace,
  kind: 'Deployment',
  apiVersion: 'apps/v1',
  name: 'order-origin',
  uid: 'root-uid',
  scopeMode: 'namespace',
}
const pod = { ...root, name: podName, uid: podUid, kind: 'Pod', apiVersion: 'v1' }
const importTarget = {
  targetId,
  clusterId: root.clusterId,
  namespace,
  workloadKind: root.kind,
  workloadName: root.name,
  ownershipMode: 'observe_only',
  resources: [{ resource: root, specDigest: 'a'.repeat(64) }],
}
const view = { applicationEnvironmentId: consumer, environmentKey: 'test' }
const source = { applicationEnvironmentId: origin, environmentKey: 'test-source' }
const collection = `/application-environments/${consumer}/service-references`
const exactTarget = `${collection}/${service}/targets/${targetId}`

for (const [kind, sourceType] of [
  ['logs', 'import'],
  ['terminal', 'import'],
  ['logs', 'helm'],
] as const) {
  test(`referenced ${sourceType} ${kind} disconnects across environments and revalidates source, ownership and Pod before reconnect`, async ({
    context,
    page,
  }) => {
    let sourceStatus = 200
    let owned = true
    let uid = podUid
    let resolves = 0
    const target =
      sourceType === 'helm'
        ? {
            ...importTarget,
            workloadKind: 'HelmRelease',
            workloadName: 'order-chart',
            helmRevision: 2,
          }
        : importTarget
    const tickets: string[] = []
    const sockets: Array<{ url: string; closed: boolean }> = []
    const evidence = await installMocks(context, page, (url, method, request) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      const data = (value: unknown) => ({ body: { data: value } })
      const base = task114Response(url, method)
      if (path === '/access/permission-snapshot') {
        const original = base?.body as { data: { permissionKeys: string[] } }
        return data({
          ...original.data,
          permissionKeys: [
            ...original.data.permissionKeys,
            'delivery.application-environments.update',
            'delivery.application-environments.view',
            'platform.pods.logs',
            'platform.pods.exec',
          ],
        })
      }
      if (path === '/application-environments')
        return data([
          {
            id: consumer,
            applicationId: 'retail',
            environmentId: 'test',
            targets: [],
          },
        ])
      if (path === collection)
        return {
          body: { items: [{ id: referenceId, revision: 1, serviceId: service, state: 'active' }] },
        }
      if (path === '/application-environments/retail-prod/service-references')
        return { body: { items: [] } }
      if (path === `${collection}/${service}`)
        return data({
          id: referenceId,
          revision: 1,
          preview: {
            applicationId: 'retail',
            serviceId: service,
            consumer: view,
            source,
            targets: [target],
          },
        })
      if (path === exactTarget) {
        resolves += 1
        expect(url.searchParams.get('expectedReferenceId')).toBe(referenceId)
        expect(url.searchParams.get('expectedRevision')).toBe('1')
        if (sourceStatus !== 200)
          return {
            status: sourceStatus,
            body: { error: { code: 'reference_error', message: 'Source unavailable' } },
          }
        return {
          body: {
            applicationId: 'retail',
            serviceId: service,
            referenceId,
            referenceRevision: 1,
            view,
            origin: source,
            target,
          },
        }
      }
      if (path === `/application-environments/${origin}/targets/${targetId}/relations`)
        return data({
          root,
          nodes: [
            { role: 'root', resource: root },
            { role: owned ? 'owned' : 'candidate', resource: pod },
            { role: 'candidate', resource: { ...pod, uid: 'other', name: 'same-selector-only' } },
          ],
          edges: [],
          coverage: [],
        })
      if (path === '/clusters/cluster-a/resources/graph' && sourceType === 'helm') {
        expect(url.searchParams.get('namespace')).toBe(namespace)
        expect(url.searchParams.get('kind')).toBe('Deployment')
        expect(url.searchParams.get('name')).toBe(root.name)
        return data({
          rootId: 'root',
          nodes: [
            { id: 'root', resource: root },
            {
              id: 'rs',
              resource: { ...root, kind: 'ReplicaSet', name: 'order-rs', uid: 'rs-uid' },
            },
            { id: 'pod', resource: pod },
            { id: 'candidate', resource: { ...pod, uid: 'other', name: 'same-selector-only' } },
          ],
          edges: [
            { sourceId: 'root', targetId: 'rs', relation: 'controls' },
            { sourceId: 'rs', targetId: 'pod', relation: owned ? 'owns' : 'selects' },
            { sourceId: 'root', targetId: 'candidate', relation: 'selects' },
          ],
        })
      }
      if (path === '/clusters/capabilities')
        return data(
          ['pod.logs', 'pod.exec'].map((key) => ({
            key,
            riskLevel: 'read',
            requiredScopes: [],
            requiresApproval: false,
            direct: { status: 'available' },
            agent: { status: 'available' },
          })),
        )
      if (path === `/clusters/cluster-a/workloads/pods/${podName}/detail`)
        return data({
          name: podName,
          namespace,
          uid,
          phase: 'Running',
          allowedActions: ['logs', 'exec'],
          containers: [
            { name: 'main', role: 'main', image: 'demo:1', ready: true, restartCount: 0 },
          ],
        })
      if (path === `/clusters/cluster-a/workloads/pods/${podName}/logs`)
        return data({ podName, namespace, content: 'ephemeral-session-output' })
      if (path === '/auth/stream-ticket' && method === 'POST') {
        tickets.push(request.postDataJSON().path as string)
        return data({ ticket: 'ephemeral-stream-ticket', expiresAt: '2099-01-01T00:00:00Z' })
      }
      return base
    })
    await page.routeWebSocket(
      /\/workloads\/pods\/origin-pod\/(logs\/stream|terminal)/,
      (socket) => {
        const opened = { url: socket.url(), closed: false }
        sockets.push(opened)
        socket.onClose(() => {
          opened.closed = true
        })
        socket.send(
          JSON.stringify({
            type: kind === 'logs' ? 'log' : 'stdout',
            data: 'ephemeral-session-output',
          }),
        )
      },
    )
    await page.goto(
      `/applications/retail?tab=services&applicationEnvironmentId=${consumer}&serviceId=${service}&serviceTab=pods`,
    )
    const originPods = page.getByRole('region', { name: '引用来源 Pod' })
    await expect(originPods.getByText('same-selector-only')).toHaveCount(0)
    await originPods
      .getByRole('button', { name: `查看 ${podName} ${kind === 'logs' ? '日志' : '终端'}` })
      .click()
    await expect.poll(() => sockets.length).toBe(1)
    expect(new URL(sockets[0].url).searchParams.get('namespace')).toBe(namespace)
    expect(new URL(sockets[0].url).searchParams.get('expectedUid')).toBe(podUid)
    const initialResolves = resolves

    const environment = page.getByRole('navigation', { name: '选择环境' })
    await environment.getByRole('button', { name: /生产环境/ }).click()
    await expect.poll(() => sockets[0].closed).toBe(true)
    await environment.getByRole('button', { name: /测试环境/ }).click()
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    const dock = page.getByRole('region', { name: '实时会话', exact: true })
    const reconnect = dock.getByRole('button', { name: '重新连接', exact: true })
    const retry = dock.getByRole('button', { name: '重新校验并连接', exact: true })
    await expect(reconnect).toBeVisible()
    expect(resolves).toBe(initialResolves)
    const initialTickets = tickets.length
    sourceStatus = 403
    await reconnect.click()
    await expect(dock).toContainText('当前没有引用来源访问权限')
    sourceStatus = 409
    await retry.click()
    await expect(dock).toContainText('引用来源或 Pod 归属已变化')
    sourceStatus = 200
    owned = false
    const previous = resolves
    await retry.click()
    await expect.poll(() => resolves).toBeGreaterThan(previous)
    await expect(dock).toContainText('引用来源或 Pod 归属已变化')
    owned = true
    uid = 'same-name-replaced'
    await retry.click()
    await expect(dock).toContainText('Pod 已被替换')
    expect(sockets).toHaveLength(1)
    expect(tickets).toHaveLength(initialTickets)
    uid = podUid
    await retry.click()
    await expect.poll(() => sockets.length).toBe(2)
    const storage = await page.evaluate(() => Object.values(localStorage).join(''))
    expect(storage).toContain(referenceId)
    expect(storage).toContain(podUid)
    expect(storage).not.toMatch(
      /ephemeral-session-output|ephemeral-stream-ticket|same-name-replaced/,
    )
    verifyBrowserEvidence(evidence)
  })
}
