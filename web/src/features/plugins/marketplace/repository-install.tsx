import { ContextFeedback } from '@/components/context-feedback'
import { useInlineApiError } from '@/components/request-feedback'
import { operationModalStyles } from '@/components/modal-styles'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useQueryClient } from '@tanstack/react-query'
import { Button, Descriptions, Form, Input, Modal, Space, Typography } from 'antd'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { ApiError } from '@/services/api-error'
import { pluginApi } from '../plugin-api'
import type {
  InstalledPlugin,
  PluginRepositoryPreview,
  PluginRepositoryPreviewRequest,
} from '../plugin-model'
import { pluginMutations } from '../mutations'
import { pluginStatusBadge } from '../shared/formatters'
import { RepositoryRefs } from '../shared/repository-refs'
import {
  previewCanConfirm,
  previewExpired,
  previewNeedsRefresh,
  previewPublisherToTrust,
  previewRequiresBranch,
  RepositoryPreviewReview,
  RepositoryPublisherTrust,
  repositoryFailureText,
  verifyTrustedPreview,
} from '../shared/repository-preview'

const { Text } = Typography

interface RepositoryInstallValues {
  repositoryUrl: string
  ref?: string
}

function installedStatusText(plugin: InstalledPlugin) {
  if (plugin.status === 'enabled') return '已启用'
  if (plugin.status === 'pending_config') return '待配置'
  return '未启用'
}

