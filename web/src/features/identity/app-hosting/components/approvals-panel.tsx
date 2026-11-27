import { ContextFeedback } from '@/components/context-feedback'
import { ManagementDataPage } from '@/components/management-data-page'
import {
  ManagementDensityButton,
  ManagementQueryActions,
  ManagementQueryScope,
  ManagementRefreshButton,
  ManagementState,
  ManagementTableToolbar,
} from '@/components/management-list'
import { operationModalStyles } from '@/components/modal-styles'
import { StatusTag } from '@/components/status-tag'
import { TableRowActionMenu } from '@/components/table-row-action-menu'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { tableColumnPresets } from '@/utils/table-columns'
import { formatDateTime } from '@/utils/time'
import { CheckOutlined, CloseOutlined } from '@ant-design/icons'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type { TableColumnsType } from 'antd'
import { Button, Form, Input, Modal, Space, Typography } from 'antd'
import { useEffect, useMemo, useState } from 'react'
import { formatApprovalStatus, formatInvalidationReason, formatVisibilityScope } from '../model'
import { appHostingAdminMutations } from '../mutations'
import { appHostingAdminQueries } from '../queries'
import type { HostedAppVisibilityApproval, HostedAppVisibilityApprovalStatus } from '../types'

const { Text } = Typography

type ApprovalStatusFilter = HostedAppVisibilityApprovalStatus | 'all'

const APPROVAL_STATUS_SCOPE_OPTIONS: Array<{ label: string; value: ApprovalStatusFilter }> = [
  { label: '待审批', value: 'pending' },
  { label: '已批准', value: 'approved' },
  { label: '已拒绝', value: 'rejected' },
  { label: '已撤回', value: 'withdrawn' },
  { label: '已失效', value: 'invalidated' },
  { label: '全部', value: 'all' },
]

