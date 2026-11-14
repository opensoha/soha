import { Alert, Button } from 'antd'
import { YamlDraftDiffEditor } from '@/components/yaml-draft-diff-editor'
import { useI18n } from '@/i18n'

interface HelmReleaseValuesPanelProps {
  applyDisabled: boolean
  applying: boolean
  canEdit: boolean
  draft: string
  error?: Error | null
  onApply: () => void
  onChange: (value: string) => void
  onReset: () => void
  serverChanged?: boolean
  onCompareLatest?: () => void
  original: string
}

export function HelmReleaseValuesPanel({
  applyDisabled,
  applying,
  canEdit,
  draft,
  error,
  onApply,
  onChange,
  onReset,
  original,
  serverChanged,
  onCompareLatest,
}: HelmReleaseValuesPanelProps) {
  const { localeCode } = useI18n()
  if (error) {
    return (
      <Alert
        type="error"
        showIcon
        title={localeCode === 'zh_CN' ? 'values.yaml 加载失败' : 'Failed to load values.yaml'}
        description={error.message}
      />
    )
  }
  return (
    <>
      {serverChanged ? (
        <Alert
          type="warning"
          showIcon
          title={
            localeCode === 'zh_CN'
              ? '运行值已更新，草稿已保留'
              : 'Runtime values changed; draft retained'
          }
          action={
            <Button onClick={onCompareLatest} disabled={applying}>
              {localeCode === 'zh_CN' ? '比对最新值' : 'Compare latest'}
            </Button>
          }
        />
      ) : null}
      <YamlDraftDiffEditor
        baselineOnLeft
        title="values.yaml"
        description={
          localeCode === 'zh_CN'
            ? '比对已提交参数与修改后的草稿，确认后更新 Release。'
            : 'Compare submitted values with your draft before updating the release.'
        }
        original={original}
        modified={draft}
        onChange={onChange}
        onReset={applying ? undefined : onReset}
        onApply={canEdit ? onApply : undefined}
        applying={applying}
        applyDisabled={applyDisabled}
        leftLabel={localeCode === 'zh_CN' ? '已提交参数' : 'Submitted values'}
        rightLabel={localeCode === 'zh_CN' ? '更新草稿' : 'Draft'}
        editable={canEdit && !applying}
        saveDisabled
      />
    </>
  )
}
