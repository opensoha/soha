import { useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { AppstoreOutlined, PlayCircleOutlined, RadarChartOutlined, RobotOutlined, ToolOutlined } from '@ant-design/icons'
import { App, Button, Card, Col, Empty, List, Row, Segmented, Space, Statistic, Table, Tag, Typography } from 'antd'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { PageHeader } from '@/components/page-header'
import { StatusTag } from '@/components/status-tag'
import { api } from '@/services/api-client'
import type { ApiResponse } from '@/types'
import { AIWorkbenchPage } from './workbench-page'
import type { WorkbenchSession } from './workbench-types'

const { Paragraph, Text } = Typography

interface Insight {
  title: string
  description: string
  severity: string
  actions?: string[]
}

interface RootCauseRun {
  id: string
  kind?: string
  title: string
  status: string
  severity: string
  summary: string
}

export function AIObserveOverviewPage() {
  const navigate = useNavigate()
  const sessionsQuery = useQuery({
    queryKey: ['ai-observe-overview-sessions'],
    queryFn: () => api.get<ApiResponse<WorkbenchSession[]>>('/copilot/sessions'),
  })
  const insightsQuery = useQuery({
    queryKey: ['ai-observe-overview-insights'],
    queryFn: () => api.get<ApiResponse<Insight[]>>('/copilot/insights'),
  })
  const runsQuery = useQuery({
    queryKey: ['ai-observe-overview-runs'],
    queryFn: () => api.get<ApiResponse<RootCauseRun[]>>('/copilot/analysis/runs'),
  })
  const inspectionRunsQuery = useQuery({
    queryKey: ['ai-observe-overview-inspection-runs'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; status: string; severity: string; summary: string }>>>('/copilot/inspection-runs'),
  })

  const sessions = sessionsQuery.data?.data ?? []
  const insights = insightsQuery.data?.data ?? []
  const runs = runsQuery.data?.data ?? []
  const inspectionRuns = inspectionRunsQuery.data?.data ?? []

  return (
    <div className="kc-page">
      <PageHeader
        title="AI观测分析中心"
        description="面向运维中后台的 AIOps 助手入口，统一调查、巡检、性能和工具能力。"
        actions={
          <Space>
            <Button icon={<ToolOutlined />} onClick={() => navigate('/ai-observe/tools')}>工具与技能</Button>
            <Button type="primary" icon={<RobotOutlined />} onClick={() => navigate('/ai-observe/workbench')}>进入调查工作台</Button>
          </Space>
        }
      />

      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card>
            <Space direction="vertical" size={12} style={{ width: '100%' }}>
              <Space align="start">
                <RobotOutlined style={{ fontSize: 24 }} />
                <div>
                  <Text strong>助手欢迎区</Text>
                  <Paragraph type="secondary" style={{ marginBottom: 0 }}>
                    当前 AI 能力已覆盖会话调查、根因分析、性能分析、链路分析与巡检复盘。
                  </Paragraph>
                </div>
              </Space>
              <Space wrap>
                <Tag color="blue">双层 AIOps 控制面</Tag>
                <Tag>Ant Design X 工作台</Tag>
                <Tag>会话级工具集装配</Tag>
              </Space>
              <Space>
                <Button onClick={() => navigate('/ai-observe/workbench?mode=root-cause')}>按根因模式开始</Button>
                <Button onClick={() => navigate('/ai-observe/workbench?mode=performance')}>按性能模式开始</Button>
                <Button onClick={() => navigate('/ai-observe/workbench?mode=trace')}>按链路模式开始</Button>
              </Space>
            </Space>
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card title="运行态概览">
            <Row gutter={[12, 12]}>
              <Col span={12}><Statistic title="调查会话" value={sessions.length} prefix={<RobotOutlined />} /></Col>
              <Col span={12}><Statistic title="根因运行" value={runs.length} prefix={<RadarChartOutlined />} /></Col>
              <Col span={12}><Statistic title="巡检运行" value={inspectionRuns.length} prefix={<AppstoreOutlined />} /></Col>
              <Col span={12}><Statistic title="AI 洞察" value={insights.length} prefix={<ToolOutlined />} /></Col>
            </Row>
            <Paragraph type="secondary" style={{ marginTop: 16, marginBottom: 0 }}>
              入口层负责快速判断当前平台是否需要立即进入调查、巡检复盘或工具配置。
            </Paragraph>
          </Card>
        </Col>

        <Col xs={24} xl={8}>
          <Card title="最近调查">
            {sessions.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无会话" /> : (
              <List
                dataSource={sessions.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item actions={[<Link key="open" to="/ai-observe/workbench">打开</Link>]}>
                    <List.Item.Meta title={item.title} description={item.metadata?.summary || item.updatedAt} />
                    {item.metadata?.mode ? <Tag>{item.metadata.mode}</Tag> : null}
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="最近分析">
            {runs.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无根因运行" /> : (
              <List
                dataSource={runs.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta title={item.title} description={item.summary} />
                    <Space direction="vertical" size={4}>
                      <StatusTag value={item.status} />
                      <StatusTag value={item.severity} />
                    </Space>
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
        <Col xs={24} xl={8}>
          <Card title="风险雷达">
            {insights.length === 0 ? <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无风险信号" /> : (
              <List
                dataSource={insights.slice(0, 5)}
                renderItem={(item) => (
                  <List.Item>
                    <List.Item.Meta title={item.title} description={item.description} />
                    <StatusTag value={item.severity} />
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export function AIOperationsPage() {
  const { message } = App.useApp()
  const queryClient = useQueryClient()
  const navigate = useNavigate()
  const [activeView, setActiveView] = useState<'tasks' | 'runs' | 'policies'>('tasks')
  const tasksQuery = useQuery({
    queryKey: ['ai-operations-tasks'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; title: string; scopeType: string; clusterId?: string; namespace?: string; checks?: string[]; enabled: boolean; intervalMinutes: number; lastRunAt?: string }>>>('/copilot/inspection-tasks'),
  })
  const runsQuery = useQuery({
    queryKey: ['ai-operations-runs'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; taskId: string; status: string; severity: string; summary: string; findings?: Array<{ id: string; title: string; severity: string }>; startedAt: string; completedAt?: string }>>>('/copilot/inspection-runs'),
  })
  const policiesQuery = useQuery({
    queryKey: ['ai-operations-policies'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; triggerType: string; analysisKinds?: string[]; analysisProfileId: string; enabled: boolean; remediationPolicy: string }>>>('/copilot/automation-policies'),
  })
  const createSessionMutation = useMutation({
    mutationFn: (runId: string) => api.post(`/copilot/inspection-runs/${runId}/session`),
    onSuccess: () => {
      void message.success('已从巡检运行创建调查会话')
      void queryClient.invalidateQueries({ queryKey: ['ai-observe-overview-sessions'] })
      navigate('/ai-observe/workbench?mode=inspection_review')
    },
    onError: (err: Error) => void message.error(err.message),
  })
  const executeMutation = useMutation({
    mutationFn: (taskId: string) => api.post(`/copilot/inspection-tasks/${taskId}/execute`),
    onSuccess: () => {
      void message.success('巡检已执行')
      void queryClient.invalidateQueries({ queryKey: ['ai-operations-runs'] })
      void queryClient.invalidateQueries({ queryKey: ['ai-operations-tasks'] })
    },
    onError: (err: Error) => void message.error(err.message),
  })

  const tasks = tasksQuery.data?.data ?? []
  const runs = runsQuery.data?.data ?? []
  const policies = policiesQuery.data?.data ?? []

  return (
    <div className="kc-page">
      <PageHeader
        title="巡检与自动化"
        description="统一查看巡检任务、巡检运行、自动化策略，并把发现结果送入调查工作台。"
        actions={
          <Space>
            <Button onClick={() => navigate('/ai-observe/workbench?mode=inspection_review')}>进入巡检复盘工作台</Button>
            <Button type="primary" onClick={() => navigate('/ai-observe/workbench')}>新建调查</Button>
          </Space>
        }
      />
      <Card styles={{ body: { paddingBottom: 8 } }}>
        <Segmented
          value={activeView}
          onChange={(value) => setActiveView(value as typeof activeView)}
          options={[
            { value: 'tasks', label: '巡检任务' },
            { value: 'runs', label: '巡检运行' },
            { value: 'policies', label: '自动化策略' },
          ]}
        />
        <Paragraph type="secondary" style={{ marginTop: 12, marginBottom: 0 }}>
          把巡检任务、巡检运行与自动化策略放在同一工作区，避免在调查和自动化之间来回跳转。
        </Paragraph>
      </Card>

      {activeView === 'tasks' ? (
        <Card title="巡检任务">
          <Table
            rowKey="id"
            dataSource={tasks}
            pagination={{ pageSize: 10 }}
            columns={[
              { title: '任务名称', dataIndex: 'title' },
              { title: '范围', dataIndex: 'scopeType', render: (_value, record) => [record.scopeType, record.clusterId, record.namespace].filter(Boolean).join(' / ') },
              { title: '检查项', dataIndex: 'checks', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
              { title: '间隔', dataIndex: 'intervalMinutes', render: (value: number) => `${value} min` },
              { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
              {
                title: '操作',
                dataIndex: 'id',
                render: (value: string) => (
                  <Button icon={<PlayCircleOutlined />} loading={executeMutation.isPending} onClick={() => executeMutation.mutate(value)}>
                    立即执行
                  </Button>
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      {activeView === 'runs' ? (
        <Card title="巡检运行记录">
          <Table
            rowKey="id"
            dataSource={runs}
            pagination={{ pageSize: 10 }}
            columns={[
              { title: '运行 ID', dataIndex: 'id' },
              { title: '任务', dataIndex: 'taskId' },
              { title: '状态', dataIndex: 'status', render: (value: string) => <StatusTag value={value} /> },
              { title: '严重度', dataIndex: 'severity', render: (value: string) => <StatusTag value={value} /> },
              { title: '发现项', dataIndex: 'findings', render: (value: Array<{ id: string }>) => value?.length ?? 0 },
              { title: '摘要', dataIndex: 'summary' },
              {
                title: '联动',
                dataIndex: 'id',
                render: (value: string) => (
                  <Button onClick={() => createSessionMutation.mutate(value)}>
                    创建调查会话
                  </Button>
                ),
              },
            ]}
          />
        </Card>
      ) : null}

      {activeView === 'policies' ? (
        <Card title="自动化策略">
          <Paragraph type="secondary">
            自动化策略只负责触发和分析范围，不应隐式替代会话级 toolset 选择。需要深入排查时，优先把结果送回调查工作台。
          </Paragraph>
          <Table
            rowKey="id"
            dataSource={policies}
            pagination={{ pageSize: 10 }}
            columns={[
              { title: '名称', dataIndex: 'name' },
              { title: '触发类型', dataIndex: 'triggerType' },
              { title: '分析类型', dataIndex: 'analysisKinds', render: (value: string[]) => <Space wrap>{(value ?? []).map((item) => <Tag key={item}>{item}</Tag>)}</Space> },
              { title: '分析模板', dataIndex: 'analysisProfileId' },
              { title: '修复策略', dataIndex: 'remediationPolicy' },
              { title: '启用', dataIndex: 'enabled', render: (value: boolean) => <StatusTag value={value ? 'enabled' : 'disabled'} /> },
            ]}
          />
        </Card>
      ) : null}
    </div>
  )
}

export function AIToolsPage() {
  const navigate = useNavigate()
  const settingsQuery = useQuery({
    queryKey: ['ai-tools-settings'],
    queryFn: () => api.get<ApiResponse<{ skillsRegistry?: Array<{ id: string; name: string; description?: string; enabled: boolean; scopes?: string[] }> }>>('/settings/ai'),
  })
  const adaptersQuery = useQuery({
    queryKey: ['ai-tools-adapters'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; description: string; sourceKind: string; category?: string; tools?: Array<{ name: string; description: string }> }>>>('/copilot/data-source-capabilities'),
  })
  const dataSourcesQuery = useQuery({
    queryKey: ['ai-tools-datasources'],
    queryFn: () => api.get<ApiResponse<Array<{ id: string; name: string; sourceKind: string; backendType: string; enabled: boolean; mcpAdapter: string; validationStatus?: string }>>>('/copilot/data-sources'),
  })

  const adapters = adaptersQuery.data?.data ?? []
  const dataSources = dataSourcesQuery.data?.data ?? []
  const skills = settingsQuery.data?.data?.skillsRegistry ?? []

  return (
    <div className="kc-page">
      <PageHeader
        title="工具与技能"
        description="全局配置镜像与会话级装配入口，统一查看 MCP adapters、数据源和技能能力。"
      />
      <Row gutter={[16, 16]}>
        <Col xs={24} xl={12}>
          <Card title="MCP Adapters">
            <List
              dataSource={adapters}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.sourceKind}</Tag></Space>}
                    description={item.description}
                  />
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col xs={24} xl={12}>
          <Card title="Data Sources">
            <List
              dataSource={dataSources}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.backendType}</Tag></Space>}
                    description={`${item.sourceKind} / ${item.mcpAdapter}`}
                  />
                  <StatusTag value={item.validationStatus || (item.enabled ? 'enabled' : 'disabled')} />
                </List.Item>
              )}
            />
          </Card>
        </Col>
        <Col xs={24}>
          <Card title="Skills Registry">
            <List
              dataSource={skills}
              locale={{ emptyText: '暂无全局 skills 配置' }}
              renderItem={(item) => (
                <List.Item>
                  <List.Item.Meta
                    title={<Space><Text strong>{item.name}</Text><Tag>{item.id}</Tag></Space>}
                    description={item.description || (item.scopes ?? []).join(', ')}
                  />
                  <StatusTag value={item.enabled ? 'enabled' : 'disabled'} />
                </List.Item>
              )}
            />
            <Space style={{ marginTop: 16 }}>
              <Button onClick={() => navigate('/settings/ai')}>前往 AI 设置</Button>
              <Button type="primary" onClick={() => navigate('/ai-observe/workbench')}>回到调查工作台</Button>
            </Space>
          </Card>
        </Col>
      </Row>
    </div>
  )
}

export { AIWorkbenchPage }
