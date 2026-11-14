export const ACCESS_ACTION_OPTIONS = [
  { value: 'view', label: '查看' },
  { value: 'list', label: '列表' },
  { value: 'watch', label: '监听' },
  { value: 'create', label: '创建' },
  { value: 'update', label: '修改' },
  { value: 'delete', label: '删除' },
  { value: 'restart', label: '重启' },
  { value: 'rollback', label: '回滚' },
  { value: 'scale', label: '伸缩' },
  { value: 'trigger', label: '触发' },
  { value: 'logs', label: '日志' },
  { value: 'exec', label: '终端' },
]

export const ACCESS_ACTION_LABEL_MAP = Object.fromEntries(
  ACCESS_ACTION_OPTIONS.map((option) => [option.value, option.label]),
) as Record<string, string>
