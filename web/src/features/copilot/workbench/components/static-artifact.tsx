import { lazy, Suspense } from 'react'
import { Alert, App, Button, Space, Tabs, Typography } from 'antd'
import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'
import type { WorkbenchArtifact } from '../types'
import { staticArtifactLabel } from '../artifacts'
import { WorkbenchMarkdown } from './markdown'

const DraftDiff = lazy(() =>
  import('@/components/yaml-draft-diff-editor').then((module) => ({
    default: module.YamlDraftDiffEditor,
  })),
)

export function StaticArtifactView({
  artifact,
  sources,
  onSource,
}: {
  artifact: WorkbenchArtifact
  sources: WorkbenchSource[]
  onSource: (source: WorkbenchSource) => void
}) {
  const { message } = App.useApp()
  const snapshot = artifact.dataSourceSnapshot ?? {}
  const content = typeof snapshot.content === 'string' ? snapshot.content : ''
  const baseline = typeof snapshot.baseline === 'string' ? snapshot.baseline : ''
  const format = typeof snapshot.format === 'string' ? snapshot.format : 'text'
  const config = artifact.kind === 'configuration_preview'
  return (
    <Space
      orientation="vertical"
      size={12}
      className="soha-ai-static-artifact"
      style={{ width: '100%' }}
    >
      {config ? (
        <Alert
          type="info"
          title="静态配置草稿，尚未应用"
          description={
            snapshot.baselineCitationId
              ? '差异基线取自本轮提供的引用。'
              : '本草稿没有基线，按新增内容展示。'
          }
        />
      ) : null}
      <Button
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(content)
            void message.success('已复制内容')
          } catch {
            void message.error('复制失败，请手动选择内容。')
          }
        }}
      >
        复制{staticArtifactLabel(artifact.kind)}
      </Button>
      <Tabs
        items={[
          {
            key: 'content',
            label: '内容',
            children:
              format === 'markdown' ? (
                <WorkbenchMarkdown content={content} sources={sources} onSource={onSource} />
              ) : (
                <pre className="soha-ai-static-content">{content}</pre>
              ),
          },
          ...(config && ['yaml', 'json'].includes(format)
            ? [
                {
                  key: 'diff',
                  label: '差异',
                  children: (
                    <Suspense fallback={<Typography.Text>正在加载差异…</Typography.Text>}>
                      <DraftDiff
                        title={artifact.title || '配置差异'}
                        editable={false}
                        modified={baseline}
                        original={content}
                        leftLabel="引用基线"
                        rightLabel="基线 → 生成草稿"
                      />
                    </Suspense>
                  ),
                },
              ]
            : []),
        ]}
      />
    </Space>
  )
}