export function useRepositoryInstallAction(canTrustPublisher: boolean) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const [form] = Form.useForm<RepositoryInstallValues>()
  const [open, setOpen] = useState(false)
  const [marketSource, setMarketSource] = useState<PluginRepositoryPreviewRequest>()
  const [branchRepository, setBranchRepository] = useState<string>()
  const [branch, setBranch] = useState<string>()
  const [preview, setPreview] = useState<PluginRepositoryPreview | null>(null)
  const [previewError, setPreviewError] = useState<Error | null>(null)
  const [previewing, setPreviewing] = useState(false)
  const [trustAcknowledged, setTrustAcknowledged] = useState(false)
  const [expired, setExpired] = useState(false)
  const [installed, setInstalled] = useState<InstalledPlugin | null>(null)
  const [conflictPluginId, setConflictPluginId] = useState<string | null>(null)
  const requestSequence = useRef(0)
  const publisherToTrust = previewPublisherToTrust(preview)
  const confirmable = preview && previewCanConfirm(preview)
  const mutation = useOperationMutation(pluginMutations.repositoryInstall(queryClient), {
    label: ['从仓库安装插件', 'Install plugin from repository'],
    target: () => preview?.manifest.name || 'GitHub 仓库',
    receipt: (item) => ({
      message: `仓库安装记录已保存：${item.name}`,
      evidence: '仓库安装默认不启用；实际状态以安装记录为准。',
    }),
  })
  useInlineApiError(previewError)
  const resetMutation = mutation.reset

  const clearPreview = useCallback(
    (reset = true) => {
      requestSequence.current += 1
      setPreview(null)
      setTrustAcknowledged(false)
      setPreviewing(false)
      setExpired(false)
      if (reset) resetMutation()
    },
    [resetMutation],
  )

  useEffect(() => {
    if (!preview) return
    const expiresAt = Date.parse(preview.expiresAt)
    if (!Number.isFinite(expiresAt)) return
    const expire = () => {
      clearPreview(false)
      setExpired(true)
    }
    if (expiresAt <= Date.now()) {
      expire()
      return
    }
    const timer = window.setTimeout(expire, expiresAt - Date.now())
    return () => window.clearTimeout(timer)
  }, [clearPreview, preview])

  function close() {
    if (mutation.blocked) return
    requestSequence.current += 1
    setOpen(false)
    setMarketSource(undefined)
    setBranchRepository(undefined)
    setBranch(undefined)
    setPreview(null)
    setTrustAcknowledged(false)
    setPreviewError(null)
    setPreviewing(false)
    setExpired(false)
    setInstalled(null)
    setConflictPluginId(null)
    if (!marketSource) form.resetFields()
  }

  function start(source?: PluginRepositoryPreviewRequest) {
    requestSequence.current += 1
    if (!source) form.resetFields()
    setMarketSource(source)
    setBranchRepository(undefined)
    setBranch(undefined)
    setPreview(null)
    setTrustAcknowledged(false)
    setPreviewError(null)
    setPreviewing(false)
    setExpired(false)
    setInstalled(null)
    setConflictPluginId(null)
    mutation.reset()
    setOpen(true)
  }

  function resetForInput() {
    if (installed) return
    setPreviewError(null)
    setConflictPluginId(null)
    setBranchRepository(undefined)
    setBranch(undefined)
    clearPreview()
  }

  async function previewRepository(trust = false) {
    if (mutation.blocked || previewing || (branchRepository && !branch)) return
    if (
      trust &&
      (!canTrustPublisher ||
        !trustAcknowledged ||
        !publisherToTrust ||
        !preview ||
        previewExpired(preview))
    )
      return
    const reviewed = trust ? preview : null
    const sequence = ++requestSequence.current
    let input: PluginRepositoryPreviewRequest
    try {
      if (marketSource) {
        input = marketSource
      } else {
        const values = await form.validateFields()
        input = {
          repositoryUrl: values.repositoryUrl.trim(),
          ...(values.ref?.trim() ? { ref: values.ref.trim() } : {}),
        }
      }
    } catch {
      return
    }
    if (sequence !== requestSequence.current) return
    setPreview(null)
    setTrustAcknowledged(false)
    setPreviewError(null)
    setExpired(false)
    setInstalled(null)
    setConflictPluginId(null)
    mutation.reset()
    setPreviewing(true)
    try {
      const result = await pluginApi.repositoryPreview({
        ...input,
        ...(branch ? { ref: branch } : {}),
        ...(reviewed ? { trustPublisherFingerprint: publisherToTrust } : {}),
      })
      if (sequence !== requestSequence.current) return
      if (reviewed) verifyTrustedPreview(reviewed, result)
      setPreview(result)
    } catch (error) {
      if (sequence !== requestSequence.current) return
      setPreviewError(error instanceof Error ? error : new Error(String(error)))
      if (previewRequiresBranch(error)) {
        setBranchRepository(input.repositoryUrl)
        setBranch(undefined)
      }
    } finally {
      if (sequence === requestSequence.current) setPreviewing(false)
    }
  }

  function confirm() {
    if (!preview || !previewCanConfirm(preview) || mutation.blocked) return
    if (previewExpired(preview)) {
      clearPreview(false)
      setExpired(true)
      return
    }
    const pluginId = preview.manifest.id
    mutation.mutate(
      { previewToken: preview.previewToken },
      {
        onSuccess: (item) => {
          setInstalled(item)
          setPreview(null)
          setExpired(false)
        },
        onError: (error) => {
          if (error instanceof ApiError && error.code === 'plugin_source_conflict') {
            setConflictPluginId(pluginId)
            clearPreview(false)
          } else if (previewNeedsRefresh(error)) {
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
    loading: mutation.isPending,
    open: () => start(),
    openMarketplace: (
      repositoryUrl: string,
      pluginId: string,
      marketplaceSourceId?: string,
      ref?: string,
    ) => {
      if (marketplaceSourceId) start({ repositoryUrl, pluginId, marketplaceSourceId, ref })
    },
    dialog: (
      <Modal
        open={open}
        title="从 GitHub 仓库安装"
        width={760}
        styles={operationModalStyles}
        style={{ top: 32 }}
        closable={!mutation.blocked}
        keyboard={!mutation.blocked}
        mask={{ closable: false }}
        onCancel={close}
        footer={
          installed ? (
            <Space wrap>
              <Button onClick={close}>关闭</Button>
              <Button
                type="primary"
                onClick={() => {
                  close()
                  navigate('/plugins/marketplace?installation=installed')
                }}
              >
                查看已安装插件
              </Button>
            </Space>
          ) : (
            <Space wrap>
              <Button disabled={mutation.blocked} onClick={close}>
                取消
              </Button>
              <Button
                loading={previewing}
                disabled={mutation.blocked || Boolean(branchRepository && !branch)}
                onClick={() => void previewRepository()}
              >
                {preview ? '重新预览' : '预览'}
              </Button>
              {!preview || confirmable ? (
                <Button
                  type="primary"
                  loading={mutation.isPending}
                  disabled={!preview || previewing || mutation.blocked}
                  onClick={confirm}
                >
                  确认安装
                </Button>
              ) : null}
            </Space>
          )
        }
      >
        <Space orientation="vertical" size={12} className="soha-plugin-repository-dialog">
          {marketSource ? (
            <Descriptions
              column={1}
              size="small"
              items={[
                {
                  key: 'repository',
                  label: '仓库',
                  children: (
                    <span className="soha-plugin-digest">{marketSource.repositoryUrl}</span>
                  ),
                },
                ...(marketSource.ref
                  ? [
                      {
                        key: 'release',
                        label: '发布标签',
                        children: marketSource.ref.replace(/^refs\/tags\//, ''),
                      },
                    ]
                  : []),
              ]}
            />
          ) : (
            <Form
              form={form}
              layout="vertical"
              disabled={mutation.blocked || Boolean(installed)}
              onValuesChange={resetForInput}
            >
              <Form.Item
                label="仓库地址"
                name="repositoryUrl"
                extra="支持公开 GitHub 仓库、分支或标签页、Release 页面链接。"
                rules={[{ required: true, message: '请输入公开 GitHub 仓库地址' }]}
              >
                <Input autoFocus allowClear placeholder="https://github.com/owner/repository" />
              </Form.Item>
              <Form.Item label="修订（可选）" name="ref" extra="留空时预览最新稳定 Release。">
                <Input allowClear placeholder="分支、标签或提交（可选）" />
              </Form.Item>
            </Form>
          )}
          {branchRepository && !installed ? (
            <RepositoryRefs
              repositoryUrl={branchRepository}
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
              title="预览已过期或内容已变化，确认资格已清除。请保留当前输入并重新预览。"
            />
          ) : null}
          {previewError && !previewRequiresBranch(previewError) ? (
            <ContextFeedback
              showIcon
              type="error"
              title="仓库预览失败"
              description={repositoryFailureText(previewError)}
            />
          ) : null}
          {preview ? (
            <RepositoryPreviewReview preview={preview} canTrustPublisher={canTrustPublisher} />
          ) : null}
          {canTrustPublisher && publisherToTrust ? (
            <RepositoryPublisherTrust
              acknowledged={trustAcknowledged}
              onAcknowledgedChange={setTrustAcknowledged}
              onTrust={() => void previewRepository(true)}
            />
          ) : null}
          {operationError ? (
            <ContextFeedback
              showIcon
              type="error"
              title={mutation.outcomeUnknown ? '安装结果不明，请先核对已安装记录' : '仓库安装失败'}
              description={
                <Space orientation="vertical" size={6}>
                  <span>{repositoryFailureText(operationError)}</span>
                  {conflictPluginId ? (
                    <Button
                      size="small"
                      onClick={() => {
                        close()
                        navigate(`/plugins/installed/${encodeURIComponent(conflictPluginId)}`)
                      }}
                    >
                      查看已安装记录与当前来源
                    </Button>
                  ) : null}
                </Space>
              }
            />
          ) : null}
          {installed ? (
            <div className="soha-plugin-repository-installed" role="status">
              <Text strong>安装记录已保存：{installed.name}</Text>
              <Descriptions
                column={1}
                size="small"
                items={[
                  { key: 'version', label: '版本', children: installed.version },
                  { key: 'status', label: '状态', children: pluginStatusBadge(installed.status) },
                  {
                    key: 'source',
                    label: '来源',
                    children: installed.sourceInfo?.canonicalSource || installed.source,
                  },
                ]}
              />
              <Text type="secondary">
                {installedStatusText(installed)}；仓库安装默认不启用，需按需配置和启用。
              </Text>
            </div>
          ) : null}
          {mutation.feedback}
        </Space>
      </Modal>
    ),
  }
}
