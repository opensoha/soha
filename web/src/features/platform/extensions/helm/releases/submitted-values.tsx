import { Card } from 'antd'
import { useI18n } from '@/i18n'

export function SubmittedValues({ content, revision }: { content: string; revision?: string }) {
  const { localeCode } = useI18n()
  const zh = localeCode === 'zh_CN'

  return (
    <Card
      className="soha-detail-card"
      title={zh ? '当前提交的 Values' : 'Submitted values'}
      extra={
        revision ? (
          <span style={{ fontSize: 'var(--soha-font-size-supporting)' }}>Revision {revision}</span>
        ) : undefined
      }
      style={{ minWidth: 0, overflow: 'hidden' }}
    >
      <pre
        aria-label={zh ? '当前提交的 Values YAML' : 'Submitted values YAML'}
        tabIndex={0}
        style={{ margin: 0, maxHeight: '60vh', maxWidth: '100%', overflow: 'auto' }}
      >
        <code className="language-yaml">{content}</code>
      </pre>
    </Card>
  )
}
