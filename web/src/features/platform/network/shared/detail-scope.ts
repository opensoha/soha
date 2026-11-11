import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { useSearchParams } from 'react-router-dom'
import { resolveNetworkDetailScope } from './scope'

export function useNetworkDetailScope() {
  const [params] = useSearchParams()
  const selected = usePlatformScopeStore()
  return resolveNetworkDetailScope(selected, params)
}
