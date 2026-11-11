import {
  AppstoreOutlined,
  BookOutlined,
  CommentOutlined,
  FileSearchOutlined,
  LinkOutlined,
  NodeIndexOutlined,
  SettingOutlined,
  ToolOutlined,
} from '@ant-design/icons'

export const CHAT_STARTERS = [
  {
    value: 'resources',
    label: '资源与应用',
    icon: <AppstoreOutlined />,
    description: '梳理资源状态与应用关系',
    prompt: '请帮我梳理资源与应用的状态和关联关系，先确认需要查看的对象、范围与可用数据来源。',
  },
  {
    value: 'troubleshoot',
    label: '问题排查',
    icon: <FileSearchOutlined />,
    description: '从现象和线索逐步定位问题',
    prompt:
      '我想排查一个问题，请先帮我明确现象、影响范围和需要补充的信息，再给出排查步骤。问题描述：',
  },
  {
    value: 'knowledge',
    label: '知识与文档',
    icon: <BookOutlined />,
    description: '查找资料、解释配置与使用方式',
    prompt: '请根据可用的知识与文档帮我解答问题，并标明来源；资料不足时请说明。我的问题是：',
  },
  {
    value: 'plan',
    label: '变更与方案',
    icon: <NodeIndexOutlined />,
    description: '讨论实现路径、风险和验证方式',
    prompt:
      '请帮我制定一个变更或实现方案，包含步骤、影响、风险和验证方式，先不要执行操作。目标是：',
  },
] as const

export const CHAT_COMMANDS = [
  { value: 'tools', label: '工具与技能', icon: <ToolOutlined />, action: 'tools' },
  { value: 'context', label: '查看上下文', icon: <LinkOutlined />, action: 'context' },
  { value: 'settings', label: '会话设置', icon: <SettingOutlined />, action: 'settings' },
  { value: 'session', label: '引用其他会话', icon: <CommentOutlined />, action: 'session' },
  { value: 'analysis', label: '专业分析任务', icon: <FileSearchOutlined />, action: 'analysis' },
  ...CHAT_STARTERS,
] as const

export function slashToken(value: string) {
  const match = value.match(/(?:^|\s)\/([^\s/]*)$/)
  return match
    ? { start: value.length - match[1].length - 1, query: match[1].toLowerCase() }
    : undefined
}

export function matchingCommands(value: string) {
  const token = slashToken(value)
  return token
    ? CHAT_COMMANDS.filter(
        (item) => item.value.includes(token.query) || item.label.includes(token.query),
      )
    : []
}

export function replaceSlashToken(value: string, replacement: string) {
  const token = slashToken(value)
  return token ? value.slice(0, token.start) + replacement : value
}
