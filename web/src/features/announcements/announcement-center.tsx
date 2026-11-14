import { useEffect, useMemo, useState } from 'react'
import { App, Badge, Button, Modal, Popover, Space, Tag, Tabs, Typography } from 'antd'
import { BellOutlined } from '@ant-design/icons'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { HeaderActionButton } from '@/components/header-action-button'
import { ManagementState } from '@/components/management-list'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { localeText, useI18n } from '@/i18n'
import type { LocaleCode } from '@/i18n'
import { formatDateTime } from '@/utils/time'
import { markAnnouncementRead } from './api'
import { announcementKeys } from './keys'
import type { AnnouncementInboxItem } from './types'
import { useAnnouncementInbox } from './use-inbox'

export type { AnnouncementInboxItem } from './types'

const { Paragraph, Text, Title } = Typography

export const announcementInboxQueryKey = announcementKeys.inboxRoot

function levelTag(level: string, localeCode: LocaleCode) {
  const normalized = String(level || '').toLowerCase()
  if (normalized === 'critical') {
    return <Tag color="red">{localeText(localeCode, '严重', 'Critical')}</Tag>
  }
  if (normalized === 'warning') {
    return <Tag color="gold">{localeText(localeCode, '警告', 'Warning')}</Tag>
  }
  return <Tag color="blue">{localeText(localeCode, '信息', 'Info')}</Tag>
}

function formatAnnouncementTime(item: AnnouncementInboxItem) {
  return formatDateTime(item.publishedAt || item.updatedAt || item.createdAt)
}

function AnnouncementList({
  items,
  onRead,
  onPreview,
  readingID,
  localeCode,
}: {
  items: AnnouncementInboxItem[]
  onRead: (id: string) => void
  onPreview: (item: AnnouncementInboxItem) => void
  readingID?: string | null
  localeCode: LocaleCode
}) {
  if (items.length === 0) {
    return (
      <ManagementState
        bordered={false}
        compact
        title={localeText(localeCode, '暂无公告', 'No announcements')}
        description={localeText(
          localeCode,
          '当前没有需要处理的公告。',
          'There are no announcements requiring attention.',
        )}
      />
    )
  }

  return (
    <div className="soha-announcement-center-list" role="list">
      {items.map((item) => (
        <div className="soha-announcement-center-item" key={item.id} role="listitem">
          <div className="soha-announcement-center-item__body">
            <Space size={8} wrap>
              <Button
                type="link"
                className="soha-system-linklike"
                onClick={() => onPreview(item)}
              >
                {item.title}
              </Button>
              {item.sticky ? (
                <Tag color="purple">{localeText(localeCode, '置顶', 'Pinned')}</Tag>
              ) : null}
              {levelTag(item.level, localeCode)}
            </Space>
            <Space orientation="vertical" size={4} style={{ width: '100%' }}>
              <Paragraph
                style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}
                ellipsis={{
                  rows: 2,
                  expandable: true,
                  symbol: localeText(localeCode, '展开', 'Expand'),
                }}
              >
                {item.content}
              </Paragraph>
              <Text type="secondary">{formatAnnouncementTime(item)}</Text>
            </Space>
          </div>
          <div className="soha-announcement-center-item__actions">
            <Button size="small" type="link" onClick={() => onPreview(item)}>
              {localeText(localeCode, '查看', 'View')}
            </Button>
            {item.isRead ? (
              <Text type="secondary">
                {item.readAt
                  ? `${localeText(localeCode, '已读', 'Read')} ${formatDateTime(item.readAt)}`
                  : localeText(localeCode, '已读', 'Read')}
              </Text>
            ) : (
              <Button
                size="small"
                type="link"
                loading={readingID === item.id}
                onClick={() => onRead(item.id)}
              >
                {localeText(localeCode, '标记已读', 'Mark as read')}
              </Button>
            )}
          </div>
        </div>
      ))}
    </div>
  )
}

