import { lazy, Suspense, useState } from 'react'
import { Alert, Empty, Select, Space, Tabs, Typography } from 'antd'
import type {
  TemplateUpdateCandidate,
  TemplateUpdateDiff,
  TemplateUpdateChange,
} from '@opensoha/contracts/gen/ts/sohaapi'

const DiffEditor = lazy(() =>
  import('@/components/yaml-draft-diff-editor').then((module) => ({
    default: module.YamlDraftDiffEditor,
  })),
)

const sections = {
  source: '模板源文件',
  schema: '变量约束',
  defaults: '默认值',
  parameters: '有效变量',
  resources: '配置资源',
}

function ChangeDetail({
  change,
  diff,
  initiallyOpen,
}: {
  change: TemplateUpdateChange
  diff: TemplateUpdateDiff
  initiallyOpen: boolean
}) {
  const [open, setOpen] = useState(initiallyOpen)
  return (
    <details
      open={open}
      onToggle={(event) => setOpen(event.currentTarget.open)}
      className="soha-template-adoption__change"
    >
      <summary>
        {sections[change.section]} · {change.path}
      </summary>
      {open ? (
        <Suspense fallback={<div role="status">正在加载差异编辑器</div>}>
          <DiffEditor
            title={`${sections[change.section]} · ${change.path}`}
            // The shared editor shows `modified` on the left and diffs it against `original`.
            modified={change.before || ''}
            original={change.after || ''}
            leftLabel={`采用前 · v${diff.fromVersion}`}
            rightLabel={`采用后 · v${diff.toVersion}`}
            editable={false}
          />
        </Suspense>
      ) : null}
    </details>
  )
}

function DiffChanges({ diff }: { diff: TemplateUpdateDiff }) {
  return (
    <Space orientation="vertical" className="soha-template-adoption__full" size={12}>
      {diff.historyGap ? (
        <Alert
          type="warning"
          showIcon
          title="中间版本历史存在缺口"
          description="保留可用版本的源文件差异；确认缺口后仅采用最终版本。"
        />
      ) : null}
      {diff.renderStatus !== 'available' ? (
        <Alert
          type="warning"
          showIcon
          title={diff.renderStatus === 'invalid' ? '此段配置无法完整渲染' : '资源差异尚未完整渲染'}
          description={
            diff.diagnostics.join('；') ||
            '仍可查看源文件、变量约束与默认值；最终采用版本会单独校验。'
          }
        />
      ) : null}
      {!diff.changes.length ? (
        <Empty description="此版本段没有配置差异" />
      ) : (
        diff.changes.map((change, index) => (
          <ChangeDetail
            key={`${diff.fromVersion}:${diff.toVersion}:${change.section}:${change.path}:${index}`}
            change={change}
            diff={diff}
            initiallyOpen={diff.changes.length <= 4}
          />
        ))
      )}
    </Space>
  )
}

export function TemplateAdoptionDiff({ candidate }: { candidate: TemplateUpdateCandidate }) {
  const [segment, setSegment] = useState(0)
  const selected = candidate.versions[Math.min(segment, Math.max(candidate.versions.length - 1, 0))]
  return (
    <Tabs
      className="soha-resource-tabs"
      items={[
        {
          key: 'cumulative',
          label: `累计差异 v${candidate.target.fromVersion} → v${candidate.toVersion}`,
          children: <DiffChanges diff={candidate.cumulative} />,
        },
        {
          key: 'versions',
          label: `逐版本差异（${candidate.versions.length} 段）`,
          children: (
            <Space orientation="vertical" size={12} className="soha-template-adoption__full">
              <Typography.Text type="secondary">
                逐段仅供审阅，中间版本不会被部署或采用。实际发布记录仍在应用交付历史中。
              </Typography.Text>
              {selected ? (
                <>
                  <Select
                    aria-label="版本差异段"
                    value={Math.min(segment, candidate.versions.length - 1)}
                    onChange={setSegment}
                    options={candidate.versions.map((item, index) => ({
                      value: index,
                      label: `v${item.fromVersion} → v${item.toVersion}${item.historyGap ? ' · 历史缺口' : ''}`,
                    }))}
                  />
                  <DiffChanges diff={selected} />
                </>
              ) : (
                <Empty description="没有可比较的相邻版本" />
              )}
            </Space>
          ),
        },
      ]}
    />
  )
}
