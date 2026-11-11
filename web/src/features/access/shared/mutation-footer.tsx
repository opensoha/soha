import type { ReactNode } from 'react'
import { Button, type ModalProps } from 'antd'
import { OperationFeedback } from '@/components/request-feedback'
import { localeText, useI18n } from '@/i18n'
import { writeOutcomeUnknown } from '@/utils/query-feedback'

export const accessMutationModalStyles: ModalProps['styles'] = {
  container: {
    display: 'flex',
    flexDirection: 'column',
    maxHeight: 'calc(100dvh - 64px)',
    overflow: 'hidden',
  },
  header: {
    minHeight: 32,
    display: 'flex',
    alignItems: 'center',
    marginBottom: 16,
    paddingBottom: 0,
  },
  body: {
    minHeight: 0,
    maxHeight: 'calc(100dvh - 300px)',
    overflowY: 'auto',
    scrollbarGutter: 'stable',
  },
  footer: {
    flexShrink: 0,
    maxHeight: '50dvh',
    overflowY: 'auto',
    marginTop: 16,
    paddingTop: 0,
  },
}

interface AccessMutationFooterProps {
  children: ReactNode
  error: Error | null
  saving: boolean
  target?: string
  onCheck?: () => void
}

export function AccessMutationFooter({
  children,
  error,
  saving,
  target,
  onCheck,
}: AccessMutationFooterProps) {
  const { localeCode } = useI18n()
  return (
    <div className="soha-access-mutation-footer">
      <OperationFeedback
        pending={saving}
        error={error}
        label={localeText(localeCode, '保存', 'Save')}
        target={target}
        receipt={
          error && onCheck
            ? {
                message: '',
                evidence: (
                  <Button size="small" onClick={onCheck}>
                    {localeText(localeCode, '核对列表记录', 'Check list records')}
                  </Button>
                ),
              }
            : undefined
        }
      />
      {writeOutcomeUnknown(error) ? (
        <p>
          {localeText(
            localeCode,
            '输入已保留。重读列表不会确认原请求；结果未确认前继续暂停提交。',
            'Your input is preserved. Reading the list does not confirm the original request; submission remains paused until it is verified.',
          )}
        </p>
      ) : null}
      <div className="soha-access-mutation-actions">
        <div className="soha-access-mutation-buttons">{children}</div>
      </div>
    </div>
  )
}
