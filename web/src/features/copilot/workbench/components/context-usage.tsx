import { Progress } from 'antd'
import { modelContextSummary } from '../context-selection'

export function ContextRows({ items }: { items: { label: string; value: string }[] }) {
  return (
    <dl className="soha-ai-context-rows">
      {items.map((item) => (
        <div key={item.label}>
          <dt>{item.label}</dt>
          <dd>{item.value}</dd>
        </div>
      ))}
    </dl>
  )
}

export function ContextUsage({ value }: { value?: unknown }) {
  const usage = modelContextSummary(value)
  return (
    <div className="soha-ai-context-usage">
      <strong>{usage.label}</strong>
      {usage.percent !== undefined && (
        <Progress
          percent={Math.min(100, usage.percent)}
          size="small"
          format={() => `${usage.estimated ? '约 ' : ''}${Math.round(usage.percent!)}%`}
          aria-label="最近一次输入占模型窗口"
        />
      )}
      <ContextRows items={usage.rows} />
      <p>最近一次模型调用。输入包含系统提示、历史、背景和工具定义；不是当前草稿的预测用量。</p>
    </div>
  )
}
