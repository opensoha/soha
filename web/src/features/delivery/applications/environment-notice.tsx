import { SafetyOutlined } from '@ant-design/icons'
import { localeText, useI18n } from '@/i18n'

export function EnvironmentNotice({
  name,
  cluster,
  namespace,
  isProduction,
}: {
  isProduction?: boolean
  name: string
  cluster?: string
  namespace?: string
}) {
  const { localeCode } = useI18n()
  if (isProduction === false) return null
  const label = isProduction
    ? localeText(localeCode, '当前：生产环境', 'Current: production')
    : localeText(localeCode, '环境类型未确认', 'Environment type unverified')
  return (
    <div className="soha-production-notice" role="note" aria-label={label}>
      <SafetyOutlined />
      <strong>
        {label} · {name}
      </strong>
      {cluster ? (
        <span>
          {localeText(localeCode, '集群', 'Cluster')}：{cluster}
        </span>
      ) : null}
      {namespace ? <span>Namespace：{namespace}</span> : null}
    </div>
  )
}
