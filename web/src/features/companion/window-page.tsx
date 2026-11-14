import { useEffect } from 'react'
import { usePermissionSnapshot } from '@/features/auth'
import { GlobalAIAssistantProvider } from '@/features/copilot'
import { AssistantCompanionOverlay } from './assistant-companion-overlay'

export function CompanionWindowPage() {
  const permissionSnapshotQuery = usePermissionSnapshot()

  useEffect(() => {
    document.documentElement.classList.add('soha-companion-window-document')
    document.body.classList.add('soha-companion-window-document')
    return () => {
      document.documentElement.classList.remove('soha-companion-window-document')
      document.body.classList.remove('soha-companion-window-document')
    }
  }, [])

  return (
    <GlobalAIAssistantProvider
      enabled
      nativeCompanionWindow
      permissionSnapshot={permissionSnapshotQuery.data?.data}
      renderCompanion={(props) => <AssistantCompanionOverlay {...props} />}
    >
      <main aria-label="Soha 桌面宠物" className="soha-companion-window-root" />
    </GlobalAIAssistantProvider>
  )
}
