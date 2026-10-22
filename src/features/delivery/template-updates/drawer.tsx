import { useMemo, useState, type Key } from 'react'
import {
  Alert,
  Button,
  Checkbox,
  Drawer,
  Input,
  InputNumber,
  Select,
  Space,
  Steps,
  Switch,
  Typography,
} from 'antd'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import type {
  TemplateParameterValues,
  TemplateUpdateCandidate,
  TemplateUpdateMode,
  TemplateUpdatePolicy,
  TemplateUpdatePreview,
  TemplateUpdateSelection,
  TemplateUpdateTarget,
} from '@opensoha/contracts/gen/ts/sohaapi'
import { AdminTable } from '@/components/admin-table'
import { ReadFeedback } from '@/components/request-feedback'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { StatusTag } from '@/components/status-tag'
import { hasPermission, usePermissionSnapshot } from '@/features/auth'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { TemplateAdoptionDiff } from './diff'
import type { TemplateUpdateScope } from './api'
import { templateUpdateMutations, templateUpdateQueries } from './queries'
import './styles.css'

const policyLabels: Record<TemplateUpdateMode, string> = {
  fixed: '固定版本',
  manual: '手动采用',
  automatic: '自动更新',
  approval: '审批后采用',
}

function AdoptionPolicy({
  target,
  policy,
  canWrite,
  readFailed,
}: {
  target: TemplateUpdateTarget
  policy?: TemplateUpdatePolicy
  canWrite: boolean
  readFailed: boolean
}) {
  const client = useQueryClient()
  const [mode, setMode] = useState<TemplateUpdateMode>(policy?.mode ?? 'fixed')
  const [paused, setPaused] = useState(policy?.paused ?? false)
  const save = useOperationMutation(templateUpdateMutations.policy(client), {
    label: ['保存采用策略', 'Save adoption policy'],
    target: () => `${target.applicationName} / ${target.environmentName || '应用全部环境'}`,
    receipt: () => ({ message: '采用策略已保存，生产环境继续保留审批。' }),
  })
  return (
    <Space orientation="vertical" size={6}>
      <div className="soha-template-adoption__policy">
        <Select
          className="soha-template-adoption__policy-select"
          aria-label={`${target.serviceName || target.applicationName}采用策略`}
          value={mode}
          onChange={setMode}
          disabled={!canWrite || save.blocked || readFailed || !target.eligible}
          options={Object.entries(policyLabels).map(([value, label]) => ({ value, label }))}
        />
        <Switch
          aria-label="暂停自动采用"
          checked={paused}
          onChange={setPaused}
          disabled={!canWrite || save.blocked || readFailed}
          checkedChildren="已暂停"
          unCheckedChildren="启用"
        />
        <Button
          size="small"
          disabled={!canWrite || save.blocked || readFailed || !target.eligible}
          loading={save.isPending}
          onClick={() =>
            save.mutate({
              kind: target.kind,
              templateId: target.templateId,
              targetId: target.id,
              mode,
              paused,
              expectedVersion: policy?.version ?? 0,
            })
          }
        >
          保存策略
        </Button>
      </div>
      {policy?.lastBatchId ? (
        <Link to={`/delivery/batches/${encodeURIComponent(policy.lastBatchId)}`}>
          最近自动采用批次
        </Link>
      ) : null}
      {policy?.lastError ? (
        <Typography.Text type="danger">{policy.lastError}</Typography.Text>
      ) : null}
      {save.feedback}
    </Space>
  )
}

interface Edit {
  text: string
  defaults: string[]
}
export function adoptionSelection(
  candidate: TemplateUpdateCandidate,
  edit?: Edit,
): TemplateUpdateSelection {
  if (!edit) return { targetId: candidate.target.id, wave: candidate.wave }
  const parsed: unknown = JSON.parse(edit.text)
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed))
    throw new Error('变量必须是 JSON 对象')
  const parameters = { ...parsed } as TemplateParameterValues
  for (const key of edit.defaults) delete parameters[key]
  return {
    targetId: candidate.target.id,
    parameters,
    useNewDefaults: edit.defaults,
    wave: candidate.wave,
  }
}

