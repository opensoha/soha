import { useEffect, useRef, useState } from 'react'
import { Alert, Button, Input, Modal, Popconfirm, Select, Space, Typography } from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import type {
  ManagedContentRecord,
  ManagedContentReviewInput,
  ManagedContentAuthorization,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { useAuthStore } from '@/stores/auth-store'
import { readableQueryData } from '@/utils/query-feedback'
import {
  recordsQuery,
  recordQuery,
  destinationQuery,
  reviewDestinationQuery,
  fileAction,
  authorize,
  appeal,
  review,
  saveDownload,
} from './api'
import { managedContentKeys } from './keys'
import { useTransferContext } from './context'
import { NetworkStateDialog } from './network-state'
import { TransferState, recordReceipt, uncertainRecord } from './feedback'

export function Records({ kind }: { kind: 'files' | 'records' | 'cases' }) {
  const { viewer, scope, access, text, resume } = useTransferContext()
  const client = useQueryClient()
  const [offset, setOffset] = useState(0)
  const [limit, setLimit] = useState(25)
  const [destination, setDestination] = useState('')
  const [copyFile, setCopyFile] = useState<ManagedContentRecord>()
  const [copyTarget, setCopyTarget] = useState('')
  const [dialog, setDialog] = useState<{ record: ManagedContentRecord; review: boolean }>()
  const [networkRecord, setNetworkRecord] = useState<ManagedContentRecord>()
  const [lookup, setLookup] = useState('')
  const [requestId, setRequestId] = useState('')
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const canRead = kind === 'cases' ? access.events : access.view
  const query = useQuery({
    ...recordsQuery(viewer, scope, kind, destination, offset, limit, canRead),
    refetchInterval: (q) =>
      q.state.status === 'error'
        ? false
        : q.state.data?.items.some((r) => r.state === 'prepared' || r.state === 'sending')
          ? 5000
          : false,
  })
  const data = canRead ? readableQueryData(query) : undefined
  const ownTargets = useQuery(destinationQuery(viewer, scope, kind !== 'cases' && access.view))
  const caseTargets = useQuery(
    reviewDestinationQuery(viewer, scope, kind === 'cases' && access.events),
  )
  const targets = kind === 'cases' ? caseTargets : ownTargets
  const choices = readableQueryData(targets) ?? []
  const detail = useQuery(recordQuery(viewer, scope, lookup, canRead && kind === 'records'))
  const found = readableQueryData(detail)
  const refresh = () => client.invalidateQueries({ queryKey: managedContentKeys.all })
  const operation = useOperationMutation<
    ManagedContentRecord,
    {
      record: ManagedContentRecord
      action: 'read' | 'copy' | 'delete'
      destinationId?: string
      retry?: boolean
    }
  >(
    {
      retry: false,
      mutationFn: async ({ record, action, destinationId, retry }) => {
        let authorization: ManagedContentAuthorization | undefined
        if (retry) {
          const issued = await authorize(
            viewer,
            record.id,
            record.appealStatus === 'approved' ? 'use_approval' : 'acknowledge',
          )
          authorization = { attemptId: record.id, token: issued.token }
        }
        const id = crypto.randomUUID()
        setRequestId(id)
        const source = retry ? record.sourceId : record.id
        if (!source)
          throw new Error(text('缺少原文件记录。', 'The original file record is missing.'))
        const result = await fileAction(viewer, source, action, id, destinationId, authorization)
        if (
          action === 'read' &&
          result.record.state === 'completed' &&
          result.contentBase64 !== undefined &&
          alive.current &&
          useAuthStore.getState().user?.userId === viewer
        ) {
          saveDownload(result.contentBase64, result.record.fileName)
        }
        delete result.contentBase64
        return result.record
      },
      onSuccess: (r) => {
        if (r.state === 'completed') setCopyFile(undefined)
        void refresh()
      },
    },
    {
      label: ['执行受管文件操作', 'Perform managed file operation'],
      target: (v) => v.record.id,
      resultError: uncertainRecord,
      receipt: (r) => recordReceipt(r, text),
    },
  )
  const retry = (record: ManagedContentRecord) => {
    if (record.action === 'upload' || record.action === 'paste') {
      resume(record)
      return
    }
    if (record.action === 'download' || record.action === 'copy')
      operation.mutate({
        record,
        action: record.action === 'download' ? 'read' : 'copy',
        destinationId: record.action === 'copy' ? record.destinationId : undefined,
        retry: true,
      })
  }
  const actions = (r: ManagedContentRecord) => {
    if (kind === 'files')
      return (
        <Space wrap>
          <Button
            size="small"
            disabled={!access.download || operation.blocked}
            onClick={() => operation.mutate({ record: r, action: 'read' })}
          >
            {text('检查并下载', 'Inspect and download')}
          </Button>
          <Button
            size="small"
            disabled={!access.create || !access.download || operation.blocked}
            onClick={() => {
              setCopyTarget('')
              setCopyFile(r)
            }}
          >
            {text('复制到批准目的地', 'Copy to approved destination')}
          </Button>
          <Popconfirm
            title={text('删除这条记录对应的存储对象？', 'Delete the stored object for this file?')}
            description={text(
              '仅操作自己的已完成文件，保留传输与审计记录。',
              'Only your completed file is removed. Transfer and audit records remain.',
            )}
            onConfirm={() => operation.mutate({ record: r, action: 'delete' })}
          >
            <Button danger size="small" disabled={!access.remove || operation.blocked}>
              {text('删除文件', 'Delete file')}
            </Button>
          </Popconfirm>
        </Space>
      )
    const own = r.actorId === viewer
    const retryFresh = Date.now() < Date.parse(r.createdAt) + 24 * 60 * 60 * 1000
    return (
      <Space wrap>
        {own &&
        r.state === 'blocked' &&
        (r.decision?.requiredAction === 'acknowledge' || r.appealStatus === 'approved') ? (
          <Popconfirm
            title={text(
              '仅重试原内容、目标和版本？',
              'Retry only the original content, destination and version?',
            )}
            onConfirm={() => retry(r)}
          >
            <Button
              size="small"
              disabled={
                operation.blocked ||
                !access.view ||
                !retryFresh ||
                (r.action === 'download' ? !access.download : !access.create) ||
                (r.action === 'copy' && !access.download)
              }
            >
              {r.action === 'upload' || r.action === 'paste'
                ? text('重新提供原内容', 'Provide original content again')
                : text('确认并重试原传输', 'Confirm and retry original transfer')}
            </Button>
          </Popconfirm>
        ) : null}
        {own &&
        r.state === 'blocked' &&
        r.decision?.requiredAction === 'appeal' &&
        r.appealStatus === 'none' ? (
          <Button
            size="small"
            disabled={operation.blocked || !access.view}
            onClick={() => setDialog({ record: r, review: false })}
          >
            {text('提交申诉', 'Submit appeal')}
          </Button>
        ) : null}
        {r.networkHoldId ? (
          <Button size="small" onClick={() => setNetworkRecord(r)}>
            {text('网络限制与回执', 'Network hold and receipts')}
          </Button>
        ) : null}
        {kind === 'cases' &&
        access.review &&
        !own &&
        (r.appealStatus === 'pending' ||
          (Boolean(r.networkHoldId) && r.appealStatus === 'rejected')) ? (
          <Button
            size="small"
            disabled={Boolean(r.networkHoldId) && !access.link}
            onClick={() => setDialog({ record: r, review: true })}
          >
            {text('独立审查', 'Independent review')}
          </Button>
        ) : null}
      </Space>
    )
  }
  return (
    <Space orientation="vertical" size={16} style={{ width: '100%' }}>
      {networkRecord ? (
        <NetworkStateDialog
          key={networkRecord.id}
          record={networkRecord}
          close={() => setNetworkRecord(undefined)}
        />
      ) : null}
      {kind === 'cases' ? (
        <>
          <Alert
            type="info"
            title={text(
              '审查不授予正文读取权，不能自审；批准仍需本人重新提交原内容。',
              'Review does not grant file-body access and cannot be self-approved. The owner must resubmit the original content after approval.',
            )}
          />
          <Select
            aria-label={text('选择审查目的连接', 'Select destination for review')}
            value={destination || undefined}
            onChange={(value) => {
              setDestination(value)
              setOffset(0)
            }}
            options={choices.map((d) => ({ value: d.id, label: d.name }))}
          />
        </>
      ) : null}
      {kind !== 'cases' || destination ? (
        <ReadFeedback
          canRead={canRead}
          query={query}
          label={
            kind === 'files'
              ? text('我的文件', 'My files')
              : text('内容传输记录', 'Content transfer records')
          }
        />
      ) : null}
      {!copyFile ? operation.feedback : null}
      {requestId ? (
        <Typography.Text copyable>
          {text('本次请求ID', 'Request ID')}: {requestId}
        </Typography.Text>
      ) : null}
      {operation.data ? (
        <>
          <TransferState record={operation.data} />
          <Typography.Text copyable>{operation.data.id}</Typography.Text>
        </>
      ) : null}
      <AdminTable
        rowKey="id"
        dataSource={data?.items ?? []}
        loading={query.isFetching}
        onRefresh={() => void refresh()}
        pagination={{
          current: Math.floor(offset / limit) + 1,
          pageSize: limit,
          total: data?.total ?? 0,
          showSizeChanger: true,
          pageSizeOptions: [10, 25, 50, 100],
          onChange: (page: number, size: number) => {
            setLimit(size)
            setOffset((page - 1) * size)
          },
        }}
        columns={[
          {
            title: text('文件 / 记录', 'File / Record'),
            key: 'file',
            render: (_: unknown, r: ManagedContentRecord) => (
              <Space orientation="vertical" size={2}>
                <Typography.Text>{r.fileName}</Typography.Text>
                <Typography.Text copyable type="secondary">
                  {r.id}
                </Typography.Text>
              </Space>
            ),
          },
          {
            title: text('状态', 'State'),
            key: 'state',
            render: (_: unknown, r: ManagedContentRecord) => <TransferState record={r} />,
          },
          {
            title: text('操作 / 大小', 'Action / Size'),
            key: 'action',
            render: (_: unknown, r: ManagedContentRecord) => (
              <span>
                {
                  {
                    upload: text('上传', 'Upload'),
                    paste: text('粘贴', 'Paste'),
                    copy: text('复制', 'Copy'),
                    download: text('下载', 'Download'),
                    delete: text('删除', 'Delete'),
                  }[r.action]
                }{' '}
                · {r.sizeBytes} B
              </span>
            ),
          },
          {
            title: text('策略 / 时间', 'Policies / Time'),
            key: 'policy',
            render: (_: unknown, r: ManagedContentRecord) => (
              <Space orientation="vertical" size={2}>
                {r.gates.map((g, index) => (
                  <Typography.Text key={`${g.destinationId}:${index}`}>
                    {g.destinationId} · v{g.policyVersion}
                  </Typography.Text>
                ))}
                <span>{r.updatedAt}</span>
              </Space>
            ),
          },
          {
            title: text('操作', 'Actions'),
            key: 'actions',
            render: (_: unknown, r: ManagedContentRecord) => actions(r),
          },
        ]}
      />
      {kind === 'records' ? (
        <>
          <Input.Search
            aria-label={text('按自己的请求ID查询', 'Find your request by ID')}
            maxLength={36}
            placeholder={text(
              '查询较早或未确认的请求ID',
              'Look up an older or unconfirmed request ID',
            )}
            enterButton={text('查询记录', 'Find record')}
            onSearch={(value) => setLookup(value.trim())}
          />
          {lookup ? (
            <>
              <ReadFeedback query={detail} label={text('指定记录', 'Requested record')} />
              {found ? (
                <>
                  <Typography.Text copyable>{found.id}</Typography.Text>
                  <TransferState record={found} />
                  {actions(found)}
                </>
              ) : null}
            </>
          ) : null}
        </>
      ) : null}
      <Typography.Paragraph type="secondary">
        {text(
          'prepared/sending 或结果未确认不能当成成功或已回滚。保留请求ID核对记录和目标，页面不会自动换新ID重发。自己的文件正文仅通过独立下载权限和当前策略读取；第三方直接访问存储不属于此入口。',
          'Pending or unconfirmed results are neither success nor rollback. Keep the request ID and inspect the record and target; the page never automatically replays under a new ID. File bodies require download permission and current policy. Direct third-party storage access is outside this entry.',
        )}
      </Typography.Paragraph>
      <Modal
        open={Boolean(copyFile)}
        title={text('复制到批准目的地', 'Copy to an approved destination')}
        footer={null}
        onCancel={() => {
          if (!operation.isPending) setCopyFile(undefined)
        }}
        closable={!operation.isPending}
        mask={{ closable: !operation.isPending }}
        destroyOnHidden
      >
        <Space orientation="vertical" size={16} style={{ width: '100%' }}>
          <Typography.Text>{copyFile?.fileName}</Typography.Text>
          <Select
            aria-label={text('复制目标', 'Copy destination')}
            value={copyTarget || undefined}
            onChange={setCopyTarget}
            options={choices.map((d) => ({
              value: d.id,
              label: `${d.name} · v${d.publishedVersion}`,
            }))}
          />
          {operation.feedback}
          <Button
            type="primary"
            disabled={!copyFile || !copyTarget || operation.blocked}
            loading={operation.isPending}
            onClick={() => {
              if (copyFile)
                operation.mutate({ record: copyFile, action: 'copy', destinationId: copyTarget })
            }}
          >
            {text('检查来源与目标后复制', 'Inspect source and destination, then copy')}
          </Button>
        </Space>
      </Modal>
      {dialog ? (
        <CaseDialog
          key={`${dialog.record.id}:${dialog.record.revision}`}
          record={dialog.record}
          reviewMode={dialog.review}
          close={() => setDialog(undefined)}
        />
      ) : null}
    </Space>
  )
}
function CaseDialog({
  record,
  reviewMode,
  close,
}: {
  record: ManagedContentRecord
  reviewMode: boolean
  close: () => void
}) {
  const { text, viewer, access } = useTransferContext()
  const client = useQueryClient()
  const [reason, setReason] = useState<
    'false_positive' | 'business_exception' | 'insufficient_justification'
  >(reviewMode ? 'insufficient_justification' : 'false_positive')
  const [resolution, setResolution] = useState<ManagedContentReviewInput['resolution']>(
    record.appealStatus === 'rejected' ? 'approve' : 'reject',
  )
  const save = useOperationMutation(
    {
      retry: false,
      mutationFn: () =>
        reviewMode
          ? review(viewer, record.id, {
              expectedRevision: record.revision,
              resolution,
              reasonCode: reason,
            })
          : appeal(
              viewer,
              record.id,
              reason === 'insufficient_justification' ? 'false_positive' : reason,
            ),
      onSuccess: () => void client.invalidateQueries({ queryKey: managedContentKeys.all }),
    },
    {
      label: reviewMode
        ? ['审查内容申诉', 'Review content appeal']
        : ['提交内容申诉', 'Submit content appeal'],
      target: () => record.id,
      receipt: (r) => ({
        message: text(
          '记录已保存；尚未执行原传输。',
          'Record saved; the original transfer has not executed.',
        ),
        evidence: r.id,
      }),
    },
  )
  return (
    <Modal
      open
      title={
        reviewMode
          ? text('独立审查内容申诉', 'Independent content appeal review')
          : text('提交内容申诉', 'Submit content appeal')
      }
      footer={null}
      onCancel={() => {
        if (!save.isPending) close()
      }}
      closable={!save.isPending}
      mask={{ closable: !save.isPending }}
    >
      <Space orientation="vertical" size={16} style={{ width: '100%' }}>
        <Typography.Text copyable>{record.id}</Typography.Text>
        <TransferState record={record} />
        {record.networkHoldId ? (
          <Alert
            type="warning"
            title={text(
              '网络审查需要独立联动权限；过期事件批准只解除本来源限制，不延长原传输许可。',
              'Network review requires independent linkage permission. Approving an expired event only releases this source restriction; transfer authorization is not extended.',
            )}
          />
        ) : null}
        {reviewMode ? (
          <Select
            aria-label={text('审查结论', 'Review resolution')}
            value={resolution}
            onChange={setResolution}
            disabled={save.blocked}
            options={[
              {
                value: 'reject',
                label: text('拒绝', 'Reject'),
                disabled: record.appealStatus === 'rejected',
              },
              {
                value: 'approve',
                label: text(
                  '批准申诉（许可仍受期限/版本约束）',
                  'Approve appeal (transfer expiry/versions still apply)',
                ),
              },
            ]}
          />
        ) : null}
        <Select
          aria-label={text('固定原因', 'Reason code')}
          value={reason}
          onChange={setReason}
          disabled={save.blocked}
          options={[
            { value: 'false_positive', label: text('误报', 'False positive') },
            { value: 'business_exception', label: text('业务例外', 'Business exception') },
            ...(reviewMode
              ? [
                  {
                    value: 'insufficient_justification',
                    label: text('依据不足', 'Insufficient justification'),
                  },
                ]
              : []),
          ]}
        />
        {save.feedback}
        <Button
          type="primary"
          disabled={
            save.blocked ||
            save.isSuccess ||
            (reviewMode && Boolean(record.networkHoldId) && !access.link)
          }
          loading={save.isPending}
          onClick={() => save.mutate()}
        >
          {text('保存决定', 'Save decision')}
        </Button>
      </Space>
    </Modal>
  )
}
