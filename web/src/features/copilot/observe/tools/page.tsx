import { ManagementState } from '@/components/management-list'
import { ReadFeedback } from '@/components/request-feedback'
import { MetadataTag } from '@/components/status-tag'
import { useOperationMutation } from '@/components/use-operation-mutation'
import { queryAccessDenied, readableQueryData } from '@/utils/query-feedback'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Alert,
  Button,
  Card,
  Drawer,
  Flex,
  Input,
  InputNumber,
  Select,
  Space,
  Typography,
} from 'antd'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
import '../../copilot-pages.css'
import { displayWorkbenchSessionTitle } from '../../workbench/model'
import {
  TOOLSET_BUDGET_FIELDS,
  buildDisabledToolOptions,
  canonicalDisabledToolNames,
  cleanToolsetPayload,
  countObjectKeys,
  numberRecord,
  recommendedAdapterIds,
  scopeOverrideState,
} from '../../workbench/toolset'
import type { WorkbenchSessionScope } from '../../workbench/types'
import { observeKeys } from '../keys'
import { observeMutations } from '../mutations'
import { observeQueries } from '../queries'
import './styles.css'

const { Paragraph, Text, Title } = Typography

function buildScopeSummary(scope?: WorkbenchSessionScope) {
  if (!scope) return '未固定上下文'
  return (
    [scope.clusterId, scope.namespace, scope.workload || scope.service, scope.alertId]
      .filter(Boolean)
      .join(' / ') || '未固定上下文'
  )
}

