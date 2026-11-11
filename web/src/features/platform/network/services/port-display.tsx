import { ArrowRightOutlined } from '@ant-design/icons'
import { MetadataTag } from '@/components/status-tag'
import type { MetadataTagTone } from '@/components/status-tag'
import type { ServicePortMapping } from './types'
import './styles.css'

function serviceTypeTone(type: string): MetadataTagTone {
  switch (type) {
    case 'NodePort':
      return 'gold'
    case 'LoadBalancer':
      return 'cyan'
    case 'ExternalName':
      return 'purple'
    case 'ClusterIP':
      return 'blue'
    default:
      return 'default'
  }
}

export function ServiceTypeTag({ type }: { type: string }) {
  return <MetadataTag label={type || '-'} tone={serviceTypeTone(type)} />
}

function PortArrow() {
  return <ArrowRightOutlined aria-hidden className="soha-service-port-arrow" />
}

function protocolTone(protocol: string): MetadataTagTone {
  return protocol === 'UDP' ? 'cyan' : protocol === 'SCTP' ? 'purple' : 'blue'
}

function PortTag({
  value,
  field,
  tone = 'default',
}: {
  value: string
  field: 'name' | 'nodePort' | 'port' | 'protocol' | 'targetPort'
  tone?: MetadataTagTone
}) {
  return (
    <MetadataTag
      label={
        <span className="soha-service-tag-text" data-port-field={field}>
          {value}
        </span>
      }
      title={`${field}: ${value}`}
      tone={tone}
      className="soha-service-port-tag"
    />
  )
}

function PortFields({ mapping }: { mapping: ServicePortMapping }) {
  return (
    <>
      <PortTag field="protocol" value={mapping.protocol} tone={protocolTone(mapping.protocol)} />
      <PortTag field="port" value={String(mapping.port)} />
      <PortArrow />
      <PortTag field="targetPort" value={mapping.targetPort} />
      {mapping.nodePort ? (
        <PortTag field="nodePort" value={`NodePort: ${mapping.nodePort}`} />
      ) : null}
    </>
  )
}

export function ServicePortDisplay({
  portMappings,
  ports,
  detailed = false,
}: {
  portMappings?: ServicePortMapping[]
  ports?: string[]
  detailed?: boolean
}) {
  if (!portMappings?.length)
    return ports?.length ? (
      <span className="soha-service-port-list">
        {ports.map((port, index) => (
          <MetadataTag
            key={`${port}-${index}`}
            label={<span className="soha-service-tag-text">{port}</span>}
            className="soha-service-port-tag"
          />
        ))}
      </span>
    ) : (
      '—'
    )

  return (
    <span className={`soha-service-port-list${detailed ? ' is-detailed' : ''}`}>
      {portMappings.map((mapping, index) => (
        <span
          className="soha-service-port-row"
          key={`${mapping.name ?? ''}-${mapping.port}-${mapping.protocol}-${index}`}
        >
          {detailed ? (
            <>
              <span className="soha-service-port-name">
                {mapping.name ? <PortTag field="name" value={mapping.name} /> : '—'}
              </span>
              <span className="soha-service-port-chain">
                <PortFields mapping={mapping} />
              </span>
            </>
          ) : (
            <>
              {mapping.name ? <PortTag field="name" value={mapping.name} /> : null}
              <PortFields mapping={mapping} />
            </>
          )}
        </span>
      ))}
    </span>
  )
}
