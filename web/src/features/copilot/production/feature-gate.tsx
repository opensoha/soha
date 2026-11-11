import { ReadFeedback } from '@/components/request-feedback'
import { areWorkbenchModuleFeaturesEnabled, useModuleStatuses } from '@/features/modules'
import { readableQueryData } from '@/utils/query-feedback'
import { Result, Spin } from 'antd'
import type { ReactNode } from 'react'

export function AIWorkbenchFeatureGate({
  children,
  features,
}: {
  children: ReactNode
  features: string[]
}) {
  const query = useModuleStatuses()
  if (query.isPending) {
    return <Spin fullscreen />
  }
  if (query.isError && !readableQueryData(query))
    return <ReadFeedback label="AI 能力状态" query={query} />
  if (!areWorkbenchModuleFeaturesEnabled(readableQueryData(query), 'ai', features)) {
    return <Result status="404" title="功能未启用" subTitle="当前环境未启用此 AI 能力。" />
  }
  return (
    <>
      <ReadFeedback label="AI 能力状态" query={query} />
      {children}
    </>
  )
}
