import { K8sYamlEditor } from '@/components/k8s-yaml-editor'
import { useI18n } from '@/i18n'
import { App } from 'antd'

export default function StorageYAMLPanel({
  applying,
  blocked,
  error,
  original,
  serverChanged,
  onCompareLatest,
  draft,
  onApply,
  onChange,
  onReset,
  readOnly = false,
}: {
  applying: boolean
  blocked?: boolean
  error?: Error | null
  original?: string
  serverChanged?: boolean
  onCompareLatest?: () => void
  draft: string
  onApply: () => void
  onChange: (value: string) => void
  onReset: () => void
  readOnly?: boolean
}) {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  return (
    <div style={{ height: 620 }}>
      <K8sYamlEditor
        readOnly={readOnly}
        error={error}
        original={original}
        serverChanged={serverChanged}
        onCompareLatest={onCompareLatest}
        value={draft}
        onChange={onChange}
        onReset={onReset}
        onSave={() =>
          void message.info(
            localeCode === 'zh_CN' ? '暂不支持本地草稿' : 'Local draft save disabled here',
          )
        }
        onApply={onApply}
        saveDisabled
        applyDisabled={blocked || Boolean(error) || !draft.trim()}
        applying={applying}
      />
    </div>
  )
}
