import { ContextFeedback } from '@/components/context-feedback'
import { ApiError } from '@/services/api-error'
import { Button, Checkbox, Descriptions, Space, Typography } from 'antd'
import type { PluginPermissionRequest, PluginRepositoryPreview } from '../plugin-model'
import {
  addedPluginPermissions,
  pluginPackageComponents,
  pluginRequiresSignedRelease,
  pluginSourceKindLabel,
  pluginTypeLabel,
} from '../plugin-model'
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

export function previewCanConfirm(preview: PluginRepositoryPreview) {
  if (!preview.previewToken || preview.previewToken === 'preview-only') return false
  if (pluginRequiresSignedRelease(preview.manifest)) {
    return (
      preview.installEligibility === 'confirmable' &&
      preview.signatureStatus === 'verified' &&
      Boolean(preview.releaseAttestation && preview.publisherKeyFingerprint)
    )
  }
  return preview.installEligibility === undefined || preview.installEligibility === 'confirmable'
}

export function previewPublisherToTrust(preview: PluginRepositoryPreview | null) {
  return preview?.installEligibility === 'publisher-trust-required' && !preview.sourceInfo.official
    ? preview.publisherKeyFingerprint
    : undefined
}

export function verifyTrustedPreview(
  reviewed: PluginRepositoryPreview,
  fresh: PluginRepositoryPreview,
) {
  if (
    fresh.manifest.id !== reviewed.manifest.id ||
    fresh.sourceInfo.canonicalSource !== reviewed.sourceInfo.canonicalSource ||
    fresh.sourceInfo.commitSha !== reviewed.sourceInfo.commitSha ||
    fresh.sourceInfo.rawManifestChecksum !== reviewed.sourceInfo.rawManifestChecksum ||
    fresh.publisherKeyFingerprint !== reviewed.publisherKeyFingerprint
  ) {
    throw new Error('发布者指纹或提交内容已变化，请重新预览并核对。')
  }
}

export function RepositoryPublisherTrust({
  acknowledged,
  onAcknowledgedChange,
  onTrust,
}: {
  acknowledged: boolean
  onAcknowledgedChange: (checked: boolean) => void
  onTrust: () => void
}) {
  return (
    <Space orientation="vertical" size={8}>
      <Checkbox
        checked={acknowledged}
        onChange={(event) => onAcknowledgedChange(event.target.checked)}
      >
        我已核对并信任此发布者的 Ed25519 公钥指纹
      </Checkbox>
      <Button disabled={!acknowledged} onClick={onTrust}>
        信任发布者并重新预览
      </Button>
    </Space>
  )
}

export function pluginPublisherSignatureLabel(status?: string, fingerprint?: string) {
  switch (status) {
    case 'verified':
      return fingerprint ? '发布者签名已验证' : '未提供发布者验签凭据'
    case 'untrusted':
      return '发布者公钥尚未受信'
    case 'invalid':
      return '发布者签名无效'
    case 'not_provided':
      return '未提供签名'
    case 'unverified':
      return '发布者签名未验证'
    default:
      return status || '未声明'
  }
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
  canTrustPublisher = false,
}: {
  currentPermissions?: PluginPermissionRequest | null
  preview: PluginRepositoryPreview
  canTrustPublisher?: boolean
}) {
  const { manifest, sourceInfo } = preview
  const confirmable = previewCanConfirm(preview)
  let description = '请核对目标版本和权限变化。'
  if (!confirmable) {
    switch (preview.installEligibility) {
      case 'signed-release-required':
        description = '请先为此提交发布签名 Release，再重新预览。'
        break
      case 'publisher-trust-required':
        if (sourceInfo.official) {
          description = '请先在官方目录登记此发布者的 Ed25519 公钥，再重新预览。'
        } else {
          description = canTrustPublisher
            ? '请与发布者核对 Ed25519 公钥指纹，再确认信任并重新预览。信任关系随安装确认保存。'
            : '请由具备插件管理和安装权限的管理员核对并信任发布者公钥。'
        }
        break
      default:
        description = '当前预览未取得操作确认资格，请重新预览。'
    }
  } else if (!currentPermissions) {
    description = pluginRequiresSignedRelease(manifest)
      ? '签名制品将保存到本机。安装完成后请前往已安装插件单独启用。'
      : '安装将保存清单快照和权限申请。启用需要单独操作。'
  }
  return (
    <Space orientation="vertical" size={12} className="soha-plugin-repository-review">
      <ContextFeedback
        showIcon
        type={confirmable ? 'info' : 'warning'}
        title={confirmable ? '预览已固定到不可变提交' : '只读预览'}
        description={description}
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
          { key: 'components', label: '组件', children: pluginPackageComponents(manifest) },
          {
            key: 'compatibility',
            label: '兼容 Soha',
            children: manifest.compatibility?.soha || '未声明',
          },
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
            children: pluginPublisherSignatureLabel(
              preview.signatureStatus,
              preview.publisherKeyFingerprint,
            ),
          },
          ...(preview.publisherKeyFingerprint
            ? [
                {
                  key: 'publisherFingerprint',
                  label: 'Ed25519 公钥指纹',
                  children: (
                    <span className="soha-plugin-digest">{preview.publisherKeyFingerprint}</span>
                  ),
                  ...fullSpan(),
                },
              ]
            : []),
        ]}
      />
      <div>
        <Text strong>申请权限</Text>
        <PermissionReview permissions={manifest.permissions} />
        {confirmable && currentPermissions ? (
          <div className="soha-plugin-permission-diff">
            <AddedPermissionNotice current={currentPermissions} next={manifest.permissions} />
          </div>
        ) : null}
      </div>
    </Space>
  )
}
