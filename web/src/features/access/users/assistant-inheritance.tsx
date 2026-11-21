import { useState } from 'react'
import { App, Button, Form, Modal, Radio, Select, Table } from 'antd'
import { useInfiniteQuery, useMutation } from '@tanstack/react-query'
import type { Key } from 'react'
import type {
  AssistantMeshInheritanceAsset,
  AssistantMeshInheritanceInput,
  AssistantMeshInheritancePageEnvelope,
  AssistantMeshInheritanceResultEnvelope,
  AssistantMeshInheritanceRetirement,
  AssistantMeshInheritanceRetirementEnvelope,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { ContextFeedback } from '@/components/context-feedback'
import { MutationFeedback, ReadFeedback } from '@/components/request-feedback'
import { api } from '@/services/api-client'
import { readableQueryData, writeOutcomeUnknown } from '@/utils/query-feedback'
import type { AccessUser } from '../shared/types'
import { getUserLabel } from '../shared/utils'

const path = '/copilot/assistant-mesh/governance/inheritance'
const columns = [
  { title: '已发布资产', dataIndex: 'title' },
  {
    title: '类型',
    dataIndex: 'kind',
    render: (kind: AssistantMeshInheritanceAsset['kind']) =>
      kind === 'skill' ? 'Skill' : '经验卡',
  },
  { title: '版本', dataIndex: 'version', render: (version: number) => `v${version}` },
]

export function AssistantInheritance({
  owner,
  users,
  session,
  onClose,
}: {
  owner: AccessUser
  users: AccessUser[]
  session: number
  onClose: () => void
}) {
  const { message } = App.useApp()
  const [heirId, setHeirId] = useState<string>()
  const [action, setAction] = useState<'inherit' | 'retire'>('inherit')
  const retiring = action === 'retire'
  const [mode, setMode] = useState<'selected' | 'all'>('selected')
  const [assetIds, setAssetIds] = useState<Key[]>([])
  const list = useInfiniteQuery({
    queryKey: ['heya-inheritance', session, owner.id],
    gcTime: 0,
    initialPageParam: '',
    queryFn: async ({ pageParam, signal }) => {
      const params = new URLSearchParams({ ownerId: owner.id })
      if (pageParam) params.set('after', pageParam)
      const { data } = await api.get<AssistantMeshInheritancePageEnvelope>(`${path}?${params}`, {
        signal,
      })
      return data
    },
    getNextPageParam: (page) => page.nextAfter || undefined,
  })
  const assets = readableQueryData(list)?.pages.flatMap((page) => page.items) ?? []
  const heirs = users
    .filter((user) => user.status === 'active' && user.id !== owner.id)
    .map((user) => ({ label: getUserLabel(user), value: user.id }))
  const transfer = useMutation({
    mutationFn: async (
      input: AssistantMeshInheritanceInput | AssistantMeshInheritanceRetirement,
    ) => {
      if ('heirId' in input) {
        const { data } = await api.post<AssistantMeshInheritanceResultEnvelope>(path, input)
        return `已继承 ${data.transferred} 项资产`
      }
      const { data } = await api.post<AssistantMeshInheritanceRetirementEnvelope>(
        `${path}/retire`,
        input,
      )
      return `已下架 ${data.retired} 项资产`
    },
    onSuccess: (receipt) => {
      message.success(receipt)
      onClose()
    },
    onError: () => void list.refetch(),
  })
  const canTransfer =
    list.isSuccess &&
    !list.isFetching &&
    !transfer.isPending &&
    !writeOutcomeUnknown(transfer.error) &&
    (retiring || heirs.some(({ value }) => value === heirId)) &&
    assets.length > 0 &&
    (mode === 'all' ||
      (assetIds.length > 0 &&
        assetIds.length <= 100 &&
        assetIds.every((id) => assets.some((asset) => asset.id === id))))

  return (
    <Modal
      open
      width={680}
      title={`处置 ${getUserLabel(owner)} 的 Heya 资产`}
      onCancel={onClose}
      okText={`确认${retiring ? '下架' : '继承'}${mode === 'all' ? '全部' : '所选'}`}
      confirmLoading={transfer.isPending}
      okButtonProps={{ disabled: !canTransfer, danger: retiring }}
      cancelButtonProps={{ disabled: transfer.isPending }}
      closable={!transfer.isPending}
      mask={{ closable: !transfer.isPending }}
      keyboard={!transfer.isPending}
      onOk={() => {
        if (!canTransfer) return
        const selection = {
          ownerId: owner.id,
          ...(mode === 'selected' ? { assetIds: assetIds.map(String) } : {}),
        }
        if (retiring) transfer.mutate(selection)
        else if (heirId) transfer.mutate({ ...selection, heirId })
      }}
    >
      <ReadFeedback label="待继承的 Heya 资产" query={list} />
      <MutationFeedback
        operations={[{ mutation: transfer, label: '处置 Heya 资产', target: getUserLabel(owner) }]}
      />
      {writeOutcomeUnknown(transfer.error) ? (
        <ContextFeedback type="warning" title="请核对剩余资产和审计记录后，再提交新的处置请求。" />
      ) : null}
      <Form layout="vertical" disabled={transfer.isPending}>
        <Form.Item label="处置方式">
          <Radio.Group
            value={action}
            onChange={(event) => setAction(event.target.value)}
            options={[
              { value: 'inherit', label: '指定继承人' },
              { value: 'retire', label: '直接下架' },
            ]}
          />
        </Form.Item>
        {!retiring ? (
          <Form.Item label="继承人" htmlFor="heya-successor">
            <Select
              id="heya-successor"
              value={heirId}
              options={heirs}
              onChange={setHeirId}
              showSearch={{ optionFilterProp: 'label' }}
              placeholder="选择启用的员工"
            />
          </Form.Item>
        ) : null}
        <Form.Item
          label="资产范围"
          extra={
            retiring
              ? '下架后停止共享，原发布版本进入归档。逐项最多选择 100 个。'
              : '原发布版本、受众和版本历史随资产转移。逐项最多选择 100 个。'
          }
        >
          <Radio.Group
            value={mode}
            onChange={(event) => setMode(event.target.value)}
            options={[
              { value: 'selected', label: '仅选中的资产' },
              { value: 'all', label: '该员工的全部已发布 Skill 和经验卡' },
            ]}
          />
        </Form.Item>
      </Form>
      <Table<AssistantMeshInheritanceAsset>
        size="small"
        rowKey="id"
        columns={columns}
        dataSource={assets}
        pagination={false}
        scroll={{ y: 300 }}
        loading={list.isPending}
        locale={{ emptyText: '暂无可继承的已发布资产' }}
        rowSelection={
          mode === 'selected'
            ? {
                selectedRowKeys: assetIds,
                onChange: setAssetIds,
                getCheckboxProps: () => ({ disabled: transfer.isPending }),
              }
            : undefined
        }
      />
      {list.hasNextPage ? (
        <Button
          type="link"
          disabled={transfer.isPending}
          loading={list.isFetchingNextPage}
          onClick={() => void list.fetchNextPage()}
        >
          加载更多资产
        </Button>
      ) : null}
    </Modal>
  )
}