export function AIToolsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const requestedSessionId = searchParams.get('session') || undefined
  const queryClient = useQueryClient()
  const [selectedAdapterIds, setSelectedAdapterIds] = useState<string[]>([])
  const [selectedSkillIds, setSelectedSkillIds] = useState<string[]>([])
  const [disabledToolNames, setDisabledToolNames] = useState<string[]>([])
  const [budgetOverrides, setBudgetOverrides] = useState<Record<string, number>>({})
  const [scopeOverrides, setScopeOverrides] = useState<Partial<WorkbenchSessionScope>>({})
  const [sessionDrawerOpen, setSessionDrawerOpen] = useState(false)
  const catalogQuery = useQuery(observeQueries.tools.catalog())
  const sessionsQuery = useQuery(observeQueries.overview.sessions())
  const sessionDetailQuery = useQuery(observeQueries.tools.session(requestedSessionId))

  const draftSessionRef = useRef<string>()
  const draftDirty = useRef(false)
  const adapters = useMemo(
    () => readableQueryData(catalogQuery)?.adapters ?? [],
    [readableQueryData(catalogQuery)?.adapters],
  )
  const dataSources = useMemo(
    () => readableQueryData(catalogQuery)?.dataSources ?? [],
    [readableQueryData(catalogQuery)?.dataSources],
  )
  const skills = useMemo(
    () => readableQueryData(catalogQuery)?.skillsRegistry ?? [],
    [readableQueryData(catalogQuery)?.skillsRegistry],
  )
  const currentSession = readableQueryData(sessionDetailQuery)
  const disabledToolOptions = useMemo(() => buildDisabledToolOptions(adapters), [adapters])
  const cleanedBudgetOverrides = useMemo(() => numberRecord(budgetOverrides), [budgetOverrides])
  const cleanedScopeOverrides = useMemo(() => scopeOverrideState(scopeOverrides), [scopeOverrides])
  const activeDataSourceAdapters = [
    ...new Set(
      dataSources
        .filter((item) => item.enabled)
        .map((item) => item.mcpAdapter)
        .filter(Boolean),
    ),
  ]
  const unavailableSelectedAdapterIds = selectedAdapterIds.filter(
    (adapterId) =>
      adapterId !== 'platform-native.v1' && !activeDataSourceAdapters.includes(adapterId),
  )

  useEffect(() => {
    if (draftSessionRef.current === currentSession?.id && draftDirty.current) return
    draftSessionRef.current = currentSession?.id
    setSelectedAdapterIds(currentSession?.metadata?.toolset?.enabledAdapterIds ?? [])
    setSelectedSkillIds(currentSession?.metadata?.toolset?.enabledSkillIds ?? [])
    setDisabledToolNames(
      canonicalDisabledToolNames(
        currentSession?.metadata?.toolset?.disabledToolNames ?? [],
        adapters,
      ),
    )
    setBudgetOverrides(numberRecord(currentSession?.metadata?.toolset?.budgetOverrides))
    setScopeOverrides(scopeOverrideState(currentSession?.metadata?.toolset?.scopeOverrides))
  }, [
    adapters,
    currentSession?.id,
    currentSession?.metadata?.toolset?.budgetOverrides,
    currentSession?.metadata?.toolset?.disabledToolNames,
    currentSession?.metadata?.toolset?.enabledAdapterIds,
    currentSession?.metadata?.toolset?.enabledSkillIds,
    currentSession?.metadata?.toolset?.scopeOverrides,
  ])

  const patchSessionMutation = useOperationMutation(
    {
      ...observeMutations.tools.patchSession(),
      onSuccess: async (_response, payload) => {
        await queryClient.invalidateQueries({ queryKey: observeKeys.tools.sessions() })
        await queryClient.invalidateQueries({
          queryKey: observeKeys.tools.session(payload.sessionId),
        })
      },
    },
    { label: ['保存会话工具装配', 'Save session toolset'], target: (v) => v.sessionId },
  )

  const blocked =
    patchSessionMutation.blocked ||
    queryAccessDenied(sessionDetailQuery) ||
    queryAccessDenied(catalogQuery)
  const denied = queryAccessDenied(sessionDetailQuery) || queryAccessDenied(patchSessionMutation)
  useEffect(() => {
    if (denied) {
      setSessionDrawerOpen(false)
      setSelectedAdapterIds([])
      setSelectedSkillIds([])
      setDisabledToolNames([])
      setBudgetOverrides({})
      setScopeOverrides({})
      draftDirty.current = false
    }
  }, [denied])
  const setBudgetOverrideValue = (key: string, value: number | string | null) => {
    draftDirty.current = true
    setBudgetOverrides((current) => {
      const next = { ...current }
      const numberValue = Number(value)
      if (Number.isFinite(numberValue) && numberValue > 0) {
        next[key] = numberValue
      } else {
        delete next[key]
      }
      return next
    })
  }

  const setScopeOverrideValue = (key: keyof WorkbenchSessionScope, value: string) => {
    draftDirty.current = true
    setScopeOverrides((current) => {
      const next = { ...current }
      const trimmed = value.trim()
      if (trimmed) {
        next[key] = trimmed as never
      } else {
        delete next[key]
      }
      return next
    })
  }

  const setScopeOverrideNumberValue = (
    key: keyof WorkbenchSessionScope,
    value: number | string | null,
  ) => {
    draftDirty.current = true
    setScopeOverrides((current) => {
      const next = { ...current }
      const numberValue = Number(value)
      if (Number.isFinite(numberValue) && numberValue > 0) {
        next[key] = numberValue as never
      } else {
        delete next[key]
      }
      return next
    })
  }

  const applyRecommendedToolset = () => {
    if (blocked) return
    draftDirty.current = true
    setSelectedAdapterIds(recommendedAdapterIds(adapters, dataSources))
    setSelectedSkillIds(skills.filter((item) => item.enabled).map((item) => item.id))
    setDisabledToolNames([])
    setBudgetOverrides({ timeoutSeconds: 60, maxEvidenceItems: 20 })
    setScopeOverrides({})
  }

  const clearToolset = () => {
    if (blocked) return
    draftDirty.current = true
    setSelectedAdapterIds([])
    setSelectedSkillIds([])
    setDisabledToolNames([])
    setBudgetOverrides({})
    setScopeOverrides({})
  }

  const saveToolset = () => {
    if (blocked || !requestedSessionId || !currentSession) return
    patchSessionMutation.mutate({
      sessionId: requestedSessionId,
      body: {
        toolset: cleanToolsetPayload({
          enabledAdapterIds: selectedAdapterIds,
          enabledSkillIds: selectedSkillIds,
          disabledToolNames: canonicalDisabledToolNames(disabledToolNames, adapters),
          budgetOverrides: cleanedBudgetOverrides,
          scopeOverrides: cleanedScopeOverrides,
        }),
      },
    })
  }

  const selectSession = (sessionId?: string) => {
    if (blocked) return
    draftDirty.current = false
    patchSessionMutation.reset()
    const next = new URLSearchParams(searchParams)
    if (sessionId) next.set('session', sessionId)
    else next.delete('session')
    setSessionDrawerOpen(false)
    setSearchParams(next)
  }

  return (
    <div className="soha-page">
      <ReadFeedback query={catalogQuery} label="工具目录" />
      <ReadFeedback query={sessionsQuery} label="会话列表" />
      {!sessionDrawerOpen && patchSessionMutation.feedback}
      <Card className="soha-session-assembly-card" size="small">
        <Flex align="center" gap={12} justify="space-between" wrap="wrap">
          <Select
            disabled={blocked}
            allowClear
            showSearch={{ optionFilterProp: 'label' }}
            loading={sessionsQuery.isLoading}
            placeholder="选择会话"
            className="soha-session-assembly-card__select"
            value={requestedSessionId}
            options={(readableQueryData(sessionsQuery) ?? []).map((session) => ({
              value: session.id,
              label: displayWorkbenchSessionTitle(session.title),
            }))}
            onChange={selectSession}
          />
          <Flex align="center" gap={8} wrap="wrap">
            {currentSession ? (
              <>
                <MetadataTag label={currentSession.metadata?.mode || 'general'} />
                <MetadataTag label={buildScopeSummary(currentSession.metadata?.scope)} />
                <MetadataTag
                  tone="blue"
                  label={`${selectedAdapterIds.length || 'auto'} adapters / ${selectedSkillIds.length || 'global'} skills`}
                />
              </>
            ) : null}
            <Button
              type="primary"
              disabled={blocked || !requestedSessionId}
              loading={sessionDetailQuery.isLoading}
              onClick={() => setSessionDrawerOpen(true)}
            >
              配置装配
            </Button>
          </Flex>
        </Flex>
      </Card>

      <Drawer
        destroyOnHidden
        open={sessionDrawerOpen}
        closable={!blocked}
        keyboard={!blocked}
        mask={{ closable: !blocked }}
        title="会话级工具装配"
        size={720}
        loading={sessionDetailQuery.isLoading}
        onClose={() => {
          if (!blocked) {
            setSessionDrawerOpen(false)
            draftDirty.current = false
          }
        }}
        footer={
          currentSession ? (
            <Flex justify="space-between" gap={12} wrap="wrap">
              <Space wrap>
                <Button disabled={blocked} onClick={clearToolset}>
                  恢复自动选择
                </Button>
                <Button disabled={blocked} onClick={applyRecommendedToolset}>
                  应用推荐预设
                </Button>
              </Space>
              <Button
                disabled={blocked}
                type="primary"
                loading={patchSessionMutation.isPending}
                onClick={saveToolset}
              >
                保存会话级装配
              </Button>
            </Flex>
          ) : null
        }
      >
        <ReadFeedback query={sessionDetailQuery} label="会话工具装配" />
        {patchSessionMutation.feedback}
        <div
          onChange={() => {
            draftDirty.current = true
          }}
        >
          {!requestedSessionId || !currentSession ? (
            <ManagementState
              bordered={false}
              compact
              kind={sessionDetailQuery.isError ? 'error' : 'select-scope'}
              title={sessionDetailQuery.isError ? '会话加载失败' : '未选择会话'}
              description={
                sessionDetailQuery.isError
                  ? '无法读取当前会话，请稍后重试。'
                  : '先从左侧菜单进入一个会话，再配置工具装配。'
              }
            />
          ) : (
            <Space orientation="vertical" size={16} style={{ width: '100%' }}>
              <Space size={[8, 8]} wrap>
                <MetadataTag
                  tone="blue"
                  label={displayWorkbenchSessionTitle(currentSession.title)}
                />
                <MetadataTag label={currentSession.metadata?.mode || 'general'} />
                <MetadataTag label={buildScopeSummary(currentSession.metadata?.scope)} />
                <MetadataTag
                  label={
                    selectedAdapterIds.length > 0
                      ? `${selectedAdapterIds.length} adapters`
                      : 'auto adapters'
                  }
                />
                <MetadataTag label={`${disabledToolNames.length} disabled tools`} />
                <MetadataTag label={`${countObjectKeys(cleanedBudgetOverrides)} budgets`} />
              </Space>

              {unavailableSelectedAdapterIds.length > 0 ? (
                <Alert
                  type="warning"
                  showIcon
                  title="部分已选 adapter 当前没有启用数据源"
                  description={`${unavailableSelectedAdapterIds.join(', ')} 会保留在会话策略中，但运行时相关工具可能被跳过。`}
                />
              ) : null}

              <section>
                <Title level={5}>Adapters 与工具</Title>
                <Space orientation="vertical" size={12} style={{ width: '100%' }}>
                  <Select
                    disabled={blocked}
                    mode="multiple"
                    allowClear
                    maxTagCount="responsive"
                    showSearch={{ optionFilterProp: 'label' }}
                    placeholder="留空表示自动允许所有已注册 adapter"
                    style={{ width: '100%' }}
                    value={selectedAdapterIds}
                    onChange={(value: string[]) => {
                      draftDirty.current = true
                      setSelectedAdapterIds(value)
                    }}
                    options={adapters.map((item) => ({
                      value: item.id,
                      label: `${item.name} (${item.sourceKind})`,
                    }))}
                  />
                  <Select
                    disabled={blocked}
                    mode="multiple"
                    allowClear
                    maxTagCount="responsive"
                    showSearch={{ optionFilterProp: 'label' }}
                    placeholder="选择要屏蔽的工具，保存为 adapter.tool"
                    style={{ width: '100%' }}
                    value={disabledToolNames}
                    onChange={(value: string[]) => (
                      (draftDirty.current = true),
                      setDisabledToolNames(canonicalDisabledToolNames(value, adapters))
                    )}
                    options={disabledToolOptions}
                  />
                  <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                    禁用工具会以 `adapter.tool` 形式保存，避免同名工具跨 adapter 被误屏蔽。
                  </Paragraph>
                </Space>
              </section>

              <section>
                <Title level={5}>Skills</Title>
                <Select
                  disabled={blocked}
                  mode="multiple"
                  allowClear
                  maxTagCount="responsive"
                  showSearch={{ optionFilterProp: 'label' }}
                  placeholder="选择会话级技能；留空表示沿用全局启用项"
                  style={{ width: '100%' }}
                  value={selectedSkillIds}
                  onChange={(value: string[]) => {
                    draftDirty.current = true
                    setSelectedSkillIds(value)
                  }}
                  options={skills
                    .filter((item) => item.enabled)
                    .map((item) => ({ value: item.id, label: item.name }))}
                />
              </section>

              <section>
                <Title level={5}>Budget Overrides</Title>
                <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                  {TOOLSET_BUDGET_FIELDS.map((field) => (
                    <Flex
                      key={field.key}
                      justify="space-between"
                      align="center"
                      gap={12}
                      wrap="wrap"
                    >
                      <span style={{ flex: '1 1 220px' }}>
                        <Text strong>{field.label}</Text>
                        <Text type="secondary" style={{ display: 'block' }}>
                          {field.description}
                        </Text>
                      </span>
                      <InputNumber
                        disabled={blocked}
                        min={0}
                        suffix={field.suffix}
                        style={{ width: 180, maxWidth: '100%' }}
                        value={budgetOverrides[field.key]}
                        onChange={(value) => setBudgetOverrideValue(field.key, value)}
                      />
                    </Flex>
                  ))}
                </Space>
              </section>

              <section>
                <Title level={5}>Scope Overrides</Title>
                <Space orientation="vertical" size={8} style={{ width: '100%' }}>
                  <Alert
                    type="info"
                    showIcon
                    title="Scope override 会叠加到当前会话范围"
                    description={`当前会话范围：${buildScopeSummary(currentSession.metadata?.scope)}`}
                  />
                  <Input
                    disabled={blocked}
                    placeholder="Override cluster"
                    value={scopeOverrides.clusterId || ''}
                    onChange={(event) => setScopeOverrideValue('clusterId', event.target.value)}
                  />
                  <Input
                    disabled={blocked}
                    placeholder="Override namespace"
                    value={scopeOverrides.namespace || ''}
                    onChange={(event) => setScopeOverrideValue('namespace', event.target.value)}
                  />
                  <Input
                    disabled={blocked}
                    placeholder="Override workload"
                    value={scopeOverrides.workload || ''}
                    onChange={(event) => setScopeOverrideValue('workload', event.target.value)}
                  />
                  <Input
                    disabled={blocked}
                    placeholder="Override service"
                    value={scopeOverrides.service || ''}
                    onChange={(event) => setScopeOverrideValue('service', event.target.value)}
                  />
                  <Input
                    disabled={blocked}
                    placeholder="Override alert ID"
                    value={scopeOverrides.alertId || ''}
                    onChange={(event) => setScopeOverrideValue('alertId', event.target.value)}
                  />
                  <InputNumber
                    disabled={blocked}
                    min={0}
                    suffix="minutes"
                    placeholder="Override time range"
                    style={{ width: '100%' }}
                    value={scopeOverrides.timeRangeMinutes}
                    onChange={(value) => setScopeOverrideNumberValue('timeRangeMinutes', value)}
                  />
                </Space>
              </section>
            </Space>
          )}
        </div>
      </Drawer>
    </div>
  )
}
