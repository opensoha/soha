import { useState } from 'react'
import { Alert, Button, Form, Input, Select, Space, Typography } from 'antd'
import type {
  DLPDataLevel,
  DLPShadowInput,
  DLPShadowResult,
  DLPShadowSample,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useI18n } from '@/i18n'

export function dataLevelOptions(text: (zh: string, en: string) => string) {
  return [
    { value: 'public', label: text('公开', 'Public') },
    { value: 'internal', label: text('内部', 'Internal') },
    { value: 'confidential', label: text('机密', 'Confidential') },
    { value: 'restricted', label: text('严格受限', 'Restricted') },
  ] satisfies { value: DLPDataLevel; label: string }[]
}
export function DataLevelFields({ allowArchives = false }: { allowArchives?: boolean }) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  return (
    <>
      <Form.Item
        name="minimumDataLevel"
        initialValue="public"
        label={text('最低处置等级', 'Minimum enforcement level')}
        extra={text(
          '默认对全部匹配处置；未检查仍按模式处理，不因阈值放行。',
          'By default all matches enforce. Uninspected content still follows the configured mode.',
        )}
      >
        <Select options={dataLevelOptions(text)} />
      </Form.Item>
      <Form.Item
        name="detectorVersion"
        initialValue="content-dlp-text-v1"
        label={text('固定检测器版本', 'Pinned detector version')}
        extra={
          allowArchives
            ? text(
                '选择 archive-v2 可检查有界 ZIP/TAR/GZIP 文本；warn/block 拒绝任一未知成员，audit 仅记录且可能放行。原始及累计展开各≤256KiB、64项、3层、比例≤100:1；暂不支持 Office/PDF/图片。',
                'Choose archive-v2 for bounded ZIP/TAR/GZIP text. Warn/block deny unknown members; audit records and may allow them. Raw and cumulative expansion each ≤256KiB, 64 entries, 3 layers, ratio ≤100:1. Office/PDF/images remain unsupported.',
              )
            : undefined
        }
      >
        <Select
          options={[
            { value: 'content-dlp-text-v1', label: 'content-dlp-text-v1' },
            ...(allowArchives
              ? [{ value: 'content-dlp-archive-v2', label: 'content-dlp-archive-v2' }]
              : []),
          ]}
        />
      </Form.Item>
    </>
  )
}
type Evidence = {
  stepUpRequired?: boolean
  allowed?: boolean
  dataLevel?: string
  minimumDataLevel?: string
  detectorVersion?: string
  ruleSetVersion?: string
  matchedRuleIds?: string[]
}
export function DecisionEvidence({
  decision,
  shadow = false,
}: {
  decision: Evidence
  shadow?: boolean
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const levels = new Map<string, string>([
    ...dataLevelOptions(text).map((x) => [x.value, x.label] as [string, string]),
    ['historical', text('历史记录未提供等级', 'Classification absent in historical record')],
    ['unknown', text('未知（未完整检查）', 'Unknown (not fully inspected)')],
    ['unclassified', text('未分级（不证明公开）', 'Unclassified (not proof of public data)')],
  ])
  return (
    <Space orientation="vertical" size={2}>
      <Typography.Text>
        {text('派生等级', 'Derived level')}:{' '}
        {levels.get(decision.dataLevel ?? 'historical') ?? decision.dataLevel} ·{' '}
        {text('阈值', 'Threshold')}: {levels.get(decision.minimumDataLevel ?? 'public')}
      </Typography.Text>
      <Typography.Text type="secondary">
        {decision.detectorVersion ??
          text('历史记录未提供版本', 'Version absent in historical record')}
      </Typography.Text>
      {decision.ruleSetVersion ? (
        <Typography.Text copyable type="secondary">
          {decision.ruleSetVersion}
        </Typography.Text>
      ) : null}
      {decision.matchedRuleIds?.length ? (
        <Typography.Text>
          {text('命中规则', 'Matched rules')}: {decision.matchedRuleIds.join(', ')}
        </Typography.Text>
      ) : null}
      {decision.stepUpRequired && !decision.allowed ? (
        <Alert
          type="warning"
          title={
            shadow
              ? text('此样本的匹配放行将需要 MFA', 'Releasing this sample would also require MFA')
              : text('放行还需近期 MFA 验证', 'Release also requires recent MFA')
          }
          description={
            shadow ? (
              text(
                '仅试算策略条件，不发起验证或领取授权。',
                'Evaluates policy conditions only; no challenge or authorization is issued.',
              )
            ) : (
              <>
                {text(
                  '在同一登录会话的安全页完成二次验证，再回来确认或领取已批准的授权；验证不替代申诉审批。',
                  'Complete step-up verification in the same signed-in session, then return to acknowledge or claim approval. MFA does not replace appeal review.',
                )}{' '}
                <Typography.Link href="/portal/security" target="_blank" rel="noopener noreferrer">
                  {text('打开安全页', 'Open security settings')}
                </Typography.Link>
              </>
            )
          }
        />
      ) : null}
    </Space>
  )
}
export function DLPShadowPanel({
  revision,
  disabled,
  evaluate,
}: {
  revision: number
  disabled: boolean
  evaluate: (input: DLPShadowInput) => Promise<DLPShadowResult>
}) {
  const { localeCode } = useI18n()
  const text = (zh: string, en: string) => (localeCode === 'zh_CN' ? zh : en)
  const [samples, setSamples] = useState([''])
  const operation = useOperationMutation(
    {
      retry: false,
      gcTime: 0,
      mutationFn: async () => {
        if (samples.some((sample) => !sample.trim()))
          throw new Error(text('请填写非空白样本。', 'Enter nonblank samples.'))
        const encoded = samples.map((sample) => {
          const bytes = new TextEncoder().encode(sample)
          if (bytes.length > 32768)
            throw new Error(
              text('每个影子样本最多32KiB。', 'Each shadow sample is limited to 32KiB.'),
            )
          let raw = ''
          for (let i = 0; i < bytes.length; i += 8192)
            raw += String.fromCharCode(...bytes.subarray(i, i + 8192))
          bytes.fill(0)
          return btoa(raw)
        })
        setSamples([''])
        return evaluate({ expectedRevision: revision, samples: encoded })
      },
    },
    {
      label: ['影子评估已保存草稿', 'Evaluate saved draft in shadow'],
      target: () => `r${revision}`,
      receipt: () => ({ message: text('样本评估完成。', 'Sample evaluation completed.') }),
    },
  )
  const report = !disabled ? operation.data : undefined
  return (
    <Space orientation="vertical" size={8} style={{ width: '100%' }}>
      <Typography.Text strong>
        {text('发布前影子评估', 'Shadow evaluation before publication')}
      </Typography.Text>
      <Alert
        type="info"
        title={text(
          '仅试算已保存版本；样本不会传到目的地。',
          'Evaluates the saved version; samples stay in Core.',
        )}
        description={text(
          '每批1–8个完整样本，每个≤32KiB。只返回去敏决定，不存正文、不发布或触发网络限制。计数仅描述这些样本，不是误报率。',
          '1–8 complete samples per batch, each ≤32KiB. Returns redacted decisions without retaining content, publishing policy or restricting networks. Counts describe these samples, not false-positive rates.',
        )}
      />
      {samples.map((sample, index) => (
        <Input.TextArea
          key={index}
          aria-label={text(`影子样本${index + 1}`, `Shadow sample ${index + 1}`)}
          value={sample}
          rows={3}
          disabled={disabled || operation.blocked}
          onChange={(event) => {
            setSamples((previous) =>
              previous.map((value, i) => (i === index ? event.target.value : value)),
            )
            operation.reset()
          }}
        />
      ))}
      <Space wrap>
        <Button
          disabled={disabled || operation.blocked || samples.length >= 8}
          onClick={() => setSamples((previous) => [...previous, ''])}
        >
          {text('添加样本', 'Add sample')}
        </Button>
        <Button
          disabled={
            disabled ||
            operation.blocked ||
            revision < 1 ||
            samples.some((sample) => !sample.trim())
          }
          loading={operation.isPending}
          onClick={() => operation.mutate()}
        >
          {text('评估已保存草稿', 'Evaluate saved draft')}
        </Button>
        <Button
          disabled={operation.isPending}
          onClick={() => {
            setSamples([''])
            operation.reset()
          }}
        >
          {text('清除样本与结果', 'Clear samples and results')}
        </Button>
      </Space>
      {operation.feedback}
      {report ? (
        <>
          <Typography.Text>
            r{report.revision} / v{report.publishedVersion} · {text('将阻断', 'Would block')}{' '}
            {report.wouldBlock} · {text('需确认', 'Would warn')} {report.wouldWarn} ·{' '}
            {text('未完整检查', 'Uninspected')} {report.uninspected} ·{' '}
            {text('决定变化', 'Changed decisions')} {report.changed}
          </Typography.Text>
          <AdminTable
            rowKey="index"
            dataSource={report.samples}
            pagination={false}
            columns={[
              {
                title: text('样本', 'Sample'),
                key: 'sample',
                render: (_: unknown, row: DLPShadowSample) =>
                  `${row.index + 1} · ${row.sizeBytes} B`,
              },
              {
                title: text('已发布', 'Published'),
                key: 'published',
                render: (_: unknown, row: DLPShadowSample) =>
                  row.published ? (
                    <>
                      <Typography.Text>
                        {row.published.enabled
                          ? row.published.mode
                          : text('策略停用', 'Policy disabled')}{' '}
                        · {row.published.status} ·{' '}
                        {row.published.allowed ? text('允许', 'Allowed') : text('拒绝', 'Denied')}
                      </Typography.Text>
                      <DecisionEvidence decision={row.published} shadow />
                    </>
                  ) : (
                    text('尚未发布', 'Not published')
                  ),
              },
              {
                title: text('保存的草稿', 'Saved draft'),
                key: 'draft',
                render: (_: unknown, row: DLPShadowSample) => (
                  <>
                    <Typography.Text>
                      {row.draft.enabled ? row.draft.mode : text('策略停用', 'Policy disabled')} ·{' '}
                      {row.draft.status} ·{' '}
                      {row.draft.allowed ? text('允许', 'Allowed') : text('拒绝', 'Denied')}
                    </Typography.Text>
                    <DecisionEvidence decision={row.draft} shadow />
                  </>
                ),
              },
            ]}
          />
        </>
      ) : null}
    </Space>
  )
}
