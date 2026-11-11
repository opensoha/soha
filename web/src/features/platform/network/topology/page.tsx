import { lazy, Suspense } from 'react'
import { Card, Spin } from 'antd'

const NetworkTopologyRuntimePage = lazy(async () => {
  const module = await import('./runtime-page')
  return { default: module.NetworkTopologyRuntimePage }
})

export function NetworkTopologyPage() {
  return (
    <Suspense
      fallback={
        <Card className="soha-detail-card">
          <Spin size="large" />
        </Card>
      }
    >
      <NetworkTopologyRuntimePage />
    </Suspense>
  )
}
