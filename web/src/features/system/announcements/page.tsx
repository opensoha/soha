import {
  ManagementIconButton,
  ManagementRefreshButton,
  ManagementToolbarSearch,
} from '@/components/management-list'
import {
  hiddenModalHeaderStyle,
  operationModalStyles,
  viewportModalStyle,
  visuallyHiddenModalTitleStyle,
} from '@/components/modal-styles'
import { OverviewMetricCard, type OverviewMetricItem } from '@/components/overview-visuals'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import {
  ClockCircleOutlined,
  DeleteOutlined,
  EditOutlined,
  EyeOutlined,
  FileTextOutlined,
  NotificationOutlined,
  PauseCircleOutlined,
  PlusOutlined,
  PushpinOutlined,
  SendOutlined,
  TeamOutlined,
} from '@ant-design/icons'
import type {
  AnnouncementReceipt,
  AnnouncementReceiptState,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Descriptions,
  Drawer,
  Form,
  Input,
  Modal,
  Pagination,
  Popconfirm,
  Segmented,
  Select,
  Space,
  Spin,
  Switch,
  Tabs,
  Tag,
  Typography,
} from 'antd'
import dayjs from 'dayjs'
import { useEffect, useMemo, useState } from 'react'
import { systemMutations } from '../mutations'
import { systemQueries } from '../queries'
import {
  buildAnnouncementDateRange,
  buildAnnouncementLifecycle,
  type Announcement,
  type AnnouncementDateRange,
  type AnnouncementDurationPreset,
} from '../system-model'
import './styles.css'

const { Paragraph, Text } = Typography
const MODAL_FORM_LAYOUT = {
  labelAlign: 'left' as const,
  labelCol: { flex: '120px' },
  wrapperCol: { flex: 'auto' },
}
type AnnouncementDurationOption = AnnouncementDurationPreset | 'custom'
const ANNOUNCEMENT_DURATION_OPTIONS = [
  { label: '1天', value: 'one-day' },
  { label: '3天', value: 'three-days' },
  { label: '1周', value: 'one-week' },
  { label: '1个月', value: 'one-month' },
] satisfies { label: string; value: AnnouncementDurationOption }[]
const RECEIPT_PAGE_SIZE = 15

