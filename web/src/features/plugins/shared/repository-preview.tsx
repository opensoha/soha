import { ContextFeedback } from '@/components/context-feedback'
import { ApiError } from '@/services/api-error'
import { Descriptions, Space, Typography } from 'antd'
import type { PluginPermissionRequest, PluginRepositoryPreview } from '../plugin-model'
import { addedPluginPermissions, pluginSourceKindLabel, pluginTypeLabel } from '../plugin-model'
import { compactPluginTags } from './formatters'
import { PermissionReview } from './manifest-sections'

const { Text } = Typography

function fullSpan() {
  return { span: { xs: 1, sm: 2 } }
}

export function previewExpired(preview: PluginRepositoryPreview) {
  const expiresAt = Date.parse(preview.expiresAt)
  return Number.isFinite(expiresAt) && expiresAt <= Date.now()
}

export function previewNeedsRefresh(error: Error) {
  return (
    error instanceof ApiError && (error.code === 'plugin_preview_invalid' || error.status === 410)
  )
}

export function previewRequiresBranch(error: unknown) {
  return error instanceof ApiError && error.code === 'plugin_repository_branch_required'
}

export function repositoryUnsupported(error: Error) {
  return (
    error instanceof ApiError &&
    ((error.status === 404 && !error.code) ||
      [405, 501].includes(error.status) ||
      error.code === 'unsupported_operation')
  )
}

export function repositoryFailureText(error?: Error | null) {
  if (!error) return ''
  if (!repositoryUnsupported(error)) return error.message
  return `${error.message} 当前服务端不支持仓库操作，已停止；未回退到旧安装或升级入口。`
}

export function AddedPermissionNotice({
  current,
  next,
}: {
  current?: PluginPermissionRequest | null
  next?: PluginPermissionRequest | null
}) {
  const added = addedPluginPermissions(current, next)
  if (!added.required.length && !added.domain.length) {
    return <Text type="secondary">未新增权限要求</Text>
  }
  return (
    <ContextFeedback
      showIcon
      type="warning"
      title="升级将新增权限要求"
      description={
        <Space orientation="vertical" size={6}>
          {added.required.length ? (
            <div>
              <Text type="secondary">新增 Gateway</Text>
              <div>{compactPluginTags(added.required, added.required.length)}</div>
            </div>
          ) : null}
          {added.domain.length ? (
            <div>
              <Text type="secondary">新增 Domain</Text>
              <div>{compactPluginTags(added.domain, added.domain.length)}</div>
            </div>
          ) : null}
        </Space>
      }
    />
  )
}

export function RepositoryPreviewReview({
  currentPermissions,
  preview,
}: {
  currentPermissions?: PluginPermissionRequest | null
  preview: PluginRepositoryPreview
}) {
  const { manifest, sourceInfo } = preview
  return (
    <Space orientation="vertical" size={12} className="soha-plugin-repository-review">
      <ContextFeedback
        showIcon
        type="info"
        title={
          currentPermissions
            ? '预览已固定到不可变提交，请核对目标版本和权限变化。'
            : '预览已固定到不可变提交；安装只保存 manifest 快照和权限申请，不会自动启用或授予权限。'
        }
      />
      <Descriptions
        bordered
        column={{ xs: 1, sm: 2 }}
        size="small"
        items={[
          {
            key: 'plugin',
            label: '插件',
            children: `${manifest.name} / ${manifest.id}`,
            ...fullSpan(),
          },
          { key: 'version', label: '目标版本', children: manifest.version },
          { key: 'type', label: '类型', children: pluginTypeLabel(manifest.type) },
          {
            key: 'runtime',
            label: '运行方式',
            children: manifest.runtime?.mode || 'manifest-only',
          },
          {
            key: 'sourceKind',
            label: '来源类型',
            children: pluginSourceKindLabel(sourceInfo.kind),
          },
          {
            key: 'official',
            label: '官方收录',
            children: sourceInfo.official ? '是' : '否',
          },
          {
            key: 'repository',
            label: '仓库',
            children: <span className="soha-plugin-digest">{sourceInfo.canonicalSource}</span>,
            ...fullSpan(),
          },
          {
            key: 'pinnedRef',
            label: '请求修订',
            children: sourceInfo.pinnedRef || '-',
          },
          {
            key: 'commit',
            label: '固定提交',
            children: <span className="soha-plugin-digest">{sourceInfo.commitSha}</span>,
            ...fullSpan(),
          },
          {
            key: 'digest',
            label: '原始摘要',
            children: <span className="soha-plugin-digest">{sourceInfo.rawManifestChecksum}</span>,
            ...fullSpan(),
          },
          { key: 'checksum', label: '完整性', children: preview.checksumStatus || '-' },
          {
            key: 'signature',
            label: '签名状态',
            children: preview.signatureStatus || '未声明',
          },
        ]}
      />
      <div>
        <Text strong>申请权限</Text>
        <PermissionReview permissions={manifest.permissions} />
        {currentPermissions ? (
          <div className="soha-plugin-permission-diff">
            <AddedPermissionNotice current={currentPermissions} next={manifest.permissions} />
          </div>
        ) : null}
      </div>
    </Space>
  )
}
