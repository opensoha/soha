import { test, expect } from '../fixtures/real-app'

// Dedicated real target only: requests pass through to the attested Core and K3s/Agent.
test('Task114 real import identity, idempotence and scoped denials', async ({ real }) => {
  const { target, api } = real
  expect(target.scenario).toBe('task114')
  if (target.role === 'test-writer') expect(process.env.SOHA_E2E_MUTATIONS).toBe('1')
  const env = target.fixtures.environments.test
  const base = `/api/v1/clusters/${target.clusterId}`
  for (const workload of env.importWorkloads) {
    const query = new URLSearchParams({
      clusterId: target.clusterId,
      namespace: env.namespace,
      workloadKind: workload.workloadKind,
    })
    if (workload.crdName) query.set('crdName', workload.crdName)
    const response = await api.get('/api/v1/application-environments/target-candidates?' + query)
    expect(response.status()).toBe(200)
    const candidate = (await response.json()).items.find(
      (item: { workloadName: string }) => item.workloadName === workload.workloadName,
    )
    expect(candidate.source.resource.uid).toBe(workload.expectedUid)
    expect(candidate.source.specDigest).toBe(workload.expectedSpecDigest)
    expect(JSON.stringify(candidate)).not.toContain('TASK114_SYNTHETIC_ROOT_CANARY')
  }
  const input = {
    clusterId: target.clusterId,
    namespace: env.namespace,
    applicationKey: target.fixtures.application.key,
    applicationName: target.fixtures.application.name,
    environmentKey: 'test',
    environmentName: 'Task114 test',
    ownershipMode: 'observe_only',
    workloads: env.importWorkloads,
  }
  const before = await api.get(
    `/api/v1/application-environments/${env.imported.applicationEnvironmentId}`,
  )
  expect(before.status()).toBe(200)
  const snapshot = (await before.json()).data
  const imported = await api.post('/api/v1/application-environments/imports', { data: input })
  if (target.role === 'readonly') expect(imported.status()).toBe(403)
  else {
    expect(imported.status()).toBe(200)
    const second = (await imported.json()).data
    expect(second.applicationEnvironmentId).toBe(snapshot.id)
    const after = await api.get(`/api/v1/application-environments/${snapshot.id}`)
    expect((await after.json()).data.targets).toEqual(snapshot.targets)
    for (const field of ['expectedUid', 'expectedSpecDigest']) {
      const stale = structuredClone(input)
      stale.workloads = [structuredClone(input.workloads[0])]
      stale.workloads[0][field] = field === 'expectedUid' ? 'replaced-uid' : '0'.repeat(64)
      expect(
        (await api.post('/api/v1/application-environments/imports', { data: stale })).status(),
      ).toBe(409)
    }
  }
  expect(
    (
      await api.get(
        '/api/v1/application-environments/target-candidates?' +
          new URLSearchParams({
            clusterId: target.deniedClusterId,
            namespace: env.namespace,
            workloadKind: 'Deployment',
          }),
      )
    ).status(),
  ).toBe(403)
  expect((await api.get(base + '/workloads/pods?namespace=default')).status()).toBe(403)
  const otherMode = target.mode === 'e2e-direct' ? 'agent' : 'direct'
  const apps = await api.get('/api/v1/applications')
  expect(apps.status()).toBe(200)
  expect(
    (await apps.json()).items.every(
      (item: { key: string }) => !item.key.startsWith('task114-' + otherMode),
    ),
  ).toBe(true)
})

test('Task114 real generic CR chain, YAML boundary and Pod UID guard', async ({ real, page }) => {
  const { target, api } = real
  const env = target.fixtures.environments.test
  const path = `/api/v1/application-environments/${env.imported.applicationEnvironmentId}/targets/${env.rootTargetId}`
  const response = await api.get(path + '/relations')
  expect(response.status()).toBe(200)
  const graph = (await response.json()).data
  const serialized = JSON.stringify(graph)
  for (const uid of [env.rootUid, env.childUid, env.childDeploymentUid])
    expect(serialized).toContain(uid)
  for (const [fromUid, toUid] of [
    [env.rootUid, env.childUid],
    [env.childUid, env.childDeploymentUid],
  ])
    expect(graph.edges).toContainEqual(expect.objectContaining({ fromUid, toUid, type: 'owns' }))
  expect(graph.edges.some((edge: { type: string }) => edge.type === 'references')).toBe(true)
  expect(serialized).toContain('ReplicaSet')
  expect(serialized).toContain('Pod')
  expect(serialized).not.toContain('TASK114_SYNTHETIC_ROOT_CANARY')
  const source = await api.get(path + '/source')
  expect(source.status()).toBe(target.role === 'test-writer' ? 200 : 403)
  if (source.ok())
    expect((await source.json()).data.content.includes('TASK114_SYNTHETIC_ROOT_CANARY')).toBe(true)
  const pods = await api.get(
    `/api/v1/clusters/${target.clusterId}/workloads/pods?namespace=${env.namespace}`,
  )
  expect(pods.status()).toBe(200)
  const pod = (await pods.json()).items.find(
    (item: { name: string; phase: string }) =>
      item.name.startsWith('native-api-') && item.phase === 'Running',
  )
  expect(pod.uid).toBeTruthy()
  const logs = `/api/v1/clusters/${target.clusterId}/workloads/pods/${pod.name}/logs`
  const params = new URLSearchParams({
    namespace: env.namespace,
    container: 'sidecar',
    expectedUid: pod.uid,
  })
  const current = await api.get(logs + '?' + params)
  expect(current.status()).toBe(target.role === 'test-writer' ? 200 : 403)
  if (current.ok())
    expect((await current.json()).data.content.includes('task114-sidecar')).toBe(true)
  params.set('expectedUid', 'replaced-uid')
  expect((await api.get(logs + '?' + params)).status()).toBe(
    target.role === 'test-writer' ? 409 : 403,
  )
  const terminalPath = logs.replace(/\/logs$/, '/terminal') + '?' + params
  const ticket = await api.post('/api/v1/auth/stream-ticket', { data: { path: terminalPath } })
  // Ticket issuance pins identity/path. Resource permission and UID are checked in the stream.
  expect(ticket.status()).toBe(201)
  const streamURL = new URL(terminalPath, target.baseURL)
  streamURL.protocol = streamURL.protocol === 'https:' ? 'wss:' : 'ws:'
  streamURL.searchParams.set('stream_ticket', (await ticket.json()).data.ticket)
  const messages = await page.evaluate(
    (url) =>
      new Promise<Array<{ type: string; data?: string; channel?: string }>>((resolve, reject) => {
        const socket = new WebSocket(url)
        const frames: Array<{ type: string; data?: string; channel?: string }> = []
        const timer = setTimeout(() => {
          socket.close()
          reject(new Error('Denied terminal did not close'))
        }, 10_000)
        socket.onmessage = (event) => frames.push(JSON.parse(String(event.data)))
        socket.onerror = () => {
          clearTimeout(timer)
          reject(new Error('Terminal probe failed before business rejection'))
        }
        socket.onclose = () => {
          clearTimeout(timer)
          resolve(frames)
        }
      }),
    streamURL.toString(),
  )
  expect(messages.some((message) => message.type === 'error')).toBe(true)
  expect(messages.some((message) => message.type === 'output' || message.channel)).toBe(false)
})