export function AnnouncementBell() {
  const { localeCode } = useI18n()
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const canViewAnnouncements = hasPermission(
    permissionSnapshotQuery.data?.data,
    'system.announcements.view',
  )
  const inboxQuery = useAnnouncementInbox(10)
  const [modalItem, setModalItem] = useState<AnnouncementInboxItem | null>(null)
  const [autoOpenedID, setAutoOpenedID] = useState<string | null>(null)

  const items = inboxQuery.data?.data.items ?? []
  const unreadCount = inboxQuery.data?.data.unreadCount ?? 0
  const unreadItems = useMemo(() => items.filter((item) => !item.isRead), [items])
  const topUnread = useMemo(() => unreadItems[0] ?? null, [unreadItems])

  useEffect(() => {
    if (!canViewAnnouncements) return
    if (!topUnread) return
    if (autoOpenedID === topUnread.id) return
    setModalItem(topUnread)
    setAutoOpenedID(topUnread.id)
  }, [autoOpenedID, canViewAnnouncements, topUnread])

  const markReadMutation = useMutation({
    mutationFn: markAnnouncementRead,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: announcementInboxQueryKey })
      void message.success(
        localeText(localeCode, '公告已标记为已读', 'Announcement marked as read'),
      )
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const handleMarkRead = async (announcementID: string) => {
    await markReadMutation.mutateAsync(announcementID)
    setModalItem((current) => (current?.id === announcementID ? null : current))
  }

  if (!canViewAnnouncements) return null

  return (
    <>
      <Popover
        placement="bottomRight"
        trigger="click"
        content={
          <div style={{ width: 420, maxWidth: 'min(92vw, 420px)' }}>
            <div
              style={{
                display: 'flex',
                justifyContent: 'space-between',
                alignItems: 'center',
                marginBottom: 12,
              }}
            >
              <Title level={5} style={{ margin: 0 }}>
                {localeText(localeCode, '公告中心', 'Announcements')}
              </Title>
              <Text type="secondary">
                {localeCode === 'zh_CN' ? `${unreadCount} 条未读` : `${unreadCount} unread`}
              </Text>
            </div>
            <Tabs
              size="small"
              items={[
                {
                  key: 'unread',
                  label: `${localeText(localeCode, '未读', 'Unread')} (${unreadCount})`,
                  children: (
                    <AnnouncementList
                      items={unreadItems}
                      localeCode={localeCode}
                      onRead={handleMarkRead}
                      onPreview={setModalItem}
                      readingID={markReadMutation.variables as string | null}
                    />
                  ),
                },
                {
                  key: 'all',
                  label: `${localeText(localeCode, '全部', 'All')} (${items.length})`,
                  children: (
                    <AnnouncementList
                      items={items}
                      localeCode={localeCode}
                      onRead={handleMarkRead}
                      onPreview={setModalItem}
                      readingID={markReadMutation.variables as string | null}
                    />
                  ),
                },
              ]}
            />
          </div>
        }
      >
        <HeaderActionButton
          ariaLabel={
            unreadCount > 0
              ? localeCode === 'zh_CN'
                ? `公告中心，${unreadCount} 条未读`
                : `Announcements, ${unreadCount} unread`
              : localeText(localeCode, '公告中心', 'Announcements')
          }
          className="soha-header-bell"
          icon={
            <Badge count={unreadCount} size="small" overflowCount={99}>
              <BellOutlined />
            </Badge>
          }
        />
      </Popover>

      <Modal
        open={Boolean(modalItem)}
        title={modalItem?.title || localeText(localeCode, '公告', 'Announcement')}
        onCancel={() => setModalItem(null)}
        footer={[
          <Button key="close" onClick={() => setModalItem(null)}>
            {localeText(localeCode, '稍后查看', 'Later')}
          </Button>,
          <Button
            key="read"
            type="primary"
            loading={markReadMutation.isPending}
            onClick={() => {
              if (modalItem) void handleMarkRead(modalItem.id)
            }}
          >
            {localeText(localeCode, '已读', 'Mark as read')}
          </Button>,
        ]}
        destroyOnHidden
      >
        {modalItem ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Space size={8} wrap>
              {modalItem.sticky ? (
                <Tag color="purple">{localeText(localeCode, '置顶', 'Pinned')}</Tag>
              ) : null}
              {levelTag(modalItem.level, localeCode)}
              <Text type="secondary">{formatAnnouncementTime(modalItem)}</Text>
            </Space>
            <Paragraph style={{ marginBottom: 0, whiteSpace: 'pre-wrap' }}>
              {modalItem.content}
            </Paragraph>
          </Space>
        ) : null}
      </Modal>
    </>
  )
}