export function TemplateAdoptionDrawer({
  scope,
  serviceId,
  environmentId,
  initialVersion,
  initialTargetIds,
  onClose,
}: {
  scope: TemplateUpdateScope
  serviceId?: string
  environmentId?: string
  initialVersion?: number
  initialTargetIds?: string[]
  onClose: () => void
}) {
  const client = useQueryClient()
  const permission = usePermissionSnapshot()
  const canWrite = hasPermission(permission.data?.data, 'delivery.application-environments.update')
  const canExecute = canWrite && hasPermission(permission.data?.data, 'delivery.workflows.trigger')
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(initialTargetIds?.length ? 100 : 20)
  const [selected, setSelected] = useState<Key[]>(initialTargetIds ?? [])
  const [version, setVersion] = useState(initialVersion)
  const [activeCandidate, setActiveCandidate] = useState<string>()
  const [result, setResult] = useState<TemplateUpdatePreview>()
  const [edits, setEdits] = useState<Record<string, Edit>>({})
  const [editError, setEditError] = useState<string>()
  const [waveSize, setWaveSize] = useState(2)
  const [stopOnFailure, setStopOnFailure] = useState(true)
  const [acknowledged, setAcknowledged] = useState(false)
  const [acceptedIds, setAcceptedIds] = useState<Key[]>([])
  const [batchKey] = useState(() => crypto.randomUUID())
  const queryScope = { ...scope, serviceId, applicationEnvironmentId: environmentId }
  const targetsQuery = useQuery(templateUpdateQueries.targets(queryScope, page, pageSize))
  const policiesQuery = useQuery(templateUpdateQueries.policies(scope))
  const versionsQuery = useQuery(templateUpdateQueries.versions(scope))
  const preview = useOperationMutation(templateUpdateMutations.preview(client), {
    label: ['比较模板更新', 'Compare template updates'],
    target: () => scope.templateId,
    readOnly: true,
    receipt: () => ({ message: '配置预览已冻结，可核对最终版本和逐版本差异。' }),
  })
  const batch = useOperationMutation(templateUpdateMutations.batch(client), {
    label: ['分批采用模板', 'Adopt template in waves'],
    target: () => scope.templateId,
    receipt: (result) => ({
      message: '采用批次已受理，请查看执行和审批进度。',
      evidence: (
        <Link to={`/delivery/batches/${encodeURIComponent(result.id)}`}>查看批次 {result.id}</Link>
      ),
    }),
  })
  const targetPage = readableQueryData(targetsQuery)
  const policies = readableQueryData(policiesQuery)
  const versions = readableQueryData(versionsQuery) ?? []
  const currentVersion =
    version ?? Math.max(0, ...versions.map((item) => item.publishedVersion ?? 0))
  const candidate =
    result?.candidates.find((item) => item.id === activeCandidate) ?? result?.candidates[0]
  const selectedCandidates =
    result?.candidates.filter((item) => acceptedIds.includes(item.id)) ?? []
  const historyGap = selectedCandidates.some((item) => item.cumulative.historyGap)
  const visibleTargets = useMemo(
    () =>
      (targetPage?.items ?? []).filter(
        (target) =>
          (!serviceId || target.serviceId === serviceId) &&
          (!environmentId || target.applicationEnvironmentId === environmentId),
      ),
    [targetPage, serviceId, environmentId],
  )
  const retrySelection = Boolean(initialTargetIds?.length)
  const targetSelection = retrySelection
    ? selected
    : selected.filter((id) => visibleTargets.some((item) => item.id === id))
  const resetComparison = () => {
    preview.reset()
    setResult(undefined)
    setAcceptedIds([])
    setActiveCandidate(undefined)
    setEdits({})
    setEditError(undefined)
    setAcknowledged(false)
  }
  const compare = async (recompare = false) => {
    setEditError(undefined)
    const acceptedTargets = recompare
      ? new Set(selectedCandidates.map((item) => item.target.id))
      : undefined
    let targets: TemplateUpdateSelection[]
    try {
      targets =
        recompare && result
          ? result.candidates.map((item) => adoptionSelection(item, edits[item.target.id]))
          : targetSelection.map((targetId) => ({ targetId: String(targetId) }))
    } catch (error) {
      setEditError(error instanceof Error ? error.message : '变量配置无效')
      return
    }
    try {
      const response = await preview.mutateAsync({
        kind: scope.kind,
        templateId: scope.templateId,
        applicationId: scope.applicationId,
        toVersion: currentVersion,
        targets,
      })
      setResult(response)
      setAcceptedIds(
        response.candidates
          .filter(
            (item) => item.eligible && (!acceptedTargets || acceptedTargets.has(item.target.id)),
          )
          .map((item) => item.id),
      )
      setActiveCandidate(response.candidates[0]?.id)
      setEdits({})
      setAcknowledged(false)
    } catch (error) {
      if (queryAccessDenied({ error: error instanceof Error ? error : null })) {
        setResult(undefined)
        setAcceptedIds([])
        setEdits({})
      }
      /* The persistent mutation feedback owns API failures. */
    }
  }
  const adopt = () => {
    if (
      !result ||
      !canExecute ||
      preview.blocked ||
      preview.isError ||
      !selectedCandidates.length ||
      (historyGap && !acknowledged) ||
      Object.keys(edits).length
    )
      return
    batch.mutate({
      previewId: result.id,
      input: {
        previewDigest: result.digest,
        candidateIds: selectedCandidates.map((item) => item.id),
        waveSize,
        stopOnFailure,
        acknowledgeHistoryGaps: acknowledged,
        idempotencyKey: batchKey,
      },
    })
  }
  return (
    <Drawer
      open
      title={`模板采用 · ${scope.templateId}`}
      size="min(1180px, 100vw)"
      onClose={onClose}
      className="soha-template-adoption"
      footer={
        <div className="soha-template-adoption__footer">
          <Space wrap>
            <Button onClick={onClose}>关闭</Button>
            {result && !batch.isSuccess ? (
              <Button disabled={preview.blocked || batch.blocked} onClick={resetComparison}>
                重新选择应用
              </Button>
            ) : null}
            <Link to="/delivery/batches">核对交付批次</Link>
          </Space>
          <Space wrap>
            {!result ? (
              <Button
                type="primary"
                disabled={
                  !targetSelection.length ||
                  !currentVersion ||
                  targetsQuery.isError ||
                  versionsQuery.isError ||
                  preview.blocked
                }
                loading={preview.isPending}
                onClick={() => void compare()}
              >
                比较所选 {targetSelection.length} 项
              </Button>
            ) : !batch.isSuccess ? (
              <>
                <Button
                  aria-label="重新生成差异"
                  disabled={preview.blocked || batch.blocked}
                  loading={preview.isPending}
                  onClick={() => void compare(true)}
                >
                  重新生成差异
                </Button>
                <Button
                  type="primary"
                  disabled={
                    !canExecute ||
                    preview.blocked ||
                    preview.isError ||
                    batch.blocked ||
                    !selectedCandidates.length ||
                    (historyGap && !acknowledged) ||
                    Boolean(Object.keys(edits).length)
                  }
                  loading={batch.isPending}
                  onClick={adopt}
                >
                  分批采用 {selectedCandidates.length} 项配置
                </Button>
              </>
            ) : null}
          </Space>
        </div>
      }
    >
      <Space orientation="vertical" size={20} className="soha-template-adoption__full">
        <Steps
          current={batch.isSuccess ? 2 : result ? 1 : 0}
          items={[
            { title: '受影响应用' },
            { title: '变量与版本差异' },
            { title: '分批采用与审批' },
          ]}
        />
        <Alert
          type="info"
          showIcon
          title="采用配置后，再选择是否部署"
          description="固定版本也可手动采用；自动更新需要显式订阅。独立副本和独立文件覆盖不会被传播覆盖。构建模板影响本应用后续构建，部署模板按环境采用；历史执行记录保持原快照。"
        />
        {preview.feedback}
        {batch.feedback}
        {editError ? (
          <Alert type="error" showIcon title="请修正变量配置" description={editError} />
        ) : null}
        {!result ? (
          <>
            <div className="soha-template-adoption__controls">
              <label className="soha-template-adoption__field">
                目标发布版本
                <Select
                  aria-label="目标发布版本"
                  disabled={preview.blocked}
                  value={currentVersion || undefined}
                  onChange={(next) => {
                    setVersion(next)
                    resetComparison()
                  }}
                  loading={versionsQuery.isFetching}
                  options={versions.map((item) => ({
                    value: item.publishedVersion,
                    label: `v${item.publishedVersion} · ${item.name}`,
                  }))}
                />
              </label>
              <Typography.Text type="secondary">
                {retrySelection
                  ? '已保留全部未完成目标，含其他页；提交前重新校验。'
                  : '仅选择当前页。'}
                可见范围共 {targetPage?.total ?? '—'} 项，当前范围显示 {visibleTargets.length} 项。
              </Typography.Text>
            </div>
            <ReadFeedback query={versionsQuery} label="模板版本" />
            <ReadFeedback query={policiesQuery} label="采用策略" />
            <AdminTable
              rowKey="id"
              dataSource={visibleTargets}
              loading={targetsQuery.isLoading}
              error={targetsQuery.error}
              hasReadData={Boolean(targetPage)}
              onRetry={() => void targetsQuery.refetch()}
              onRefresh={() => void targetsQuery.refetch()}
              refreshing={targetsQuery.isFetching}
              enableColumnSelection={false}
              localSorting={false}
              selectCurrentPageOnly
              currentPageSelectionLabel="选择当前页应用绑定"
              rowSelection={{
                selectedRowKeys: targetSelection,
                preserveSelectedRowKeys: retrySelection,
                onChange: (keys: Key[]) =>
                  setSelected((previous) =>
                    retrySelection
                      ? [
                          ...previous.filter(
                            (id) => !visibleTargets.some((item) => item.id === id),
                          ),
                          ...keys.filter((id) => visibleTargets.some((item) => item.id === id)),
                        ]
                      : keys,
                  ),
                getCheckboxProps: (row: TemplateUpdateTarget) => ({
                  disabled: !row.eligible || preview.blocked,
                }),
              }}
              pagination={{
                disabled: preview.blocked,
                current: page,
                pageSize,
                total: targetPage?.total ?? 0,
                onChange: (next: number, size: number) => {
                  setPage(next)
                  setPageSize(size)
                  if (!retrySelection) setSelected([])
                },
              }}
              columns={[
                {
                  title: '应用 / 服务',
                  key: 'application',
                  width: 220,
                  render: (_: unknown, target: TemplateUpdateTarget) => (
                    <Space orientation="vertical" size={2}>
                      <Typography.Text strong>{target.applicationName}</Typography.Text>
                      <Typography.Text>{target.serviceName || '环境发布流程'}</Typography.Text>
                      <Typography.Text type="secondary">
                        {target.scope === 'application'
                          ? '本应用全部环境'
                          : target.environmentName || target.applicationEnvironmentId}
                      </Typography.Text>
                    </Space>
                  ),
                },
                {
                  title: '当前版本与保护',
                  key: 'version',
                  width: 240,
                  render: (_: unknown, target: TemplateUpdateTarget) => (
                    <Space orientation="vertical" size={4}>
                      <Typography.Text>v{target.fromVersion}</Typography.Text>
                      <StatusTag
                        value={
                          target.requiresApproval
                            ? 'waiting_approval'
                            : target.eligible
                              ? 'ready'
                              : 'warning'
                        }
                        label={
                          target.requiresApproval
                            ? '需审批'
                            : target.eligible
                              ? '可手动采用'
                              : '已保护'
                        }
                      />
                      {target.reason ? (
                        <Typography.Text type="secondary">{target.reason}</Typography.Text>
                      ) : null}
                      {target.independentOverrides.length ? (
                        <Typography.Text type="secondary">
                          保留覆盖：{target.independentOverrides.join('、')}
                        </Typography.Text>
                      ) : null}
                    </Space>
                  ),
                },
                {
                  title: '后续更新策略',
                  key: 'policy',
                  width: 350,
                  render: (_: unknown, target: TemplateUpdateTarget) => {
                    const policy = policies?.find((item) => item.targetId === target.id)
                    return (
                      <AdoptionPolicy
                        key={`${target.id}:${policy?.version ?? 0}`}
                        target={target}
                        policy={policy}
                        canWrite={canWrite && !preview.blocked}
                        readFailed={policiesQuery.isError}
                      />
                    )
                  },
                },
              ]}
            />
          </>
        ) : (
          <>
            <AdminTable
              rowKey="id"
              dataSource={result.candidates}
              pagination={false}
              enableColumnSelection={false}
              rowSelection={{
                selectedRowKeys: acceptedIds,
                onChange: (ids: Key[]) => setAcceptedIds(ids),
                getCheckboxProps: (row: TemplateUpdateCandidate) => ({
                  disabled: !row.eligible || preview.blocked || batch.blocked || batch.isSuccess,
                }),
              }}
              columns={[
                {
                  title: '应用 / 环境 / 服务',
                  key: 'target',
                  render: (_: unknown, item: TemplateUpdateCandidate) => (
                    <Button type="link" onClick={() => setActiveCandidate(item.id)}>
                      {item.target.applicationName} / {item.target.environmentName || '全部环境'} /{' '}
                      {item.target.serviceName || '发布流程'}
                    </Button>
                  ),
                },
                {
                  title: '采用版本',
                  key: 'version',
                  render: (_: unknown, item: TemplateUpdateCandidate) =>
                    `v${item.target.fromVersion} → v${item.toVersion}`,
                },
                {
                  title: '校验',
                  key: 'eligible',
                  render: (_: unknown, item: TemplateUpdateCandidate) => (
                    <Space orientation="vertical" size={4}>
                      <StatusTag
                        value={
                          !item.eligible
                            ? 'warning'
                            : item.requiresApproval
                              ? 'waiting_approval'
                              : 'ready'
                        }
                        label={
                          !item.eligible
                            ? '已排除'
                            : item.requiresApproval
                              ? '待审批后采用'
                              : '可采用'
                        }
                      />
                      {item.reason || null}
                    </Space>
                  ),
                },
              ]}
            />
            {candidate ? (
              <>
                <Typography.Title level={5}>
                  {candidate.target.applicationName} · {candidate.target.serviceName || '发布流程'}{' '}
                  · {candidate.target.environmentName || '应用全部环境'}
                </Typography.Title>
                <TemplateAdoptionDiff key={candidate.id} candidate={candidate} />
                {scope.kind !== 'workflow' && !batch.isSuccess ? (
                  <details>
                    <summary>调整此项变量（保留现有覆盖）</summary>
                    <Space
                      orientation="vertical"
                      size={12}
                      className="soha-template-adoption__full"
                    >
                      <label className="soha-template-adoption__field">
                        显式采用新默认值
                        <Select
                          mode="multiple"
                          aria-label="采用新默认值的字段"
                          disabled={preview.blocked || batch.blocked}
                          value={edits[candidate.target.id]?.defaults ?? []}
                          onChange={(defaults: string[]) =>
                            setEdits((old) => ({
                              ...old,
                              [candidate.target.id]: {
                                text:
                                  old[candidate.target.id]?.text ??
                                  JSON.stringify(candidate.parameters, null, 2),
                                defaults,
                              },
                            }))
                          }
                          options={Object.keys(candidate.parameters).map((key) => ({
                            label: key,
                            value: key,
                          }))}
                        />
                      </label>
                      <label className="soha-template-adoption__field">
                        服务变量 JSON
                        <Input.TextArea
                          aria-label="采用版本变量"
                          disabled={preview.blocked || batch.blocked}
                          rows={6}
                          value={
                            edits[candidate.target.id]?.text ??
                            JSON.stringify(candidate.parameters, null, 2)
                          }
                          onChange={(event) =>
                            setEdits((old) => ({
                              ...old,
                              [candidate.target.id]: {
                                defaults: old[candidate.target.id]?.defaults ?? [],
                                text: event.target.value,
                              },
                            }))
                          }
                        />
                      </label>
                      <Typography.Text type="secondary">
                        环境独立覆盖继续保留。修改后请重新生成差异再提交。
                      </Typography.Text>
                    </Space>
                  </details>
                ) : null}
              </>
            ) : null}
            {!batch.isSuccess ? (
              <div className="soha-template-adoption__controls">
                <label className="soha-template-adoption__field">
                  每批采用数量
                  <InputNumber
                    aria-label="每批采用数量"
                    min={1}
                    max={100}
                    value={waveSize}
                    onChange={(value) => setWaveSize(value ?? 1)}
                  />
                </label>
                <Checkbox
                  checked={stopOnFailure}
                  onChange={(event) => setStopOnFailure(event.target.checked)}
                >
                  遇到失败停止后续批次
                </Checkbox>
                <Typography.Text type="secondary">
                  {Math.ceil(selectedCandidates.length / waveSize)} 批 · 生产按计划逐项审批 ·
                  不执行中间版本
                </Typography.Text>
              </div>
            ) : null}
            {historyGap ? (
              <Checkbox
                checked={acknowledged}
                onChange={(event) => setAcknowledged(event.target.checked)}
              >
                已核对中间历史缺口，仅采用校验通过的最终版本
              </Checkbox>
            ) : null}
            {!canExecute ? (
              <Alert
                type="warning"
                title="缺少采用权限"
                description="提交采用批次需要工作流执行和应用环境配置更新权限，服务端会逐项重新校验当前权限。"
              />
            ) : null}
            {batch.isSuccess ? (
              <Alert
                type="success"
                showIcon
                title="批次已创建"
                description="请在现有交付批次中核对审批、失败、跳过与配置采用结果。配置采用成功后，可另行创建部署 Plan 发布应用。"
                action={
                  <Link to={`/delivery/batches/${encodeURIComponent(batch.data.id)}`}>
                    查看进度
                  </Link>
                }
              />
            ) : null}
          </>
        )}
      </Space>
    </Drawer>
  )
}
