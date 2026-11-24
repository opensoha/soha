import { ContextFeedback } from '@/components/context-feedback'
import { useInlineApiError } from '@/components/request-feedback'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useQueryClient } from '@tanstack/react-query'
import { Button, Checkbox, Descriptions, Modal, Space, Typography } from 'antd'
import { useEffect, useRef, useState } from 'react'
import { pluginKeys } from '../keys'
import { pluginApi } from '../plugin-api'
import type { InstalledPlugin, PluginRepositoryPreview, PluginUpdate } from '../plugin-model'
import { addedPluginPermissions, pluginSourceKindLabel } from '../plugin-model'
import { pluginMutations } from '../mutations'
import { RepositoryRefs } from '../shared/repository-refs'
import {
  previewExpired,
  previewNeedsRefresh,
  previewRequiresBranch,
  RepositoryPreviewReview,
  repositoryFailureText,
} from '../shared/repository-preview'

const { Text } = Typography

export function useRepositoryUpgradeAction() {
  const queryClient = useQueryClient()
  const [target, setTarget] = useState<{ plugin: InstalledPlugin; update?: PluginUpdate }>()
  const [needsBranch, setNeedsBranch] = useState(false)
  const [branch, setBranch] = useState<string>()
  const [preview, setPreview] = useState<PluginRepositoryPreview | null>(null)
  const [previewError, setPreviewError] = useState<Error | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [expired, setExpired] = useState(false)
  const [acknowledged, setAcknowledged] = useState(false)
  const requestSequence = useRef(0)
  const { plugin, update } = target ?? {}
  const source = plugin?.sourceInfo
  const repositoryUrl = update?.repositoryUrl ?? source?.canonicalSource
  const currentPermissions = plugin?.requestedPermissions ?? plugin?.manifest.permissions
  const added = addedPluginPermissions(currentPermissions, preview?.manifest.permissions)
  const permissionIncrease = Boolean(added.required.length || added.domain.length)
  const mutation = useOperationMutation(pluginMutations.repositoryUpgrade(queryClient), {
    label: ['同源升级仓库插件', 'Upgrade repository plugin from source'],
    target: (variables) => plugin?.name || variables.pluginId,
    receipt: (item) => ({
      message: `仓库升级已完成：${item.name} ${item.version}`,
      evidence: '实际启用状态与运行健康以安装记录和运行时观察为准。',
    }),
  })
  useInlineApiError(previewError)

  function clearPreview(resetOperation = true) {
    requestSequence.current += 1
    setPreview(null)
    setPreviewing(false)
    setAcknowledged(false)
    setExpired(false)
    if (resetOperation) mutation.reset()
  }

  useEffect(() => {
    if (!preview) return
    const expiresAt = Date.parse(preview.expiresAt)
    if (!Number.isFinite(expiresAt)) return
    const expire = () => {
      requestSequence.current += 1
      setPreview(null)
      setAcknowledged(false)
      setExpired(true)
    }
    if (expiresAt <= Date.now()) {
      expire()
      return
    }
    const timer = window.setTimeout(expire, expiresAt - Date.now())
    return () => window.clearTimeout(timer)
  }, [preview])

  function close() {
    if (mutation.blocked) return
    requestSequence.current += 1
    setTarget(undefined)
    setNeedsBranch(false)
    setBranch(undefined)
    setPreview(null)
    setPreviewError(null)
    setPreviewing(false)
    setExpired(false)
    setAcknowledged(false)
  }

  function start(plugin: InstalledPlugin, update?: PluginUpdate) {
    if (mutation.blocked || (!update && plugin.sourceInfo?.kind !== 'github-repo')) return
    requestSequence.current += 1
    setPreview(null)
    setPreviewError(null)
    setPreviewing(false)
    setExpired(false)
    setAcknowledged(false)
    setNeedsBranch(false)
    setBranch(undefined)
    mutation.reset()
    setTarget({ plugin, update })
  }

  async function previewUpgrade() {
    if (!plugin || !source || !repositoryUrl || mutation.blocked || (needsBranch && !branch)) return
    if (
      update &&
      (update.installedVersion !== plugin.version ||
        update.repositoryUrl !== (source.repositoryUrl ?? source.canonicalSource) ||
        update.marketplaceSourceId !== source.sourceId)
    ) {
      setPreviewError(new Error('已安装版本或来源已变化，请关闭此窗口并查看最新的可用更新。'))
      return
    }
    const sequence = ++requestSequence.current
    setPreview(null)
    setPreviewError(null)
    setExpired(false)
    setAcknowledged(false)
    mutation.reset()
    setPreviewing(true)
    try {
      const result = await pluginApi.repositoryPreview({
        repositoryUrl,
        ...(update ? { ref: update.ref } : branch ? { ref: branch } : {}),
        ...(update?.marketplaceSourceId
          ? { pluginId: plugin.id, marketplaceSourceId: update.marketplaceSourceId }
          : {}),
      })
      if (sequence !== requestSequence.current) return
      const latest = await pluginApi.installedDetail(plugin.id)
      if (sequence !== requestSequence.current) return
      if (
        result.manifest.id !== plugin.id ||
        result.sourceInfo.canonicalSource !== repositoryUrl ||
        (update && result.manifest.version !== update.targetVersion)
      ) {
        throw new Error('预览的插件或仓库与当前安装记录不一致。')
      }
      if (
        latest.updatedAt !== plugin.updatedAt ||
        latest.version !== plugin.version ||
        latest.installedAt !== plugin.installedAt ||
        latest.sourceInfo?.canonicalSource !== source.canonicalSource ||
        latest.sourceInfo?.kind !== source.kind ||
        latest.sourceInfo?.sourceId !== source.sourceId ||
        latest.sourceInfo?.repositoryUrl !== source.repositoryUrl
      ) {
        queryClient.setQueryData(pluginKeys.installedDetail(plugin.id), latest)
        void queryClient.invalidateQueries({ queryKey: pluginKeys.installed() })
        setTarget({ plugin: latest, update })
        throw new Error('已安装插件在预览期间有变更，请重新预览。')
      }
      setPreview(result)
    } catch (error) {
      if (sequence !== requestSequence.current) return
      setPreviewError(error instanceof Error ? error : new Error(String(error)))
      if (!update && previewRequiresBranch(error)) {
        setNeedsBranch(true)
        setBranch(undefined)
      }
    } finally {
      if (sequence === requestSequence.current) setPreviewing(false)
    }
  }

  function confirm() {
    if (!plugin || !preview || mutation.blocked || (permissionIncrease && !acknowledged)) return
    if (previewExpired(preview)) {
      clearPreview(false)
      setExpired(true)
      return
    }
    mutation.mutate(
      {
        pluginId: plugin.id,
        input: {
          previewToken: preview.previewToken,
          ...(permissionIncrease ? { confirmPermissionIncrease: true } : {}),
        },
      },
      {
        onSuccess: () => close(),
        onError: (error) => {
          if (previewNeedsRefresh(error)) {
            clearPreview(false)
            setExpired(true)
          }
        },
      },
    )
  }

  const operationError = mutation.error instanceof Error ? mutation.error : null

  return {
    blocked: mutation.blocked,
    feedback: mutation.feedback,
    open: start,
    dialog: (
      <Modal
        open={Boolean(target)}
        title={plugin ? `升级 ${plugin.name}` : '升级仓库插件'}
        width={760}
        styles={operationModalStyles}
        style={{ top: 32 }}
        closable={!mutation.blocked}
        keyboard={!mutation.blocked}
        mask={{ closable: false }}
        onCancel={close}
        footer={
          <Space wrap>
            <Button disabled={mutation.blocked} onClick={close}>
              取消
            </Button>
            <Button
              loading={previewing}
              disabled={mutation.blocked || (needsBranch && !branch)}
              onClick={previewUpgrade}
            >
              {preview ? '重新预览' : '预览升级'}
            </Button>
            <Button
              type="primary"
              loading={mutation.isPending}
              disabled={
                !preview || previewing || mutation.blocked || (permissionIncrease && !acknowledged)
              }
              onClick={confirm}
            >
              确认升级
            </Button>
          </Space>
        }
      >
        <Space orientation="vertical" size={12} className="soha-plugin-repository-dialog">
          {plugin && source ? (
            <Descriptions
              bordered
              column={1}
              size="small"
              items={[
                { key: 'currentVersion', label: '当前版本', children: plugin.version },
                ...(update
                  ? [{ key: 'targetVersion', label: '目标版本', children: update.targetVersion }]
                  : []),
                {
                  key: 'source',
                  label: '来源',
                  children: pluginSourceKindLabel(source.kind),
                },
                {
                  key: 'repository',
                  label: '仓库',
                  children: <span className="soha-plugin-digest">{repositoryUrl}</span>,
                },
                {
                  key: 'currentCommit',
                  label: '当前固定提交',
                  children: source.commitSha || '-',
                },
              ]}
            />
          ) : null}
          <Text type="secondary">
            只能从已安装记录确定的仓库同源升级，不能在此更换来源；预览要求插件安装权限，确认要求升级权限。
          </Text>
          {needsBranch && repositoryUrl ? (
            <RepositoryRefs
              repositoryUrl={repositoryUrl}
              kind="branch"
              disabled={mutation.blocked}
              selectedBranch={branch}
              onSelect={(value) => {
                setBranch(value)
                setPreviewError(null)
                clearPreview()
              }}
            />
          ) : null}
          {expired ? (
            <ContextFeedback
              showIcon
              type="warning"
              title="预览已过期或内容已变化，确认资格已清除。请重新预览。"
            />
          ) : null}
          {previewError && !(needsBranch && previewRequiresBranch(previewError)) ? (
            <ContextFeedback
              showIcon
              type="error"
              title="仓库升级预览失败"
              description={repositoryFailureText(previewError)}
            />
          ) : null}
          {preview ? (
            <>
              <RepositoryPreviewReview
                currentPermissions={currentPermissions ?? {}}
                preview={preview}
              />
              {permissionIncrease ? (
                <Checkbox
                  checked={acknowledged}
                  disabled={mutation.blocked}
                  onChange={(event) => setAcknowledged(event.target.checked)}
                >
                  我已知悉本次升级新增的权限要求，并同意继续升级
                </Checkbox>
              ) : null}
            </>
          ) : null}
          {operationError ? (
            <ContextFeedback
              showIcon
              type="error"
              title={mutation.outcomeUnknown ? '升级结果不明，请先核对安装记录' : '仓库升级失败'}
              description={repositoryFailureText(operationError)}
            />
          ) : null}
        </Space>
      </Modal>
    ),
  }
}
