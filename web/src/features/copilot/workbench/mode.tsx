import {
  ApiOutlined,
  BranchesOutlined,
  EyeOutlined,
  PlayCircleOutlined,
  RadarChartOutlined,
  RobotOutlined,
  ThunderboltOutlined,
  ToolOutlined,
} from '@ant-design/icons'
import type { WorkbenchMode } from './types'

export const WORKBENCH_MODE_OPTIONS = [
  { value: 'general', label: '通用聊天' },
  { value: 'root_cause', label: '根因分析' },
  { value: 'performance', label: '性能分析' },
  { value: 'trace', label: '链路分析' },
  { value: 'inspection_review', label: '巡检复盘' },
] as const

export const RUNNABLE_ANALYSIS_MODE_OPTIONS = WORKBENCH_MODE_OPTIONS.filter(
  (item) =>
    item.value === 'root_cause' ||
    item.value === 'performance' ||
    item.value === 'trace' ||
    item.value === 'inspection_review',
)

export function modeLabel(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return '根因分析'
    case 'performance':
      return '性能分析'
    case 'trace':
      return '链路分析'
    case 'inspection_review':
      return '巡检复盘'
    default:
      return '通用聊天'
  }
}

export function modeDescription(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return '围绕告警、变更和异常证据收敛根因。'
    case 'performance':
      return '聚焦延迟、容量和抖动问题，沉淀优化建议。'
    case 'trace':
      return '从入口请求向下游链路展开，定位热点 span。'
    case 'inspection_review':
      return '把巡检发现整理成后续动作和交接结论。'
    default:
      return '用于日常问答、资料整理和任务协作。'
  }
}

export function defaultAnalysisQuestion(mode: WorkbenchMode) {
  switch (mode) {
    case 'root_cause':
      return '请基于当前会话范围执行一次根因分析，输出证据、假设、影响面和下一步动作。'
    case 'performance':
      return '请分析当前会话范围内的性能波动、容量风险和优化建议。'
    case 'trace':
      return '请围绕当前会话范围定位关键链路、热点 span 和可能的下游阻塞点。'
    case 'inspection_review':
      return '请复盘当前巡检发现，整理风险、证据和后续自动化动作。'
    default:
      return '请把当前会话上下文转成结构化分析，输出证据、结论和下一步建议。'
  }
}

export function modeIcon(mode?: string) {
  switch (mode) {
    case 'root_cause':
      return <ThunderboltOutlined />
    case 'performance':
      return <RadarChartOutlined />
    case 'trace':
      return <BranchesOutlined />
    case 'inspection_review':
      return <PlayCircleOutlined />
    default:
      return <RobotOutlined />
  }
}

export function buildPromptItems(mode: WorkbenchMode) {
  if (mode === 'root_cause') {
    return [
      { key: 'alert', icon: <ThunderboltOutlined />, label: '分析当前告警根因' },
      { key: 'blast-radius', icon: <RobotOutlined />, label: '给出影响面和最可能触发链路' },
      { key: 'evidence', icon: <EyeOutlined />, label: '整理异常证据并输出结论' },
    ]
  }
  if (mode === 'performance') {
    return [
      { key: 'latency', icon: <ApiOutlined />, label: '分析服务延迟热点' },
      { key: 'capacity', icon: <RadarChartOutlined />, label: '判断容量瓶颈与资源抖动' },
      { key: 'compare', icon: <EyeOutlined />, label: '对比近期波动与基线差异' },
    ]
  }
  if (mode === 'trace') {
    return [
      { key: 'trace-hotspot', icon: <BranchesOutlined />, label: '定位最慢调用链与热点 span' },
      { key: 'upstream', icon: <RobotOutlined />, label: '总结跨服务链路中的关键阻塞点' },
      { key: 'entry', icon: <EyeOutlined />, label: '从入口请求开始追踪异常路径' },
    ]
  }
  if (mode === 'inspection_review') {
    return [
      { key: 'review', icon: <PlayCircleOutlined />, label: '复盘最近一次巡检异常' },
      { key: 'policy', icon: <ToolOutlined />, label: '根据巡检结果生成自动化建议' },
      { key: 'handoff', icon: <RobotOutlined />, label: '把巡检发现转成后续分析' },
    ]
  }
  return [
    { key: 'summary', icon: <ThunderboltOutlined />, label: '帮我解释一个问题' },
    { key: 'context', icon: <ToolOutlined />, label: '帮我整理一份资料' },
    { key: 'next', icon: <RobotOutlined />, label: '一起讨论实现方案' },
  ]
}
