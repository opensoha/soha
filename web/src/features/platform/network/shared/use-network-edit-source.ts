import { readableQueryData } from '@/utils/query-feedback'
import { useQuery } from '@tanstack/react-query'
import { useEffect, useState } from 'react'
import { getNetworkYAML } from './api'
import { networkQueries } from './queries'
import type { NetworkTarget } from './types'

export function useNetworkEditSource(
  kind: 'ingresses' | 'services',
  { scope, name }: NetworkTarget,
  changedMessage: string,
) {
  const query = useQuery({ ...networkQueries.yaml(kind, scope, name), refetchOnMount: 'always' })
  const content = readableQueryData(query)?.content
  const [source, setSource] = useState<string>()
  useEffect(() => {
    if (content && !query.isFetching && !query.isError) {
      setSource((current) => current ?? content)
    }
  }, [content, query.isFetching, query.isError])

  return {
    query,
    source,
    sourceChanged: Boolean(source && content && source !== content),
    async requireCurrentSource() {
      if (!source || (await getNetworkYAML(kind, { scope, name }))?.content !== source) {
        throw new Error(changedMessage)
      }
    },
  }
}
