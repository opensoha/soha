import { AdminTable } from '@/components/admin-table'
import { ManagementIconButton, ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { StatusTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { formatDateTime } from '@/utils/time'
import { EditOutlined, ReloadOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { CalendarProps, TableProps } from 'antd'
import {
  App,
  Avatar,
  Badge,
  Button,
  Calendar,
  Card,
  Col,
  Descriptions,
  Drawer,
  Form,
  Modal,
  Row,
  Segmented,
  Select,
  Space,
  Statistic,
  Tag,
  Timeline,
  Tooltip,
  Typography,
} from 'antd'
import type { Dayjs } from 'dayjs'
import dayjs from 'dayjs'
import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import '../observability-pages.css'
import {
  assignmentForDate,
  buildRotationConfigWithOverride,
  formatParticipantSummary,
  normalizeParticipantList,
  ONCALL_DATE_FORMAT,
  onCallUserOptions,
  participantAvatarText,
  readRotationOverrides,
  rotationModeLabel,
  shiftsForDate,
} from './model'
import { observabilityOncallMutations } from './mutations'
import { observabilityOncallQueries } from './queries'
import type { OnCallBoardView, OnCallRotation, OnCallRotationPayload, OnCallTask } from './types'

const { Text } = Typography

export function OnCallBoardPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const permissionSnapshotQuery = usePermissionSnapshot()
  const permissionSnapshot = permissionSnapshotQuery.data?.data
  const canCreateOnCall = hasPermission(permissionSnapshot, 'observe.oncall.create')
  const canUpdateOnCall = hasPermission(permissionSnapshot, 'observe.oncall.update')
  const [overrideForm] = Form.useForm<{ participants: string[] }>()
  const [view, setView] = useState<OnCallBoardView>('calendar')
  const [calendarValue, setCalendarValue] = useState<Dayjs>(dayjs())
  const [selectedScheduleId, setSelectedScheduleId] = useState('')
  const [drawerDate, setDrawerDate] = useState<Dayjs | null>(null)
  const [overrideOpen, setOverrideOpen] = useState(false)
  const [overrideRotation, setOverrideRotation] = useState<OnCallRotation | null>(null)
  const [overrideDate, setOverrideDate] = useState<Dayjs | null>(null)

  const schedulesQuery = useQuery(observabilityOncallQueries.schedules())
  const rotationsQuery = useQuery(observabilityOncallQueries.rotations())
  const tasksQuery = useQuery(observabilityOncallQueries.tasks())
  const usersQuery = useQuery(observabilityOncallQueries.users(canUpdateOnCall))
  const updateRotationOverride = useOperationMutation(
    observabilityOncallMutations.updateRotation(queryClient),
    {
      label: ['更新值班覆盖', 'Update duty override'],
      target: ({ id }) => id,
      receipt: () => ({ message: '值班覆盖记录已保存' }),
    },
  )
  const denied = queryAccessDenied(schedulesQuery) || queryAccessDenied(rotationsQuery)
  const blocked = updateRotationOverride.blocked || denied || !readableQueryData(usersQuery)
  function closeOverride() {
    if (updateRotationOverride.blocked) return
    setOverrideOpen(false)
    setOverrideRotation(null)
    overrideForm.resetFields()
  }
  useEffect(() => {
    if (denied || queryAccessDenied(updateRotationOverride)) {
      setOverrideOpen(false)
      setOverrideRotation(null)
      overrideForm.resetFields()
    }
  }, [denied, updateRotationOverride.error, overrideForm])

  const schedules = readableQueryData(schedulesQuery) ?? []
  const rotations = readableQueryData(rotationsQuery) ?? []
  const tasks = readableQueryData(tasksQuery) ?? []
  const scheduleOptions = useMemo(
    () => schedules.map((item) => ({ value: item.id, label: item.name })),
    [schedules],
  )
  const effectiveScheduleId = schedules.some((item) => item.id === selectedScheduleId)
    ? selectedScheduleId
    : schedules[0]?.id || ''
  const selectedSchedule = useMemo(
    () => schedules.find((item) => item.id === effectiveScheduleId) ?? null,
    [effectiveScheduleId, schedules],
  )
  const selectedRotation = useMemo(
    () =>
      rotations.find((item) => item.scheduleId === effectiveScheduleId && item.enabled) ??
      rotations.find((item) => item.scheduleId === effectiveScheduleId) ??
      null,
    [effectiveScheduleId, rotations],
  )
  const overrides = useMemo(
    () => readRotationOverrides(selectedRotation?.rotationConfig),
    [selectedRotation],
  )
  const overrideEntries = useMemo(
    () => Object.entries(overrides).sort(([a], [b]) => (a < b ? 1 : a > b ? -1 : 0)),
    [overrides],
  )
  const today = dayjs()
  const todayAssignment = assignmentForDate(selectedRotation, today)
  const tomorrowAssignment = assignmentForDate(selectedRotation, today.add(1, 'day'))

  function openOverride(date: Dayjs) {
    if (
      !canUpdateOnCall ||
      updateRotationOverride.blocked ||
      denied ||
      !readableQueryData(rotationsQuery)
    )
      return
    updateRotationOverride.reset()
    overrideForm.resetFields()
    setOverrideRotation(selectedRotation)
    if (!selectedRotation) {
      message.warning('当前排班尚未配置轮值，请先到值班设置中创建')
      return
    }
    setOverrideDate(date)
    overrideForm.setFieldsValue({
      participants: assignmentForDate(selectedRotation, date).participants,
    })
    setOverrideOpen(true)
  }

  function updateOverride(participants: string[]) {
    if (!overrideRotation || !overrideDate || blocked || !canUpdateOnCall) return
    const payload: OnCallRotationPayload = {
      scheduleId: overrideRotation.scheduleId,
      name: overrideRotation.name,
      participants: normalizeParticipantList(overrideRotation.participants),
      rotationConfig: buildRotationConfigWithOverride(
        overrideRotation.rotationConfig,
        overrideDate.format(ONCALL_DATE_FORMAT),
        participants,
      ),
      enabled: overrideRotation.enabled,
    }
    updateRotationOverride.mutate(
      { id: overrideRotation.id, payload },
      {
        onSuccess: () => {
          setOverrideOpen(false)
          setOverrideRotation(null)
          overrideForm.resetFields()
        },
      },
    )
  }

  const cellRender: CalendarProps<Dayjs>['cellRender'] = (current, info) => {
    if (info.type !== 'date') return info.originNode
    const assignment = assignmentForDate(selectedRotation, current)
    if (assignment.participants.length === 0) {
      return (
        <div className="soha-oncall-cell soha-oncall-cell-empty">
          <Text type="secondary">未排班</Text>
        </div>
      )
    }
    const isToday = current.isSame(today, 'day')
    return (
      <Tooltip title={assignment.participants.join('、')}>
        <div
          className={`soha-oncall-cell${assignment.override ? ' soha-oncall-cell-override' : ''}${isToday ? ' soha-oncall-cell-today' : ''}`}
        >
          <Avatar.Group size="small" max={{ count: 3 }}>
            {assignment.participants.map((name) => (
              <Avatar
                key={name}
                style={{
                  backgroundColor: assignment.override
                    ? 'var(--soha-warning)'
                    : 'var(--soha-primary)',
                }}
              >
                {participantAvatarText(name)}
              </Avatar>
            ))}
          </Avatar.Group>
          {assignment.override ? <Badge status="warning" text="覆盖" /> : null}
        </div>
      </Tooltip>
    )
  }

  const taskColumns: TableProps<OnCallTask>['columns'] = [
    { title: '标题', dataIndex: 'title' },
    {
      title: '严重度',
      dataIndex: 'severity',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '状态',
      dataIndex: 'status',
      render: (value: string) => <StatusTag value={value} />,
    },
    {
      title: '当前响应人',
      dataIndex: 'currentParticipant',
      render: (value: string) => value || '-',
    },
    {
      title: '分派规则',
      dataIndex: 'routeName',
      render: (value: string) => value || '-',
    },
    { title: '更新时间', dataIndex: 'updatedAt', render: formatDateTime },
  ]
  const drawerAssignment = drawerDate ? assignmentForDate(selectedRotation, drawerDate) : null

  return (
    <div className="soha-page">
      {!overrideOpen ? updateRotationOverride.feedback : null}
      <Space wrap>
        <ManagementIconButton
          aria-label="刷新值班板"
          tooltip="刷新值班板"
          icon={<ReloadOutlined />}
          loading={schedulesQuery.isFetching || rotationsQuery.isFetching}
          onClick={() => {
            void schedulesQuery.refetch()
            void rotationsQuery.refetch()
            void tasksQuery.refetch()
          }}
        />
      </Space>
      <ReadFeedback label="排班" query={schedulesQuery} />
      <ReadFeedback label="轮值" query={rotationsQuery} />
      {schedulesQuery.isLoading ? (
        <ManagementState kind="loading" title="正在加载排班" />
      ) : !readableQueryData(schedulesQuery) ? null : schedules.length === 0 ? (
        <Card>
          <ManagementState
            bordered={false}
            compact
            description="尚未创建排班，请前往值班设置新增。"
            kind="not-configured"
            actions={
              canCreateOnCall ? (
                <Button
                  type="primary"
                  onClick={() => navigate('/monitoring-workbench/oncall/settings')}
                >
                  前往设置
                </Button>
              ) : null
            }
          />
        </Card>
      ) : (
        <>
          <Row gutter={16} className="soha-oncall-stats-row">
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="今日值班"
                  value={
                    readableQueryData(rotationsQuery)
                      ? formatParticipantSummary(todayAssignment.participants)
                      : '—'
                  }
                  styles={{ content: { fontSize: 18 } }}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="明日值班"
                  value={
                    readableQueryData(rotationsQuery)
                      ? formatParticipantSummary(tomorrowAssignment.participants)
                      : '—'
                  }
                  styles={{ content: { fontSize: 18 } }}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="轮换节奏"
                  value={
                    !readableQueryData(rotationsQuery)
                      ? '—'
                      : selectedRotation
                        ? rotationModeLabel(selectedRotation.rotationConfig)
                        : '未配置'
                  }
                  styles={{ content: { fontSize: 18 } }}
                />
              </Card>
            </Col>
            <Col xs={24} sm={12} md={6}>
              <Card>
                <Statistic
                  title="待响应任务"
                  value={readableQueryData(tasksQuery) ? tasks.length : '—'}
                  suffix={
                    overrideEntries.length > 0 ? (
                      <Tag color="orange">覆盖 {overrideEntries.length}</Tag>
                    ) : null
                  }
                />
              </Card>
            </Col>
          </Row>
          <Card
            className="soha-oncall-board-card"
            title={
              <Space>
                <Text strong>{selectedSchedule?.name || '未选择排班'}</Text>
                {selectedRotation ? (
                  <Tag color="blue">{selectedRotation.name}</Tag>
                ) : (
                  <Tag>{readableQueryData(rotationsQuery) ? '暂无轮值' : '轮值暂不可用'}</Tag>
                )}
                {todayAssignment.override ? <Tag color="orange">今日已覆盖</Tag> : null}
              </Space>
            }
            extra={
              <Space wrap>
                <Select
                  style={{ minWidth: 220 }}
                  placeholder="选择排班"
                  disabled={overrideOpen}
                  value={effectiveScheduleId || undefined}
                  options={scheduleOptions}
                  onChange={setSelectedScheduleId}
                />
                <Segmented
                  value={view}
                  onChange={(value) => setView(value as OnCallBoardView)}
                  options={[
                    { value: 'calendar', label: '月历' },
                    { value: 'timeline', label: '时间轴' },
                    { value: 'list', label: '覆盖列表' },
                  ]}
                />
                <Button onClick={() => navigate('/monitoring-workbench/oncall/settings')}>
                  值班设置
                </Button>
              </Space>
            }
          >
            {!readableQueryData(rotationsQuery) ? (
              <ManagementState description="轮值数据暂不可用" />
            ) : view === 'calendar' ? (
              <Calendar
                value={calendarValue}
                onPanelChange={setCalendarValue}
                onSelect={(value, selectInfo) => {
                  setCalendarValue(value)
                  if (selectInfo?.source === 'date') setDrawerDate(value)
                }}
                cellRender={cellRender}
              />
            ) : null}
            {readableQueryData(rotationsQuery) && view === 'timeline' ? (
              <Timeline
                className="soha-oncall-timeline"
                items={Array.from({ length: 14 }).map((_, index) => {
                  const date = today.add(index, 'day')
                  const assignment = assignmentForDate(selectedRotation, date)
                  return {
                    color: assignment.override ? 'orange' : index === 0 ? 'blue' : 'gray',
                    children: (
                      <Space orientation="vertical" size={2}>
                        <Text strong>
                          {date.format('MM-DD ddd')}
                          {index === 0 ? ' · 今日' : ''}
                        </Text>
                        <Text>{formatParticipantSummary(assignment.participants)}</Text>
                        {assignment.override ? <Tag color="orange">手动覆盖</Tag> : null}
                      </Space>
                    ),
                  }
                })}
              />
            ) : null}
            {readableQueryData(rotationsQuery) && view === 'list' ? (
              overrideEntries.length === 0 ? (
                <ManagementState bordered={false} compact description="暂无覆盖记录" />
              ) : (
                <AdminTable
                  enableDensity
                  refreshing={rotationsQuery.isFetching}
                  onRefresh={() => void rotationsQuery.refetch()}
                  columnSettingPlacement="header"
                  columnSettingIconOnly
                  shellClassName="soha-management-table-shell"
                  columns={[
                    { title: '日期', dataIndex: 'date' },
                    {
                      title: '值班人',
                      dataIndex: 'participants',
                      render: formatParticipantSummary,
                    },
                    {
                      title: '操作',
                      key: 'actions',
                      dataIndex: 'date',
                      render: (value: string) =>
                        canUpdateOnCall ? (
                          <ManagementIconButton
                            aria-label="编辑覆盖记录"
                            disabled={updateRotationOverride.blocked || denied}
                            size="small"
                            tooltip="编辑"
                            icon={<EditOutlined />}
                            onClick={() => openOverride(dayjs(value))}
                          />
                        ) : null,
                    },
                  ]}
                  dataSource={overrideEntries.map(([date, participants]) => ({
                    date,
                    participants,
                  }))}
                  rowKey="date"
                  pagination={false}
                />
              )
            ) : null}
          </Card>
          <AdminTable
            title="待响应任务"
            enableDensity
            error={tasksQuery.error}
            hasReadData={tasksQuery.data !== undefined}
            refreshing={tasksQuery.isFetching}
            onRefresh={() => void tasksQuery.refetch()}
            columnSettingPlacement="header"
            columnSettingIconOnly
            shellClassName="soha-management-table-shell"
            columns={taskColumns}
            dataSource={tasks}
            rowKey="id"
            loading={tasksQuery.isLoading}
            pagination={{ pageSize: 10 }}
          />
        </>
      )}

      <Drawer
        title={drawerDate ? `${drawerDate.format('YYYY-MM-DD ddd')} 值班详情` : '值班详情'}
        open={Boolean(drawerDate) && !denied}
        onClose={() => setDrawerDate(null)}
        size={420}
      >
        {drawerDate && drawerAssignment ? (
          <Space orientation="vertical" size={16} style={{ width: '100%' }}>
            <Descriptions column={1} size="small" bordered>
              <Descriptions.Item label="排班">{selectedSchedule?.name || '-'}</Descriptions.Item>
              <Descriptions.Item label="轮值">
                {selectedRotation?.name || '未配置'}
              </Descriptions.Item>
              <Descriptions.Item label="值班人">
                {formatParticipantSummary(drawerAssignment.participants)}
              </Descriptions.Item>
              <Descriptions.Item label="是否覆盖">
                {drawerAssignment.override ? (
                  <Tag color="orange">手动覆盖</Tag>
                ) : (
                  <Tag>遵循轮换</Tag>
                )}
              </Descriptions.Item>
            </Descriptions>
            {!drawerAssignment.override && selectedRotation ? (
              <Card size="small" title="当日班次">
                <Timeline
                  items={shiftsForDate(selectedRotation, drawerDate).map((slot) => ({
                    children: (
                      <Text>
                        {slot.start} - {slot.end} {slot.participant}
                      </Text>
                    ),
                  }))}
                />
              </Card>
            ) : null}
            {canUpdateOnCall ? (
              <Space>
                <Button
                  type="primary"
                  icon={<EditOutlined />}
                  disabled={
                    updateRotationOverride.blocked || denied || !readableQueryData(rotationsQuery)
                  }
                  onClick={() => openOverride(drawerDate)}
                >
                  覆盖当日
                </Button>
                <Button onClick={() => navigate('/monitoring-workbench/oncall/settings')}>
                  调整轮值
                </Button>
              </Space>
            ) : null}
          </Space>
        ) : null}
      </Drawer>

      <Modal
        className="soha-observability-modal"
        style={{ top: 32 }}
        classNames={{
          body: 'soha-observability-modal-body',
          header: 'soha-observability-modal-header',
        }}
        title={overrideDate ? `${overrideDate.format(ONCALL_DATE_FORMAT)} 值班覆盖` : '值班覆盖'}
        open={overrideOpen && !denied}
        closable={!updateRotationOverride.blocked}
        keyboard={!updateRotationOverride.blocked}
        mask={{ closable: false }}
        onCancel={closeOverride}
        footer={
          <Space>
            <Button disabled={updateRotationOverride.blocked} onClick={closeOverride}>
              取消
            </Button>
            <Button
              type="primary"
              disabled={blocked}
              onClick={() => overrideForm.submit()}
              loading={updateRotationOverride.isPending}
            >
              保存覆盖
            </Button>
            <Button
              danger
              disabled={
                blocked ||
                !overrideDate ||
                !readRotationOverrides(overrideRotation?.rotationConfig)[
                  overrideDate.format(ONCALL_DATE_FORMAT)
                ]
              }
              loading={updateRotationOverride.isPending}
              onClick={() => updateOverride([])}
            >
              清除覆盖
            </Button>
          </Space>
        }
        destroyOnHidden
      >
        {updateRotationOverride.feedback}
        <ReadFeedback label="值班人员选项" query={usersQuery} />
        <Form
          layout="vertical"
          disabled={blocked}
          form={overrideForm}
          onFinish={({ participants }) => updateOverride(normalizeParticipantList(participants))}
        >
          <Form.Item name="participants" label="当日值班人员">
            <Select
              mode="multiple"
              allowClear
              showSearch={{ optionFilterProp: 'label' }}
              placeholder="选择当日值班人员"
              options={onCallUserOptions(readableQueryData(usersQuery) ?? [])}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  )
}