export function VisibilityApprovalsPanel({
  canView,
  canManage,
  currentUserId,
}: {
  canView: boolean
  canManage: boolean
  currentUserId: string
}) {
  const queryClient = useQueryClient()
  const [statusFilter, setStatusFilter] = useState<ApprovalStatusFilter>('pending')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [tableSize, setTableSize] = useState<'small' | 'middle'>('small')

  const [approveTarget, setApproveTarget] = useState<HostedAppVisibilityApproval | null>(null)
  const [approveComment, setApproveComment] = useState('')

  const [rejectTarget, setRejectTarget] = useState<HostedAppVisibilityApproval | null>(null)
  const [rejectComment, setRejectComment] = useState('')
  const [rejectError, setRejectError] = useState<string>()

  const approvalsQuery = useQuery(
    appHostingAdminQueries.approvals(
      {
        status: statusFilter === 'all' ? undefined : statusFilter,
        limit: pageSize,
        offset: (page - 1) * pageSize,
      },
      canView,
    ),
  )
  const visibleApprovalsData =
    approvalsQuery.isEnabled === false ? undefined : readableQueryData(approvalsQuery)

  const approveMutation = useOperationMutation(
    appHostingAdminMutations.approveVisibility(queryClient),
    {
      label: ['批准全公司公开申请', 'Approve organization visibility'],
      target: (input) => input.appName || input.approvalId,
    },
  )
  const rejectMutation = useOperationMutation(
    appHostingAdminMutations.rejectVisibility(queryClient),
    {
      label: ['拒绝全公司公开申请', 'Reject organization visibility'],
      target: (input) => input.appName || input.approvalId,
    },
  )

  const operationBlocked =
    approveMutation.blocked ||
    rejectMutation.blocked ||
    (approvalsQuery.isEnabled !== false && queryAccessDenied(approvalsQuery))

  useEffect(() => {
    if (approveTarget) {
      setApproveComment('')
    }
  }, [approveTarget])

  useEffect(() => {
    if (rejectTarget) {
      setRejectComment('')
      setRejectError(undefined)
    }
  }, [rejectTarget])

  const columns = useMemo<TableColumnsType<HostedAppVisibilityApproval>>(
    () => [
      {
        title: '应用名称与子域',
        dataIndex: 'appName',
        key: 'appName',
        width: 220,
        render: (_, record) => (
          <Space orientation="vertical" size={2}>
            <Text strong>{record.appName}</Text>
            <Text type="secondary" code className="soha-identity-app-hosting-subtext">
              {record.appDnsLabel}
            </Text>
          </Space>
        ),
      },
      {
        title: '申请人与部门',
        key: 'requester',
        width: 190,
        render: (_, record) => {
          const displayName = record.requesterProfile?.displayName || record.requesterUserId
          const departments = record.requesterProfile?.teamNames ?? []
          return (
            <Space orientation="vertical" size={2}>
              <Text>{displayName}</Text>
              <Text type="secondary" className="soha-identity-app-hosting-subtext">
                {departments.length > 0 ? departments.join('、') : '未标注部门'}
              </Text>
            </Space>
          )
        },
      },
      {
        title: '范围变更',
        key: 'visibilityTransition',
        width: 230,
        render: (_, record) => (
          <Text>
            {`${formatVisibilityScope(record.currentVisibility)} → ${formatVisibilityScope(record.requestedVisibility)}`}
          </Text>
        ),
      },
      {
        title: '申请说明',
        dataIndex: 'reason',
        key: 'reason',
        width: 240,
        render: (reason?: string) => (
          <Text ellipsis title={reason}>
            {reason?.trim() || '未填写申请说明'}
          </Text>
        ),
      },
      {
        ...tableColumnPresets.datetime,
        title: '提交时间',
        dataIndex: 'createdAt',
        key: 'createdAt',
        width: 170,
        render: (value: string, record) => (
          <Space orientation="vertical" size={2}>
            <Text>{formatDateTime(value)}</Text>
            {record.decidedAt ? (
              <Text type="secondary" className="soha-identity-app-hosting-subtext">
                {`处置：${formatDateTime(record.decidedAt)}`}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        ...tableColumnPresets.status,
        title: '状态与意见',
        dataIndex: 'status',
        key: 'status',
        width: 240,
        render: (status: HostedAppVisibilityApprovalStatus, record) => {
          const meta = formatApprovalStatus(status)
          const isSelfPending =
            status === 'pending' &&
            Boolean(currentUserId) &&
            record.requesterUserId === currentUserId
          return (
            <Space orientation="vertical" size={2}>
              <StatusTag label={meta.label} value={meta.value} />
              {isSelfPending ? (
                <Text type="warning" className="soha-identity-app-hosting-self-guard">
                  申请人本人需回避审批，请由其他管理员处理
                </Text>
              ) : null}
              {status === 'invalidated' ? (
                <Text
                  type="warning"
                  ellipsis
                  title={formatInvalidationReason(record.invalidationReason)}
                  className="soha-identity-app-hosting-subtext"
                >
                  {`失效原因：${formatInvalidationReason(record.invalidationReason)}`}
                </Text>
              ) : null}
              {(status === 'rejected' || status === 'approved') && record.reviewComment ? (
                <Text
                  type={status === 'rejected' ? 'danger' : 'secondary'}
                  ellipsis
                  title={record.reviewComment}
                  className="soha-identity-app-hosting-subtext"
                >
                  {`审批意见：${record.reviewComment}`}
                </Text>
              ) : null}
            </Space>
          )
        },
      },
      {
        ...tableColumnPresets.action,
        title: '操作',
        key: 'actions',
        render: (_, record) => {
          if (record.status !== 'pending') {
            return <Text type="secondary">已处理</Text>
          }

          const isSelfRequest = Boolean(currentUserId) && record.requesterUserId === currentUserId
          const disabled = !canManage || isSelfRequest || operationBlocked

          return (
            <TableRowActionMenu
              targetLabel={record.appName}
              items={[
                {
                  key: 'approve',
                  icon: <CheckOutlined />,
                  label: '批准',
                  disabled,
                  onClick: () => setApproveTarget(record),
                },
                { type: 'divider' },
                {
                  key: 'reject',
                  icon: <CloseOutlined />,
                  label: '拒绝',
                  danger: true,
                  disabled,
                  onClick: () => setRejectTarget(record),
                },
              ]}
            />
          )
        },
      },
    ],
    [canManage, currentUserId, operationBlocked],
  )

  const handleConfirmApprove = () => {
    if (!approveTarget || !canManage || operationBlocked) return
    if (currentUserId && approveTarget.requesterUserId === currentUserId) return
    approveMutation.mutate(
      {
        approvalId: approveTarget.id,
        appName: approveTarget.appName,
        comment: approveComment.trim() || undefined,
      },
      {
        onSuccess: () => setApproveTarget(null),
      },
    )
  }

  const handleConfirmReject = () => {
    if (!rejectTarget || !canManage || operationBlocked) return
    if (currentUserId && rejectTarget.requesterUserId === currentUserId) return
    const trimmed = rejectComment.trim()
    if (!trimmed) {
      setRejectError('请填写拒绝原因')
      return
    }
    setRejectError(undefined)
    rejectMutation.mutate(
      {
        approvalId: rejectTarget.id,
        appName: rejectTarget.appName,
        comment: trimmed,
      },
      {
        onSuccess: () => setRejectTarget(null),
      },
    )
  }

  return (
    <div className="soha-identity-app-hosting-section">
      {!approveTarget && approveMutation.feedback}
      {!rejectTarget && rejectMutation.feedback}

      <ManagementDataPage
        className="soha-identity-app-hosting-approvals"
        query={{
          onFinish: () => setPage(1),
          actions: (
            <ManagementQueryActions
              disabledReset={statusFilter === 'pending'}
              loading={approvalsQuery.isFetching}
              onReset={() => {
                setStatusFilter('pending')
                setPage(1)
              }}
            />
          ),
          children: (
            <ManagementQueryScope
              label="审批状态"
              options={APPROVAL_STATUS_SCOPE_OPTIONS}
              value={statusFilter}
              onChange={(value) => {
                setStatusFilter(value as ApprovalStatusFilter)
                setPage(1)
              }}
            />
          ),
        }}
        table={{
          columns,
          columnSettingIconOnly: true,
          columnSettingPlacement: 'header',
          dataSource: visibleApprovalsData?.items ?? [],
          empty:
            approvalsQuery.isError && !visibleApprovalsData ? null : (
              <ManagementState
                kind="empty"
                title={
                  statusFilter === 'pending'
                    ? '当前没有待审批的全公司公开申请'
                    : '暂无匹配的审批记录'
                }
                description="当平台启用「须管理员审批」策略且员工申请将应用设为全公司公开时，待审记录会出现在此处。"
              />
            ),
          error: approvalsQuery.isEnabled === false ? null : approvalsQuery.error,
          hasReadData: visibleApprovalsData !== undefined,
          headerExtra: (
            <ManagementTableToolbar>
              <ManagementDensityButton
                aria-label="切换审批表格密度"
                tooltip="切换审批表格密度"
                onClick={() =>
                  setTableSize((current) => (current === 'small' ? 'middle' : 'small'))
                }
              />
              <ManagementRefreshButton
                aria-label="刷新审批队列"
                loading={approvalsQuery.isFetching}
                tooltip="刷新审批队列"
                onClick={() => {
                  if (approvalsQuery.isEnabled !== false) void approvalsQuery.refetch()
                }}
              />
            </ManagementTableToolbar>
          ),
          loading: approvalsQuery.isLoading || approvalsQuery.isFetching,
          onRetry: () => {
            if (approvalsQuery.isEnabled !== false) void approvalsQuery.refetch()
          },
          pagination: {
            current: page,
            currentPage: page,
            pageSize,
            total: visibleApprovalsData?.total ?? 0,
            onChange: (nextPage: number, nextPageSize: number) => {
              if (nextPageSize !== pageSize) {
                setPageSize(nextPageSize)
                setPage(1)
              } else {
                setPage(nextPage)
              }
            },
          },
          rowKey: 'id',
          scroll: { x: 1480 },
          tableSize,
        }}
      />

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(approveTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={approveTarget ? `批准全公司公开 · ${approveTarget.appName}` : '批准全公司公开'}
        width={520}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setApproveTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              disabled={!canManage || operationBlocked}
              loading={approveMutation.isPending}
              type="primary"
              onClick={handleConfirmApprove}
            >
              确认批准
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setApproveTarget(null)
        }}
      >
        {approveMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="info"
            title="批准后将立即把可见范围扩大至全公司公开"
            description={
              approveTarget
                ? `申请人：${approveTarget.requesterProfile?.displayName || approveTarget.requesterUserId} · 当前范围：${formatVisibilityScope(approveTarget.currentVisibility)} → 全公司公开`
                : undefined
            }
          />
          <Form layout="vertical">
            <Form.Item
              label={<label htmlFor="soha-app-hosting-approve-comment">审批备注（可选）</label>}
            >
              <Input.TextArea
                id="soha-app-hosting-approve-comment"
                aria-label="审批备注"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="可选填写审批意见，将同步记录至治理审计"
                rows={3}
                value={approveComment}
                onChange={(event) => setApproveComment(event.target.value)}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>

      <Modal
        closable={!operationBlocked}
        destroyOnHidden
        mask={{ closable: false }}
        open={Boolean(rejectTarget)}
        style={{ top: 32 }}
        styles={operationModalStyles}
        title={rejectTarget ? `拒绝全公司公开 · ${rejectTarget.appName}` : '拒绝全公司公开'}
        width={520}
        footer={
          <Space>
            <Button
              disabled={operationBlocked}
              onClick={() => {
                if (!operationBlocked) setRejectTarget(null)
              }}
            >
              取消
            </Button>
            <Button
              danger
              disabled={!canManage || operationBlocked}
              loading={rejectMutation.isPending}
              type="primary"
              onClick={handleConfirmReject}
            >
              确认拒绝
            </Button>
          </Space>
        }
        onCancel={() => {
          if (!operationBlocked) setRejectTarget(null)
        }}
      >
        {rejectMutation.feedback}
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <ContextFeedback
            type="warning"
            title="拒绝后应用将维持当前可见范围不变"
            description="拒绝原因将向作者展示并写入审计日志。"
          />
          <Form layout="vertical">
            <Form.Item
              help={rejectError}
              label={<label htmlFor="soha-app-hosting-reject-comment">拒绝原因</label>}
              required
              validateStatus={rejectError ? 'error' : undefined}
            >
              <Input.TextArea
                id="soha-app-hosting-reject-comment"
                aria-label="拒绝原因"
                disabled={!canManage || operationBlocked}
                maxLength={500}
                placeholder="请说明拒绝全公司公开的原因或建议收敛的受众范围（必填）"
                rows={3}
                value={rejectComment}
                onChange={(event) => {
                  setRejectComment(event.target.value)
                  if (rejectError && event.target.value.trim()) {
                    setRejectError(undefined)
                  }
                }}
              />
            </Form.Item>
          </Form>
        </Space>
      </Modal>
    </div>
  )
}