export function AnnouncementsPage() {
  const queryClient = useQueryClient()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const [modalVisible, setModalVisible] = useState(false)
  const [rangePickerOpen, setRangePickerOpen] = useState(false)
  const [durationPreset, setDurationPreset] = useState<AnnouncementDurationOption>('custom')
  const [editing, setEditing] = useState<Announcement | null>(null)
  const [previewing, setPreviewing] = useState<Announcement | null>(null)
  const [receiptTarget, setReceiptTarget] = useState<Announcement | null>(null)
  const [receiptState, setReceiptState] = useState<AnnouncementReceiptState>('all')
  const [receiptKeyword, setReceiptKeyword] = useState('')
  const [receiptPage, setReceiptPage] = useState(1)
  const [statusView, setStatusView] = useState('all')
  const permissionSnapshot = readableQueryData(permissionSnapshotQuery)?.data
  const canCreateAnnouncement = hasPermission(permissionSnapshot, 'system.announcements.create')
  const canUpdateAnnouncement = hasPermission(permissionSnapshot, 'system.announcements.update')
  const canDeleteAnnouncement = hasPermission(permissionSnapshot, 'system.announcements.delete')
  const canPublishAnnouncement = hasPermission(permissionSnapshot, 'system.announcements.publish')
  const canWithdrawAnnouncement = hasPermission(permissionSnapshot, 'system.announcements.withdraw')
  const canSaveAnnouncement = editing ? canUpdateAnnouncement : canCreateAnnouncement
  const [announcementForm] = Form.useForm()

  const announcementQuery = useQuery(systemQueries.announcements())
  const announcements = readableQueryData(announcementQuery) ?? []
  const { isLoading } = announcementQuery
  const receiptQuery = useQuery(
    systemQueries.announcementReceipts(
      receiptTarget?.id ?? '',
      {
        keyword: receiptKeyword || undefined,
        state: receiptState,
        page: receiptPage,
        pageSize: RECEIPT_PAGE_SIZE,
      },
      Boolean(receiptTarget),
    ),
  )
  const createMutation = useOperationMutation(systemMutations.announcements.create(queryClient), {
    label: ['创建公告', '创建公告'],
    target: (v) => String(v.title || v.labelZh || '新记录'),
  })
  const updateMutation = useOperationMutation(systemMutations.announcements.update(queryClient), {
    label: ['保存公告', '保存公告'],
    target: (v) => v.id,
  })
  const publishMutation = useOperationMutation(systemMutations.announcements.publish(queryClient), {
    label: ['发布公告', '发布公告'],
    target: (id) => id,
  })
  const withdrawMutation = useOperationMutation(
    systemMutations.announcements.withdraw(queryClient),
    { label: ['撤回公告', '撤回公告'], target: (id) => id },
  )
  const deleteMutation = useOperationMutation(systemMutations.announcements.remove(queryClient), {
    label: ['删除公告', '删除公告'],
    target: (id) => id,
  })

  const receiptData = readableQueryData(receiptQuery)
  const denied = queryAccessDenied(announcementQuery)
  const blocked =
    denied ||
    [createMutation, updateMutation, publishMutation, withdrawMutation, deleteMutation].some(
      (m) => m.blocked,
    )
  useEffect(() => {
    if (!denied) return
    announcementForm.resetFields()
    setModalVisible(false)
    setEditing(null)
    setPreviewing(null)
    setReceiptTarget(null)
  }, [denied, announcementForm])
  const normalizeAnnouncementFormValues = (values: Record<string, unknown>) => {
    const { activeRange, ...payload } = values
    const [startsAt, endsAt] = (activeRange as AnnouncementDateRange | undefined) ?? []
    return {
      ...payload,
      startsAt: startsAt?.toISOString() ?? null,
      endsAt: endsAt?.toISOString() ?? null,
      audience: 'all',
    }
  }

  const handleSubmit = (values: Record<string, unknown>) => {
    if (blocked || !canSaveAnnouncement) return
    const payload = normalizeAnnouncementFormValues(values)
    if (editing) {
      updateMutation.mutate(
        { id: editing.id, values: payload },
        {
          onSuccess: () => {
            setModalVisible(false)
            setRangePickerOpen(false)
            setEditing(null)
            setDurationPreset('custom')
          },
        },
      )
    } else {
      createMutation.mutate(payload, {
        onSuccess: () => {
          setModalVisible(false)
          setRangePickerOpen(false)
          setDurationPreset('custom')
        },
      })
    }
  }

  const announcementSummary = useMemo(() => {
    const published = announcements.filter(
      (item) => buildAnnouncementLifecycle(item) === 'published',
    ).length
    const draft = announcements.filter(
      (item) => buildAnnouncementLifecycle(item) === 'draft',
    ).length
    const scheduled = announcements.filter(
      (item) => buildAnnouncementLifecycle(item) === 'scheduled',
    ).length
    const sticky = announcements.filter((item) => item.sticky).length
    return { published, draft, scheduled, sticky }
  }, [announcements])

  const filteredAnnouncements = useMemo(() => {
    if (statusView === 'all') return announcements
    return announcements.filter((item) => buildAnnouncementLifecycle(item) === statusView)
  }, [announcements, statusView])

  const overviewMetrics = [
    {
      key: 'published',
      label: '已发布',
      value: readableQueryData(announcementQuery) ? announcementSummary.published : '-',
      helper: '当前对用户可见的公告',
      icon: <NotificationOutlined />,
      tone: 'success',
    },
    {
      key: 'draft',
      label: '草稿',
      value: readableQueryData(announcementQuery) ? announcementSummary.draft : '-',
      helper: '仍在编辑，尚未推送',
      icon: <FileTextOutlined />,
    },
    {
      key: 'scheduled',
      label: '待生效',
      value: readableQueryData(announcementQuery) ? announcementSummary.scheduled : '-',
      helper: '已配置时间窗，等待生效',
      icon: <ClockCircleOutlined />,
      tone: 'warning',
    },
    {
      key: 'sticky',
      label: '置顶公告',
      value: readableQueryData(announcementQuery) ? announcementSummary.sticky : '-',
      helper: '优先出现在用户端弹窗与铃铛中',
      icon: <PushpinOutlined />,
    },
  ] satisfies OverviewMetricItem[]

  const announcementTabs = [
    { key: 'all', label: `全部 (${announcements.length})` },
    { key: 'published', label: `已发布 (${announcementSummary.published})` },
    { key: 'draft', label: `草稿 (${announcementSummary.draft})` },
    { key: 'scheduled', label: `待生效 (${announcementSummary.scheduled})` },
  ]

  const renderAnnouncementActions = (record: Announcement) => (
    <Space className="soha-row-action-icons">
      <ManagementIconButton
        aria-label="查看阅读情况"
        icon={<TeamOutlined />}
        size="small"
        tooltip="阅读情况"
        onClick={() => {
          setReceiptState('all')
          setReceiptKeyword('')
          setReceiptPage(1)
          setReceiptTarget(record)
        }}
      />
      <ManagementIconButton
        aria-label="预览公告"
        icon={<EyeOutlined />}
        size="small"
        tooltip="预览"
        onClick={() => setPreviewing(record)}
      />
      {canUpdateAnnouncement ? (
        <ManagementIconButton
          aria-label="编辑公告"
          disabled={blocked}
          icon={<EditOutlined />}
          size="small"
          tooltip="编辑"
          onClick={() => {
            setEditing(record)
            setDurationPreset('custom')
            setModalVisible(true)
          }}
        />
      ) : null}
      {canPublishAnnouncement && buildAnnouncementLifecycle(record) !== 'published' ? (
        <ManagementIconButton
          aria-label="发布公告"
          disabled={blocked}
          icon={<SendOutlined />}
          size="small"
          tooltip="发布"
          onClick={() => publishMutation.mutate(record.id)}
          loading={publishMutation.isPending && publishMutation.variables === record.id}
        />
      ) : null}
      {canWithdrawAnnouncement && buildAnnouncementLifecycle(record) === 'published' ? (
        <ManagementIconButton
          aria-label="撤回公告"
          disabled={blocked}
          icon={<PauseCircleOutlined />}
          size="small"
          tooltip="撤回"
          onClick={() => withdrawMutation.mutate(record.id)}
          loading={withdrawMutation.isPending && withdrawMutation.variables === record.id}
        />
      ) : null}
      {canDeleteAnnouncement ? (
        <Popconfirm
          title="确认删除？"
          disabled={blocked}
          okButtonProps={{ disabled: blocked }}
          onConfirm={() => deleteMutation.mutate(record.id)}
        >
          <ManagementIconButton
            aria-label="删除公告"
            disabled={blocked}
            danger
            icon={<DeleteOutlined />}
            size="small"
            tooltip="删除"
          />
        </Popconfirm>
      ) : null}
    </Space>
  )

  return (
    <div className="soha-page">
      <ReadFeedback query={announcementQuery} label="公告" />
      {!modalVisible && !denied ? (
        <>
          {createMutation.feedback}
          {updateMutation.feedback}
          {publishMutation.feedback}
          {withdrawMutation.feedback}
          {deleteMutation.feedback}
        </>
      ) : null}
      <ManagementRefreshButton
        aria-label="刷新公告"
        tooltip="刷新"
        loading={announcementQuery.isFetching}
        onClick={() => void announcementQuery.refetch()}
      />
      <div className="soha-overview-metric-grid">
        {overviewMetrics.map(({ key, ...item }) => (
          <OverviewMetricCard key={key} {...item} loading={isLoading} />
        ))}
      </div>

      <Card
        variant="outlined"
        className="soha-overview-panel-card"
        extra={
          canCreateAnnouncement ? (
            <Button
              icon={<PlusOutlined />}
              disabled={blocked}
              type="primary"
              onClick={() => {
                setEditing(null)
                setDurationPreset('one-day')
                setModalVisible(true)
              }}
            >
              新建公告
            </Button>
          ) : null
        }
      >
        <Tabs
          activeKey={statusView}
          onChange={setStatusView}
          items={announcementTabs.map((item) => ({
            key: item.key,
            label: item.label,
            children: isLoading ? (
              <Spin size="small" />
            ) : filteredAnnouncements.length ? (
              <ul className="soha-list-panel soha-system-announcement-list">
                {filteredAnnouncements.map((record: Announcement) => {
                  const lifecycle = buildAnnouncementLifecycle(record)
                  return (
                    <li key={record.id} className="soha-list-row soha-system-announcement-item">
                      <div className="soha-list-row-meta soha-system-announcement-main">
                        <Space size={8} wrap>
                          <Button
                            type="link"
                            className="soha-system-linklike"
                            onClick={() => setPreviewing(record)}
                          >
                            {record.title}
                          </Button>
                          <StatusTag value={record.level} />
                          <StatusTag value={record.status} />
                          {record.sticky ? <Tag color="purple">置顶</Tag> : null}
                          {lifecycle === 'scheduled' ? <Tag color="gold">待生效</Tag> : null}
                          {lifecycle === 'expired' ? <Tag>已过期</Tag> : null}
                        </Space>
                      </div>
                      <Paragraph
                        className="soha-system-announcement-content"
                        ellipsis={{ rows: 2 }}
                      >
                        {record.content}
                      </Paragraph>
                      <div className="soha-list-row-extra">
                        <div className="soha-system-announcement-extra">
                          <Text type="secondary">{`发布时间 ${formatDateTime(record.publishedAt || record.updatedAt || record.createdAt)}`}</Text>
                          <Text type="secondary">{`生效窗口 ${formatDateTime(record.startsAt)} ~ ${formatDateTime(record.endsAt)}`}</Text>
                        </div>
                        {renderAnnouncementActions(record)}
                      </div>
                    </li>
                  )
                })}
              </ul>
            ) : announcementQuery.isError && !readableQueryData(announcementQuery) ? null : (
              <Alert type="info" showIcon title="当前分组下暂无公告" />
            ),
          }))}
        />
      </Card>
      <Modal
        title={editing ? '编辑公告' : '新建公告'}
        open={modalVisible && !denied}
        closable={!blocked}
        keyboard={!blocked}
        cancelButtonProps={{ disabled: blocked }}
        onCancel={() => {
          setModalVisible(false)
          setRangePickerOpen(false)
          setEditing(null)
          setDurationPreset('custom')
        }}
        onOk={() => announcementForm.submit()}
        okText={editing ? '更新' : '创建'}
        cancelText="取消"
        confirmLoading={createMutation.isPending || updateMutation.isPending}
        okButtonProps={{ disabled: blocked || !canSaveAnnouncement }}
        width={720}
        mask={{ closable: false }}
        style={viewportModalStyle}
        styles={{
          ...operationModalStyles,
          header: hiddenModalHeaderStyle,
          title: visuallyHiddenModalTitleStyle,
        }}
        destroyOnHidden
      >
        {createMutation.feedback}
        {updateMutation.feedback}
        <Form
          disabled={blocked || !canSaveAnnouncement}
          {...MODAL_FORM_LAYOUT}
          form={announcementForm}
          preserve={false}
          onFinish={(values) => {
            if (!canSaveAnnouncement || blocked) return
            handleSubmit(values as Record<string, unknown>)
          }}
          initialValues={
            editing
              ? {
                  title: editing.title,
                  content: editing.content,
                  level: editing.level,
                  status: editing.status,
                  sticky: editing.sticky,
                  activeRange: [
                    editing.startsAt ? dayjs(editing.startsAt) : null,
                    editing.endsAt ? dayjs(editing.endsAt) : null,
                  ],
                }
              : {
                  level: 'info',
                  status: 'draft',
                  sticky: false,
                  activeRange: buildAnnouncementDateRange('one-day'),
                }
          }
        >
          <Form.Item name="title" label="标题" rules={[{ required: true, message: '请输入标题' }]}>
            <Input />
          </Form.Item>
          <Form.Item
            name="content"
            label="内容"
            rules={[{ required: true, message: '请输入内容' }]}
          >
            <Input.TextArea rows={4} />
          </Form.Item>
          <Form.Item name="level" label="级别">
            <Select
              options={[
                { value: 'info', label: '信息' },
                { value: 'warning', label: '警告' },
                { value: 'critical', label: '严重' },
              ]}
            />
          </Form.Item>
          <Form.Item name="status" label="状态">
            <Select
              options={[
                { value: 'draft', label: '草稿' },
                { value: 'published', label: '已发布' },
              ]}
            />
          </Form.Item>
          <Form.Item name="sticky" label="置顶" valuePropName="checked">
            <Switch />
          </Form.Item>
          <Form.Item name="activeRange" label="生效时间">
            <DatePicker.RangePicker
              className="soha-system-announcement-range"
              allowEmpty={[true, true]}
              open={rangePickerOpen}
              onOpenChange={setRangePickerOpen}
              placeholder={['开始时间', '结束时间']}
              popupClassName="soha-system-announcement-range-popup"
              renderExtraFooter={() => (
                <Segmented<AnnouncementDurationOption>
                  disabled={blocked}
                  block
                  aria-label="快捷时长"
                  className="soha-form-segmented"
                  options={ANNOUNCEMENT_DURATION_OPTIONS}
                  size="small"
                  value={durationPreset}
                  onChange={(preset) => {
                    if (preset === 'custom') {
                      setDurationPreset(preset)
                      return
                    }
                    const currentRange = announcementForm.getFieldValue('activeRange') as
                      | AnnouncementDateRange
                      | undefined
                    announcementForm.setFieldValue(
                      'activeRange',
                      buildAnnouncementDateRange(preset, currentRange?.[0] ?? dayjs()),
                    )
                    setDurationPreset(preset)
                    setRangePickerOpen(false)
                  }}
                />
              )}
              showTime
              style={{ width: '100%' }}
              onChange={() => {
                setDurationPreset('custom')
              }}
            />
          </Form.Item>
        </Form>
      </Modal>
      <Drawer
        title={previewing?.title || '公告详情'}
        open={!denied && Boolean(previewing)}
        onClose={() => setPreviewing(null)}
        size={560}
        destroyOnHidden
      >
        {previewing ? (
          <Space orientation="vertical" size={12} style={{ width: '100%' }}>
            <Descriptions
              bordered
              size="small"
              column={1}
              items={[
                {
                  key: 'status',
                  label: '状态',
                  children: (
                    <Space size={8} wrap>
                      <StatusTag value={previewing.level} />
                      <StatusTag value={previewing.status} />
                      {previewing.sticky ? <Tag color="purple">置顶</Tag> : null}
                    </Space>
                  ),
                },
                {
                  key: 'publishedAt',
                  label: '发布时间',
                  children: formatDateTime(
                    previewing.publishedAt || previewing.updatedAt || previewing.createdAt,
                  ),
                },
                {
                  key: 'window',
                  label: '生效窗口',
                  children: `${formatDateTime(previewing.startsAt)} ~ ${formatDateTime(previewing.endsAt)}`,
                },
              ]}
            />
            <Card
              size="small"
              variant="outlined"
              title="正文内容"
              className="soha-system-announcement-preview-content"
            >
              <Paragraph>{previewing.content}</Paragraph>
            </Card>
          </Space>
        ) : null}
      </Drawer>
      <Drawer
        title="阅读情况"
        open={!denied && Boolean(receiptTarget)}
        onClose={() => setReceiptTarget(null)}
        size={560}
        destroyOnHidden
      >
        <Space orientation="vertical" size={12} style={{ width: '100%' }}>
          <Segmented<AnnouncementReceiptState>
            block
            options={[
              {
                label: `全部 ${(receiptData?.readCount ?? 0) + (receiptData?.unreadCount ?? 0)}`,
                value: 'all',
              },
              { label: `已读 ${receiptData?.readCount ?? 0}`, value: 'read' },
              { label: `未读 ${receiptData?.unreadCount ?? 0}`, value: 'unread' },
            ]}
            size="small"
            value={receiptState}
            onChange={(state) => {
              setReceiptState(state)
              setReceiptPage(1)
            }}
          />
          <div className="soha-system-announcement-receipt-search">
            <ManagementToolbarSearch
              placeholder="搜索用户、用户名或邮箱"
              style={{ width: '100%' }}
              value={receiptKeyword}
              onChange={(keyword) => {
                setReceiptKeyword(keyword)
                setReceiptPage(1)
              }}
            />
            <ManagementRefreshButton
              aria-label="刷新阅读情况"
              loading={receiptQuery.isFetching}
              tooltip="刷新"
              onClick={() => void receiptQuery.refetch()}
            />
          </div>
          <ReadFeedback
            query={receiptQuery}
            label="公告阅读情况"
            canRead={Boolean(receiptTarget)}
          />
          <Spin spinning={receiptQuery.isLoading}>
            {receiptData?.items.length ? (
              <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                <ul className="soha-list-panel soha-system-announcement-receipt-list">
                  {receiptData.items.map((record: AnnouncementReceipt) => (
                    <li
                      key={record.userId}
                      className="soha-list-row soha-system-announcement-receipt-row"
                    >
                      <div className="soha-list-row-meta soha-system-announcement-receipt-identity">
                        <Text strong title={record.username}>
                          {record.username}
                        </Text>
                      </div>
                      <div className="soha-system-announcement-receipt-details">
                        <Text type="secondary" title={record.teamNames?.join('、') || '未加入组织'}>
                          {record.teamNames?.join('、') || '未加入组织'}
                        </Text>
                      </div>
                      <div className="soha-list-row-extra soha-system-announcement-receipt-status">
                        <StatusTag
                          value={record.isRead ? 'active' : 'pending'}
                          label={record.isRead ? '已读' : '未读'}
                        />
                      </div>
                    </li>
                  ))}
                </ul>
                {(receiptData.total ?? 0) > RECEIPT_PAGE_SIZE ? (
                  <Pagination
                    align="end"
                    current={receiptPage}
                    pageSize={RECEIPT_PAGE_SIZE}
                    showSizeChanger={false}
                    simple
                    size="small"
                    total={receiptData.total}
                    onChange={setReceiptPage}
                  />
                ) : null}
              </Space>
            ) : receiptQuery.isError ? null : (
              <Alert showIcon type="info" title="暂无符合条件的用户" />
            )}
          </Spin>
        </Space>
      </Drawer>
    </div>
  )
}
