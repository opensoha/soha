import { useSearchParams } from 'react-router-dom'
import { useApplicationEntryData } from './service-workspace-model'

export function ApplicationEntryReads({ applicationId }: { applicationId: string }) {
  const [searchParams] = useSearchParams()
  useApplicationEntryData(applicationId, searchParams)
  return null
}
