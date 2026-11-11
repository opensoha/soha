import { Typography } from 'antd'
import type { OverviewSectionBarProps } from './overview-types'

const { Text } = Typography

export function OverviewSectionBar({
  title,
  description,
  kicker,
  extra,
  className,
}: OverviewSectionBarProps) {
  return (
    <div className={['soha-overview-section-bar', className].filter(Boolean).join(' ')}>
      <div className="soha-overview-section-copy">
        {kicker ? <div className="soha-overview-section-kicker">{kicker}</div> : null}
        <Text strong className="soha-overview-section-title">
          {title}
        </Text>
        {description ? <div className="soha-overview-inline-caption">{description}</div> : null}
      </div>
      {extra ? <div className="soha-overview-section-extra">{extra}</div> : null}
    </div>
  )
}
