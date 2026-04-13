import type { ReactNode } from 'react'

interface StatItem {
  label: string
  value: ReactNode
  icon?: ReactNode
}

export function StatGrid({ items }: { items: StatItem[] }) {
  return (
    <div className="kc-stat-grid">
      {items.map((item) => (
        <div key={item.label} className="kc-stat-card">
          <div>
            <div className="kc-stat-label">{item.label}</div>
            <p className="kc-stat-value">{item.value}</p>
          </div>
          {item.icon ? <div className="kc-stat-icon">{item.icon}</div> : null}
        </div>
      ))}
    </div>
  )
}
