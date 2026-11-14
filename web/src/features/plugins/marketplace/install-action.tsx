import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useQueryClient } from '@tanstack/react-query'
import { Alert, Card, Descriptions, Modal, Space, Tag, Typography } from 'antd'
import { useState } from 'react'
import { pluginMutations } from '../mutations'
import type { MarketplacePlugin } from '../plugin-model'
import {
  latestMarketplaceVersion,
  manifestAssetCount,
  manifestCapabilityCount,
  manifestExtensionCount,
  requiredSecretValues,
} from '../plugin-model'
import { ExtensionPointList } from '../shared/extension-list'
import { compactPluginTags, pluginRiskTag } from '../shared/formatters'
import { PermissionReview } from '../shared/manifest-sections'
import './styles.css'

const { Text } = Typography

function InstallReview({ plugin }: { plugin: MarketplacePlugin }) {
  const manifest = plugin.manifest
  const secrets = requiredSecretValues(manifest.secrets)
  const latestVersion = latestMarketplaceVersion(plugin)
  return (
    <Space orientation="vertical" size={12} className="soha-plugin-install-review">
      <Alert
        showIcon
        type={plugin.riskLevel === 'read' ? 'info' : 'warning'}
        title="插件安装只保存 manifest 快照和 requested permissions，不会直接授予能力。"
      />
      <Descriptions
        bordered
        column={2}
        size="small"
        items={[
          { key: 'id', label: 'ID', children: plugin.id },
          { key: 'publisher', label: 'Publisher', children: plugin.publisher },
          { key: 'version', label: 'Version', children: plugin.version },
          { key: 'source', label: 'Source', children: plugin.sourceId || plugin.source },
          {
            key: 'latest',
            label: 'Latest',
            children: latestVersion?.version || plugin.latestVersion || '-',
          },
          {
            key: 'verified',
            label: 'Verified',
            children: plugin.verified ? <Tag color="green">已验证</Tag> : <Tag>未验证</Tag>,
          },
          { key: 'risk', label: 'Risk', children: pluginRiskTag(plugin.riskLevel) },
          { key: 'assets', label: 'Assets', children: manifestAssetCount(manifest) },
          {
            key: 'capabilities',
            label: 'Capabilities',
            children: manifestCapabilityCount(manifest),
          },
          {
            key: 'runtime',
            label: 'Runtime',
            children: manifest.runtime?.mode || 'manifest-only',
          },
          {
            key: 'extensions',
            label: 'Extensions',
            children: manifestExtensionCount(manifest),
          },
        ]}
      />
      <Card size="small" title="Requested permissions">
        <PermissionReview permissions={manifest.permissions} />
      </Card>
      <Card size="small" title="Required secrets">
        {secrets.length ? (
          compactPluginTags(
            secrets.map((item) => item.name),
            6,
          )
        ) : (
          <Text type="secondary">无</Text>
        )}
      </Card>
      <Card size="small" title="Extension points">
        <ExtensionPointList points={manifest.extensionPoints} />
      </Card>
    </Space>
  )
}

export function useInstallPluginAction() {
  const queryClient = useQueryClient()
  const [review, setReview] = useState<{ plugin: MarketplacePlugin; enable: boolean } | null>(null)
  const mutation = useOperationMutation(pluginMutations.install(queryClient), {
    label: ['安装插件', 'Install plugin'],
    target: (input) => input.pluginId || '插件',
    receipt: (item) => ({
      message: `安装记录已保存：${item.name}`,
      evidence: '启用状态以安装记录为准，不代表外部权限已授予或运行时已健康。',
    }),
  })

  return {
    loading: mutation.isPending,
    blocked: mutation.blocked,
    feedback: review ? null : mutation.feedback,
    dialog: (
      <Modal
        open={Boolean(review)}
        title={review ? `安装 ${review.plugin.name}` : '安装插件'}
        width={720}
        styles={operationModalStyles}
        style={{ top: 32 }}
        confirmLoading={mutation.isPending}
        closable={!mutation.blocked}
        keyboard={!mutation.blocked}
        mask={{ closable: false }}
        okButtonProps={{ disabled: mutation.blocked }}
        cancelButtonProps={{ disabled: mutation.blocked }}
        okText={review?.enable ? '安装并启用' : '安装'}
        cancelText="取消"
        onCancel={() => {
          if (!mutation.blocked) setReview(null)
        }}
        onOk={() => {
          if (!review || mutation.blocked) return
          const { plugin, enable } = review
          mutation.mutate(
            {
              pluginId: plugin.id,
              enable,
              sourceId: plugin.sourceId,
              marketplaceUrl: plugin.sourceUrl,
              version: plugin.latestVersion || plugin.version,
            },
            { onSuccess: () => setReview(null) },
          )
        }}
      >
        {mutation.feedback}
        {review ? <InstallReview plugin={review.plugin} /> : null}
      </Modal>
    ),
    confirmInstall(plugin: MarketplacePlugin, enable = false) {
      if (mutation.blocked) return
      mutation.reset()
      setReview({ plugin, enable })
    },
  }
}

export function marketplaceDetailPath(plugin: MarketplacePlugin) {
  const params = new URLSearchParams()
  if (plugin.sourceId) params.set('sourceId', plugin.sourceId)
  if (plugin.sourceUrl) params.set('marketplaceUrl', plugin.sourceUrl)
  if (plugin.latestVersion || plugin.version) {
    params.set('version', plugin.latestVersion || plugin.version)
  }
  const suffix = params.toString()
  return `/plugins/marketplace/${encodeURIComponent(plugin.id)}${suffix ? `?${suffix}` : ''}`
}
