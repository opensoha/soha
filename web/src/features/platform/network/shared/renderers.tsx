import { Tag, Tooltip, Typography } from 'antd'

const { Text } = Typography

export function renderNetworkTextList(value?: string[], empty = '-') {
  if (!value || value.length === 0) return <Text type="secondary">{empty}</Text>
  return (
    <div className="soha-rbac-subject-list">
      {value.slice(0, 3).map((item) => (
        <Tag key={item}>{item}</Tag>
      ))}
      {value.length > 3 ? (
        <Tooltip title={value.slice(3).join(', ')}>
          <Tag>{`+${value.length - 3}`}</Tag>
        </Tooltip>
      ) : null}
    </div>
  )
}
