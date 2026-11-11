import { useAuthStore } from '@/stores/auth-store'
import { AIWorkbenchController } from './controller'

export { RUNNABLE_ANALYSIS_MODE_OPTIONS } from './mode'

export function AIWorkbenchPage() {
  const owner = useAuthStore((state) => state.user?.userId)
  return <AIWorkbenchController key={owner} />
}
