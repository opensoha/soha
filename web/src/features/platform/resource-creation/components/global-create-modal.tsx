import { useI18n } from '@/i18n'
import { usePlatformScopeStore } from '@/stores/platform-scope-store'
import { CreateShell } from './create-shell'

const GLOBAL_TEMPLATE = `apiVersion: v1
kind: ConfigMap
metadata:
  name: example-config
data:
  key: value
`

interface GlobalResourceCreateModalProps {
  readonly onClose: () => void
  readonly open: boolean
}

export function GlobalResourceCreateModal({ onClose, open }: GlobalResourceCreateModalProps) {
  const { localeCode } = useI18n()
  const { clusterId } = usePlatformScopeStore()

  return (
    <CreateShell
      context={{
        clusterId: clusterId || '',
        source: 'global_yaml',
      }}
      defaultTemplate={GLOBAL_TEMPLATE}
      onClose={onClose}
      open={open}
      presentation="modal"
      title={localeCode === 'zh_CN' ? '创建资源' : 'Create resources'}
    />
  )
}
