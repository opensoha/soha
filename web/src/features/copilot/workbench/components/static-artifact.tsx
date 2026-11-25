import { ContextFeedback } from '@/components/context-feedback'
import { StatusTag } from '@/components/status-tag'
import {
  WorkspacePublishModal,
  resolveSafeHostedAppUrl,
  type WorkspaceHostedAppDeploymentResult,
} from '@/features/app-hosting'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { useAuthStore } from '@/stores/auth-store'
import { CopyOutlined, ExportOutlined } from '@ant-design/icons'
import type { WorkbenchSource } from '@opensoha/contracts/gen/ts/sohaapi'
import { App, Button, Card, Space, Tabs, Typography } from 'antd'
import { lazy, Suspense, useState } from 'react'
import { artifactSnapshotText, staticArtifactLabel } from '../artifacts'
import type { WorkbenchArtifact } from '../types'
import { WorkbenchMarkdown } from './markdown'

const DraftDiff = lazy(() =>
  import('@/components/yaml-draft-diff-editor').then((module) => ({
    default: module.YamlDraftDiffEditor,
  })),
)

export function StaticArtifactView({
  artifact,
  sessionId: propSessionId,
  artifactId: propArtifactId,
  sources,
  onSource,
}: {
  artifact: WorkbenchArtifact
  sessionId?: string | null
  artifactId?: string
  sources: WorkbenchSource[]
  onSource: (source: WorkbenchSource) => void
}) {
  const { message } = App.useApp()
  const currentUserId = useAuthStore((state) => state.user?.userId ?? '')
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [publishModalOpen, setPublishModalOpen] = useState(false)
  const [lastPublishedResult, setLastPublishedResult] =
    useState<WorkspaceHostedAppDeploymentResult | null>(null)

  const snapshot = artifact.dataSourceSnapshot ?? {}
  const content = typeof snapshot.content === 'string' ? snapshot.content : ''
  const baseline = typeof snapshot.baseline === 'string' ? snapshot.baseline : ''
  const format = typeof snapshot.format === 'string' ? snapshot.format : 'text'
  const config = artifact.kind === 'configuration_preview'

  const sessionId =
    propSessionId !== undefined
      ? (propSessionId?.trim() || artifactSnapshotText(snapshot, 'sessionId'))
      : artifactSnapshotText(snapshot, 'sessionId') ||
        (typeof window !== 'undefined'
          ? (new URLSearchParams(window.location.search).get('session')?.trim() ?? '')
          : '')
  const artifactId =
    propArtifactId?.trim() || artifactSnapshotText(snapshot, 'artifactId') || artifact.runId.trim()
  const canPublishApp =
    hasPermission(permissionSnapshotQuery.data?.data, 'apphosting.apps.publish') &&
    Boolean(content.trim()) &&
    Boolean(artifactId)

  const safePublishedUrl = resolveSafeHostedAppUrl(lastPublishedResult?.app.url)

  return (
    <Space
      orientation="vertical"
      size={12}
      className="soha-ai-static-artifact"
      style={{ width: '100%' }}
    >
      {config ? (
        <ContextFeedback
          type="info"
          title="静态配置草稿，尚未应用"
          description={
            snapshot.baselineCitationId
              ? '差异基线取自本轮提供的引用。'
              : '本草稿没有基线，按新增内容展示。'
          }
        />
      ) : null}
      <Space wrap size={8}>
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
        {canPublishApp ? (
          <Button type="primary" onClick={() => setPublishModalOpen(true)}>
            发布为托管应用
          </Button>
        ) : null}
      </Space>
      {lastPublishedResult ? (
        <Card size="small" role="status" variant="outlined">
          <Space orientation="vertical" size={6} style={{ width: '100%' }}>
            <Space size={8} wrap>
              <Typography.Text strong>已发布：{lastPublishedResult.app.name}</Typography.Text>
              <StatusTag value={lastPublishedResult.app.status} label="在线" />
              <Typography.Text type="secondary">
                v{lastPublishedResult.revision.revisionNumber}
              </Typography.Text>
            </Space>
            <Typography.Paragraph copyable style={{ marginBottom: 0 }}>
              {lastPublishedResult.app.url}
            </Typography.Paragraph>
            <Space wrap>
              {safePublishedUrl ? (
                <Button
                  size="small"
                  type="primary"
                  icon={<ExportOutlined />}
                  href={safePublishedUrl}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  打开应用
                </Button>
              ) : null}
              <Button
                size="small"
                icon={<CopyOutlined />}
                onClick={async () => {
                  try {
                    await navigator.clipboard.writeText(lastPublishedResult.app.url)
                    void message.success('已复制分享链接')
                  } catch {
                    void message.error('复制失败，请手动复制链接')
                  }
                }}
              >
                复制分享链接
              </Button>
            </Space>
          </Space>
        </Card>
      ) : null}
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
      {canPublishApp ? (
        <WorkspacePublishModal
          open={publishModalOpen}
          onClose={() => setPublishModalOpen(false)}
          currentUserId={currentUserId}
          defaultName={artifact.title || ''}
          defaultSpaFallback={false}
          source={{
            kind: 'conversation_artifact',
            ...(sessionId ? { sessionId } : {}),
            artifactId,
          }}
          sourceSummary={`对话静态成果：${artifact.title || artifactId}`}
          onPublished={(result) => setLastPublishedResult(result)}
        />
      ) : null}
    </Space>
  )
}
