import { test, expect, type WebSocketRoute } from '@playwright/test'
import { installMocks, verifyBrowserEvidence } from '../fixtures/mock-app'
import { task114Response } from '../fixtures/task114-delivery'
import { expectSessionToolbarSpacing } from '../fixtures/session-dock-geometry'

const target =
  '/applications/retail?tab=services&applicationEnvironmentId=retail-test&serviceId=order-api&serviceTab=pods'

for (const kind of ['logs', 'terminal'] as const) {
  test(`Task114 ${kind}: minimize retains connection; scope exit, reload and pagehide require explicit restore`, async ({
    context,
    page,
  }) => {
    let uid = 'uid-order-api-pod-0'
    let permitted = true
    let detailReads = 0
    let permissionReads = 0
    const tickets: string[] = []
    const opened: Array<{ url: string; closed: boolean; input: string[] }> = []
    const evidence = await installMocks(context, page, (url, method, request) => {
      const path = url.pathname.replace(/^\/api\/v1/, '')
      const data = (value: unknown) => ({ body: { data: value } })
      if (path === '/auth/stream-ticket' && method === 'POST') {
        tickets.push(request.postDataJSON().path as string)
        return data({ ticket: 'synthetic-one-use-ticket', expiresAt: '2099-01-01T00:00:00Z' })
      }
      if (path === '/access/permission-snapshot') {
        permissionReads += 1
        const selected = task114Response(url, method)!
        const body = selected.body as { data: { permissionKeys: string[] } }
        return data({
          ...body.data,
          permissionKeys: [
            ...body.data.permissionKeys,
            ...(permitted ? ['platform.pods.logs', 'platform.pods.exec'] : []),
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
      if (path === '/clusters/cluster-a/workloads/pods/order-api-pod-0/detail') {
        detailReads += 1
        return data({
          name: 'order-api-pod-0',
          namespace: 'retail-test',
          uid,
          phase: 'Running',
          allowedActions: ['logs', 'exec'],
          containers: [
            { name: 'main', role: 'main', image: 'test:1', ready: true, restartCount: 0 },
            { name: 'sidecar', role: 'sidecar', image: 'test:1', ready: true, restartCount: 0 },
          ],
        })
      }
      if (path === '/clusters/cluster-a/workloads/pods/order-api-pod-0/logs') {
        expect(url.searchParams.get('expectedUid')).toBe('uid-order-api-pod-0')
        return data({
          content: 'session-output-only',
          podName: 'order-api-pod-0',
          namespace: 'retail-test',
        })
      }
      const selected = task114Response(url, method)
      if (selected && path.endsWith('/runtime')) {
        const body = selected.body as { data: { pods?: Array<{ name: string }> } }
        if (body.data.pods)
          return data({
            ...body.data,
            pods: body.data.pods.map((pod) => ({
              ...pod,
              uid: `uid-${pod.name}`,
              allowedActions: ['logs', 'exec'],
            })),
          })
      }
      return selected
    })
    await page.routeWebSocket(
      /\/workloads\/pods\/[^/]+\/(logs\/stream|terminal)/,
      (socket: WebSocketRoute) => {
        const entry = { url: socket.url(), closed: false, input: [] as string[] }
        opened.push(entry)
        socket.onClose(() => {
          entry.closed = true
        })
        socket.onMessage((message) => {
          entry.input.push(message.toString())
        })
        socket.send(
          JSON.stringify(
            kind === 'logs'
              ? { type: 'log', data: 'session-output-only' }
              : { type: 'stdout', data: 'session-output-only\r\n' },
          ),
        )
      },
    )
    await page.goto(target)
    const pod = page.locator('.soha-pod-card-list > li').filter({ hasText: 'order-api-pod-0' })
    await pod.getByRole('button', { name: kind === 'logs' ? '日志' : '终端', exact: true }).click()
    await expect.poll(() => opened.length).toBe(1)
    await expectSessionToolbarSpacing(page.getByRole('region', { name: '实时会话', exact: true }))
    expect(new URL(opened[0].url).searchParams.get('expectedUid')).toBe('uid-order-api-pod-0')
    expect(tickets.every((path) => path.includes('expectedUid=uid-order-api-pod-0'))).toBe(true)
    await page.getByRole('button', { name: '收起实时会话', exact: true }).click()
    expect(opened[0].closed).toBe(false)
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    expect(opened).toHaveLength(1)
    await page
      .getByRole('navigation', { name: '服务列表', exact: true })
      .getByRole('button', { name: /支付/ })
      .click()
    expect(opened[0].closed).toBe(false)
    await page
      .getByRole('navigation', { name: '服务列表', exact: true })
      .getByRole('button', { name: /订单/ })
      .click()
    expect(opened).toHaveLength(1)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /生产环境/ })
      .click()
    await expect.poll(() => opened[0].closed).toBe(true)
    await page
      .getByRole('navigation', { name: '选择环境', exact: true })
      .getByRole('button', { name: /测试环境/ })
      .click()
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    expect(opened).toHaveLength(1)
    const reads = { detailReads, permissionReads }
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect.poll(() => opened.length).toBe(2)
    expect(detailReads).toBeGreaterThan(reads.detailReads)
    expect(permissionReads).toBeGreaterThan(reads.permissionReads)
    await page.getByRole('button', { name: '切换应用', exact: true }).click()
    await page
      .getByRole('region', { name: '应用切换面板', exact: true })
      .getByRole('button', { name: /实时计算/ })
      .click()
    await expect.poll(() => opened[1].closed).toBe(true)
    await page.getByRole('button', { name: '切换应用', exact: true }).click()
    await page
      .getByRole('region', { name: '应用切换面板', exact: true })
      .getByRole('button', { name: /零售业务/ })
      .click()
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    expect(opened).toHaveLength(2)
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect.poll(() => opened.length).toBe(3)
    await page.reload()
    await expect.poll(() => opened[2].closed).toBe(true)
    await page.getByRole('button', { name: /实时会话 \(1\)/ }).click()
    await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    expect(opened).toHaveLength(3)
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect.poll(() => opened.length).toBe(4)
    await page.evaluate(() =>
      window.dispatchEvent(new PageTransitionEvent('pagehide', { persisted: true })),
    )
    await expect.poll(() => opened[3].closed).toBe(true)
    await page.evaluate(() =>
      window.dispatchEvent(new PageTransitionEvent('pageshow', { persisted: true })),
    )
    await expect(page.getByRole('button', { name: '重新连接', exact: true })).toBeVisible()
    uid = 'replacement-uid'
    await page.getByRole('button', { name: '重新连接', exact: true }).click()
    await expect(page.locator('.soha-realtime-session-dock')).toContainText('Pod 已被替换')
    expect(opened).toHaveLength(4)
    uid = 'uid-order-api-pod-0'
    permitted = false
    await page.getByRole('button', { name: '重新校验并连接', exact: true }).click()
    await expect(page.locator('.soha-realtime-session-dock')).toContainText('当前没有会话访问权限')
    expect(opened).toHaveLength(4)
    const storage = await page.evaluate(() => Object.values(localStorage).join(''))
    expect(storage).toContain('uid-order-api-pod-0')
    expect(storage).not.toMatch(/session-output-only|synthetic-one-use-ticket|replacement-uid/)
    expect(
      opened
        .flatMap((entry) => entry.input)
        .filter((message) => JSON.parse(message).type === 'input'),
    ).toEqual([])
    verifyBrowserEvidence(evidence)
  })
}
