import { lazy, Suspense } from 'react'
import { Spin } from 'antd'
import type { AISettingsPageProps } from '../types'

const AISettingsPageContent = lazy(async () => {
  const module = await import('./page')
  return { default: module.AISettingsPage }
})

export function AISettingsPage(props: AISettingsPageProps) {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-64 items-center justify-center">
          <Spin size="large" />
        </div>
      }
    >
      <AISettingsPageContent {...props} />
    </Suspense>
  )
}
