import type { ReactNode } from 'react'

interface StatItem {
  label: string
  value: ReactNode
  icon?: ReactNode
}

export function StatGrid({ items }: { items: StatItem[] }) {
  return (
    <div className="soha-stat-grid">
      {items.map((item) => (
        <div key={item.label} className="soha-stat-card">
          <div>
            <div className="soha-stat-label">{item.label}</div>
            <p className="soha-stat-value">{item.value}</p>
          </div>
          {item.icon ? <div className="soha-stat-icon">{item.icon}</div> : null}
        </div>
      ))}
    </div>
  )
}
