import { Alert, Button, Descriptions, Modal, Space, Tag } from 'antd'
import type { ReactNode } from 'react'
import type { DirectoryChangeCounts, DirectorySyncPreview } from './types'

function ChangeCounts({ counts }: { counts?: DirectoryChangeCounts }) {
  if (!counts) return <>-</>
  return (
    <Space wrap size={4}>
      <Tag color="success">新增 {counts.create ?? 0}</Tag>
      <Tag color="processing">更新 {counts.update ?? 0}</Tag>
      <Tag color="warning">移动 {counts.move ?? 0}</Tag>
      <Tag>归档 {counts.archive ?? 0}</Tag>
      {(counts.conflict ?? 0) > 0 ? <Tag color="error">冲突 {counts.conflict}</Tag> : null}
    </Space>
  )
}

export function ChangePreviewModal({
  loading,
  onClose,
  onSync,
  open,
  preview,
  syncing,
  blocked = false,
  feedback,
  target,
}: {
  loading: boolean
  onClose: () => void
  onSync: () => void
  open: boolean
  preview: DirectorySyncPreview | null
  syncing: boolean
  blocked?: boolean
  feedback?: ReactNode
  target?: string
}) {
  return (
    <Modal
      title={target ? `同步变更预览 · ${target}` : '同步变更预览'}
      open={open}
      footer={
        preview
          ? [
              <Button key="cancel" onClick={onClose} disabled={syncing}>
                取消
              </Button>,
              <Button
                key="sync"
                type="primary"
                onClick={onSync}
                loading={syncing}
                disabled={blocked || loading}
              >
                立即同步
              </Button>,
            ]
          : null
      }
      onCancel={onClose}
      closable={{ disabled: syncing }}
      keyboard={!syncing}
      loading={loading}
    >
      {feedback}
      {preview ? (
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Alert showIcon type="info" title="当前仅为预览，尚未写入组织或用户数据" />
          <Descriptions column={1} size="small" bordered>
            <Descriptions.Item label="组织">
              <ChangeCounts counts={preview.organizations} />
            </Descriptions.Item>
            {preview.people ? (
              <Descriptions.Item label="人员">
                <ChangeCounts counts={preview.people} />
              </Descriptions.Item>
            ) : null}
          </Descriptions>
          {preview.warnings?.map((warning) => (
            <Alert key={warning} showIcon type="warning" title={warning} />
          ))}
        </Space>
      ) : null}
    </Modal>
  )
}
