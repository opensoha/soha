import { test, expect, verifyPage } from '../fixtures/mock-app'

test('CDP migration: clusters and namespace-scoped Deployment deep link', async ({
  page,
  evidence,
}) => {
  await page.goto('/clusters')
  await verifyPage(page, '/clusters', ['prod-cluster', 'v1.31.0'])
  const route = '/workloads/deployments/api?clusterId=cluster-a&namespace=monitoring'
  await page.goto(route)
  await verifyPage(page, route, ['api', 'monitoring', '副本与发布'])
  expect(evidence.requests).toContain(
    '/api/v1/clusters/cluster-a/workloads/deployments/api/detail?namespace=monitoring',
  )
})
